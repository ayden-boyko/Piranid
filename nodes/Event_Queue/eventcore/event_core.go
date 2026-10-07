package eventcore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"Piranid/node"

	"Piranid/pkg/authn"
	"Piranid/pkg/telemetry"

	handler "github.com/ayden-boyko/Piranid/nodes/Event_Queue/handlers"
	"github.com/ayden-boyko/Piranid/nodes/Event_Queue/models"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// BrokerDialTimeout bounds a single connection attempt.
const BrokerDialTimeout = 10 * time.Second

// BrokerReconnectDelay is how long to wait before re-dialling after the broker
// drops the connection.
const BrokerReconnectDelay = 5 * time.Second

type EventNode struct {
	*node.Node
	Service_ID string

	// Services is a pointer so the registry can never be observed in the
	// zero-value state. It used to be an embedded value that was never
	// constructed, leaving its map nil: reads were legal, so the read paths
	// looked healthy, and the first write panicked.
	Services *models.Services

	// broker owns the AMQP connection and re-establishes it when the broker
	// drops it. The previous code held a bare *amqp.Connection with no
	// NotifyClose handler and no reconnect, so a broker restart left every
	// handle permanently dead.
	broker *Broker
}

// Broker manages the RabbitMQ connection lifecycle.
//
// A bare connection is not enough: amqp091 does not reconnect, and a connection
// that dies takes every channel derived from it with it. This dials, watches for
// closure, and re-dials on an interval.
type Broker struct {
	url  string
	conn *amqp.Connection

	mu   sync.RWMutex
	done chan struct{}
	// closeOnce guards the one-shot close of done.
	closeOnce sync.Once

	// channelEpoch increments on every reconnect. Handlers compare it before
	// using a stored channel and force the caller to re-register a service
	// after a reconnect, because channels from the old connection are dead.
	channelEpoch atomic64

	logger *zap.Logger
	// onReconnect is called after a successful re-dial, so the registry can be
	// marked stale.
	onReconnect func()
}

// atomic64 is a tiny mutex-guarded counter, avoiding a dependency on
// sync/atomic's generics for such a narrow need.
type atomic64 struct {
	mu sync.Mutex
	v  uint64
}

func (a *atomic64) Inc() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v++
	return a.v
}

func (a *atomic64) Load() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}

// NewBroker dials the broker and starts the supervision loop.
func NewBroker(url string, logger *zap.Logger, onReconnect func()) (*Broker, error) {
	b := &Broker{url: url, logger: logger, onReconnect: onReconnect, done: make(chan struct{})}

	conn, err := b.dial()
	if err != nil {
		return nil, err
	}
	b.conn = conn

	go b.supervise(conn)
	return b, nil
}

func (b *Broker) dial() (*amqp.Connection, error) {
	conn, err := amqp.DialConfig(b.url, amqp.Config{
		Heartbeat: 10 * time.Second,
		Locale:    "en_US",
		Dial:      amqp.DefaultDial(BrokerDialTimeout),
	})
	if err != nil {
		return nil, fmt.Errorf("dialing RabbitMQ: %w", err)
	}
	return conn, nil
}

// Connection returns the current connection, or nil if it is down.
func (b *Broker) Connection() *amqp.Connection {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.conn == nil || b.conn.IsClosed() {
		return nil
	}
	return b.conn
}

// Epoch returns the current connection generation.
func (b *Broker) Epoch() uint64 { return b.channelEpoch.Load() }

// supervise re-dials whenever the broker drops the connection.
func (b *Broker) supervise(conn *amqp.Connection) {
	closed := make(chan *amqp.Error, 1)
	conn.NotifyClose(closed)

	for {
		select {
		case <-closed:
			b.logger.Warn("RabbitMQ connection lost, reconnecting",
				zap.Duration("delay", BrokerReconnectDelay))

			next, ok := b.reconnect()
			if !ok {
				return
			}
			conn = next
			closed = make(chan *amqp.Error, 1)
			conn.NotifyClose(closed)

			epoch := b.channelEpoch.Inc()
			b.logger.Info("RabbitMQ reconnected", zap.Uint64("epoch", epoch))
			if b.onReconnect != nil {
				b.onReconnect()
			}
		case <-b.done:
			return
		}
	}
}

// reconnect re-dials until it succeeds or the broker is closed.
//
// It had no retry at all before, so a broker that restarted while the node was
// running left every AMQP handle permanently dead.
func (b *Broker) reconnect() (*amqp.Connection, bool) {
	for {
		conn, err := b.dial()
		if err == nil {
			b.mu.Lock()
			b.conn = conn
			b.mu.Unlock()
			return conn, true
		}

		b.logger.Warn("RabbitMQ reconnect failed", zap.Error(err))

		select {
		case <-time.After(BrokerReconnectDelay):
		case <-b.done:
			return nil, false
		}
	}
}

// Close shuts the broker down. Safe to call more than once.
func (b *Broker) Close() error {
	var err error
	b.closeOnce.Do(func() {
		close(b.done)

		b.mu.Lock()
		conn := b.conn
		b.conn = nil
		b.mu.Unlock()

		if conn != nil && !conn.IsClosed() {
			err = conn.Close()
		}
	})
	return err
}

func (n *EventNode) GetServiceID() string { return n.Service_ID }

// NewEventNode builds the node with an initialised registry and broker.
func NewEventNode(n *node.Node, serviceID, brokerURL string, logger *zap.Logger) (*EventNode, error) {
	services := models.NewServices()

	broker, err := NewBroker(brokerURL, logger, func() {
		// Every stored AMQP channel belongs to the dead connection. The
		// registry is cleared so clients re-register rather than issuing
		// declarations on channels that no longer work.
		services.Reset()
	})
	if err != nil {
		return nil, err
	}

	return &EventNode{
		Node:       n,
		Service_ID: serviceID,
		Services:   services,
		broker:     broker,
	}, nil
}

// Broker returns the connection manager.
func (n *EventNode) Broker() *Broker { return n.broker }

// RegisterRoutes wires the queue administration API.
//
// Every service and queue endpoint sits behind authn.Middleware. This API is
// destructive (it declares and deletes broker queues), so it fails closed: if
// verification cannot be configured, the node does not start.
func (n *EventNode) RegisterRoutes(ctx context.Context, logger *zap.Logger) {
	apiVer := os.Getenv("API_VERSION")
	if apiVer == "" {
		apiVer = "v1"
	}

	middleware, err := BuildAuthMiddleware(logger)
	if err != nil {
		logger.Error("could not configure token verification, refusing to start",
			zap.Error(err))
		return
	}

	// Build the middleware once, at registration, rather than per request.
	// Build each protected route once, at registration, rather than rebuilding
	// the middleware on every request as the previous code did.
	protected := func(h http.HandlerFunc) http.HandlerFunc {
		guarded := middleware.Middleware(h)
		return func(w http.ResponseWriter, r *http.Request) {
			guarded.ServeHTTP(w, r)
		}
	}

	deps := handler.Deps{
		Services:     n.Services,
		Connection:   n.broker.Connection(),
		Logger:       logger,
		DrainTimeout: handler.DefaultDrainTimeout,
	}

	// deps.Connection is captured once, but a reconnect replaces it. Resolve it
	// per request instead.
	resolve := func(w http.ResponseWriter, r *http.Request) (handler.Deps, bool) {
		d := deps
		d.Connection = n.broker.Connection()
		if d.Connection == nil {
			writeServiceUnavailable(w, logger, "the message broker is unavailable; retry shortly")
			return d, false
		}
		return d, true
	}

	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/event_test", apiVer),
		handler.EventTestHandler)

	n.Node.Router.HandleFunc(fmt.Sprintf("GET /api/%s/services", apiVer), protected(
		func(w http.ResponseWriter, r *http.Request) {
			handler.GetAllServicesHandler(w, r, deps)
		}))

	n.Node.Router.HandleFunc(fmt.Sprintf("POST /api/%s/services/{service_id}", apiVer), protected(
		func(w http.ResponseWriter, r *http.Request) {
			d, ok := resolve(w, r)
			if !ok {
				return
			}
			handler.AddServiceHandler(w, r, d)
		}))

	n.Node.Router.HandleFunc(fmt.Sprintf("GET /api/%s/services/{service_id}", apiVer), protected(
		func(w http.ResponseWriter, r *http.Request) {
			handler.GetServiceHandler(w, r, deps)
		}))

	n.Node.Router.HandleFunc(fmt.Sprintf("DELETE /api/%s/services/{service_id}", apiVer), protected(
		func(w http.ResponseWriter, r *http.Request) {
			d, ok := resolve(w, r)
			if !ok {
				return
			}
			handler.RemoveServiceHandler(w, r, d)
		}))

	n.Node.Router.HandleFunc(fmt.Sprintf("POST /api/%s/services/{service_id}/queue/{queue_id}", apiVer), protected(
		func(w http.ResponseWriter, r *http.Request) {
			d, ok := resolve(w, r)
			if !ok {
				return
			}
			handler.AddQueueHandler(w, r, d)
		}))

	n.Node.Router.HandleFunc(fmt.Sprintf("GET /api/%s/services/{service_id}/queue/{queue_id}", apiVer), protected(
		func(w http.ResponseWriter, r *http.Request) {
			handler.GetQueueHandler(w, r, deps)
		}))

	n.Node.Router.HandleFunc(fmt.Sprintf("DELETE /api/%s/services/{service_id}/queue/{queue_id}", apiVer), protected(
		func(w http.ResponseWriter, r *http.Request) {
			d, ok := resolve(w, r)
			if !ok {
				return
			}
			handler.RemoveQueueHandler(w, r, d)
		}))

	// Liveness probe. Unauthenticated and dependency-free so Kubernetes can
	// reach it without a credential.
	n.Node.Router.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Wrap the router so every request produces a server span and an incoming
	// traceparent is linked rather than orphaned. Handlers previously derived
	// their spans from a context captured at startup, so client traces never
	// connected to them.
	n.Node.Server.Handler = telemetry.HTTPMiddleware("event_queue", n.Node.Router)

	logger.Info("routes registered",
		zap.String("api_version", apiVer),
		zap.Bool("token_verification", !isAnonymous(logger)),
	)
}

func isAnonymous(logger *zap.Logger) bool {
	return os.Getenv("AUTH_ALLOW_ANONYMOUS") == "true"
}

func writeServiceUnavailable(w http.ResponseWriter, logger *zap.Logger, message string) {
	logger.Warn("broker unavailable", zap.String("detail", message))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprintf(w, `{"error":"%s","message":%q}`, "broker_unavailable", message)
}

// BuildAuthMiddleware constructs the token verifier.
//
// Configuration comes from the environment:
//
//	AUTH_JWKS_URL         where the auth node publishes its public keys (required)
//	AUTH_ISSUER           expected iss claim (required)
//	AUTH_AUDIENCE         expected aud claim (required)
//	AUTH_LEEWAY           clock-skew tolerance, default 30s
//	AUTH_ALLOW_ANONYMOUS  set to "true" to disable verification (development)
func BuildAuthMiddleware(logger *zap.Logger) (*authn.Middleware, error) {
	adapter := loggerAdapter{logger}

	if os.Getenv("AUTH_ALLOW_ANONYMOUS") == "true" {
		return authn.AllowAll(adapter), nil
	}

	jwksURL := os.Getenv("AUTH_JWKS_URL")
	if jwksURL == "" {
		return nil, errors.New("AUTH_JWKS_URL is required")
	}
	issuer := os.Getenv("AUTH_ISSUER")
	if issuer == "" {
		return nil, errors.New("AUTH_ISSUER is required")
	}
	audience := os.Getenv("AUTH_AUDIENCE")
	if audience == "" {
		return nil, errors.New("AUTH_AUDIENCE is required")
	}

	fetcher := authn.NewJWKSFetcher(jwksURL, nil, 0)

	// Warm at startup so the first request is not the one that pays for it. A
	// failure is not fatal: the fetcher retries on the first unknown kid.
	if err := fetcher.Warm(); err != nil {
		logger.Warn("could not fetch JWKS at startup, will retry on first use",
			zap.String("url", jwksURL), zap.Error(err))
	}

	verifier := authn.NewVerifier(fetcher,
		authn.WithIssuer(issuer),
		authn.WithAudience(audience),
		authn.WithLeeway(ClockSkew()),
	)

	logger.Info("token verification enabled",
		zap.String("jwks_url", jwksURL),
		zap.String("issuer", issuer),
		zap.String("audience", audience),
	)

	return authn.NewMiddleware(verifier, adapter), nil
}

// ClockSkew reads AUTH_LEEWAY, defaulting to the shared constant.
func ClockSkew() time.Duration {
	raw := os.Getenv("AUTH_LEEWAY")
	if raw == "" {
		return authn.ClockSkew * time.Second
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return authn.ClockSkew * time.Second
	}
	return d
}

// SafeShutdown drains HTTP requests and releases the broker connection.
func (n *EventNode) SafeShutdown(ctx context.Context, logger *zap.Logger) error {
	var errs []error
	if err := n.Server.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}
	if n.broker != nil {
		if err := n.broker.Close(); err != nil {
			errs = append(errs, fmt.Errorf("broker close: %w", err))
		}
	}
	return errors.Join(errs...)
}

// loggerAdapter bridges zap to the small interface authn requires, so pkg/authn
// does not depend on a logging library.
type loggerAdapter struct{ l *zap.Logger }

func (a loggerAdapter) Warn(msg string, fields ...any) {
	if a.l == nil {
		return
	}
	zapFields := make([]zap.Field, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		zapFields = append(zapFields, zap.Any(fmt.Sprint(fields[i]), fields[i+1]))
	}
	a.l.Warn(msg, zapFields...)
}
