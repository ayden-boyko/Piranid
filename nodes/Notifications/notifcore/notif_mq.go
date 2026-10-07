package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// DefaultWorkerPoolSize bounds concurrent delivery workers.
const DefaultWorkerPoolSize = 5

// MQReconnectDelay is how long to wait before re-dialling the broker.
const MQReconnectDelay = 5 * time.Second

// ErrMQNotConfigured reports missing broker configuration.
var ErrMQNotConfigured = errors.New("notifications: message queue not configured")

// MQConfig describes the RabbitMQ consumer.
type MQConfig struct {
	Host       string
	Port       string
	User       string
	Password   string
	QueueName  string
	WorkerPool int

	// Durable and AutoDelete control queue durability.
	//
	// The original declaration was durable=false, autoDelete=true, so the queue
	// was destroyed when the last consumer disconnected and every pending
	// notification was lost on restart.
	Durable    bool
	AutoDelete bool

	// ReconnectDelay is the pause between reconnect attempts.
	ReconnectDelay time.Duration
}

// LoadMQConfig reads broker configuration from the environment.
func LoadMQConfig() MQConfig {
	pool, err := strconv.Atoi(os.Getenv("EVENT_SERVICE_WORKER_POOL_SIZE"))
	if err != nil || pool <= 0 {
		pool = DefaultWorkerPoolSize
	}

	return MQConfig{
		Host:           envOr("RABBIT_MQ_HOST", "rabbitmq"),
		Port:           envOr("RABBIT_MQ_PORT", "5672"),
		User:           envOr("RABBIT_MQ_USER", "guest"),
		Password:       envOr("RABBIT_MQ_PASSWORD", "guest"),
		QueueName:      os.Getenv("RABBIT_MQ_QUEUE_NAME"),
		WorkerPool:     pool,
		Durable:        true,
		AutoDelete:     false,
		ReconnectDelay: MQReconnectDelay,
	}
}

// Validate reports configuration that would leave the consumer unable to start.
func (c MQConfig) Validate() error {
	if c.QueueName == "" {
		return fmt.Errorf("%w: RABBIT_MQ_QUEUE_NAME is required", ErrMQNotConfigured)
	}
	if c.WorkerPool <= 0 {
		return fmt.Errorf("%w: worker pool size must be positive", ErrMQNotConfigured)
	}
	return nil
}

// URL renders the AMQP connection string.
func (c MQConfig) URL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:%s/", c.User, c.Password, c.Host, c.Port)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// StartMQListener consumes notification messages until ctx is cancelled.
//
// The original version declared its connection, channel and derived context
// with defer, then completed a non-blocking select and fell out of the function
// body. Every deferred call fired immediately, so the connection and channel
// closed and the workers' delivery channels emptied within milliseconds of
// start. The trailing comment asserted the opposite reasoning.
//
// This version blocks for the lifetime of the consumer and returns only when
// ctx is done.
func (n *NotificationNode) StartMQListener(ctx context.Context, cfg MQConfig, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	// The dial-and-consume loop owns the connection for its whole lifetime.
	// The original version declared the connection with defer inside a
	// non-blocking function, so the connection closed milliseconds after start
	// and the workers' delivery channels emptied immediately.
	for {
		if ctx.Err() != nil {
			return nil
		}

		err := n.runConsumerSession(ctx, cfg, logger)

		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return nil
		}

		logger.Warn("notification consumer session ended, reconnecting",
			zap.Error(err),
			zap.Duration("delay", cfg.ReconnectDelay))

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cfg.ReconnectDelay):
		}
	}
}

// runConsumerSession dials, declares, consumes, and blocks until the context is
// done or the broker drops the connection.
func (n *NotificationNode) runConsumerSession(ctx context.Context, cfg MQConfig, logger *zap.Logger) error {
	conn, err := amqp.Dial(cfg.URL())
	if err != nil {
		// Returned, not log.Panic'd. The original killed the process from a
		// goroutine, skipping every deferred cleanup, and retried nothing.
		return fmt.Errorf("dialing RabbitMQ: %w", err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("opening AMQP channel: %w", err)
	}
	defer channel.Close()

	queue, err := channel.QueueDeclare(
		cfg.QueueName,
		cfg.Durable,    // durable
		cfg.AutoDelete, // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("declaring queue %q: %w", cfg.QueueName, err)
	}

	// Manual acknowledgement. The original combined autoAck=true with an
	// explicit msg.Ack, which the client library reports as a double ack and
	// which loses the message if a worker dies before finishing.
	msgs, err := channel.Consume(
		queue.Name,
		"",    // server-generated consumer tag
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("registering consumer: %w", err)
	}

	logger.Info("notification consumer started",
		zap.String("queue", queue.Name),
		zap.Int("workers", cfg.WorkerPool),
		zap.Bool("durable", cfg.Durable),
	)

	workerCtx, cancelWorkers := context.WithCancel(ctx)

	var wg sync.WaitGroup
	for i := 0; i < cfg.WorkerPool; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.notificationWorker(workerCtx, msgs, logger)
		}()
	}

	// Block here for the lifetime of the consumer. This wait is what the
	// original function omitted.
	var sessionErr error
	select {
	case <-ctx.Done():
		logger.Info("notification consumer stopping")
	case brokerErr := <-conn.NotifyClose(make(chan *amqp.Error)):
		sessionErr = fmt.Errorf("broker connection closed: %v", brokerErr)
	}

	cancelWorkers()
	wg.Wait()

	return sessionErr
}

// notificationWorker consumes deliveries until the channel closes.
//
// Unparseable messages are rejected rather than dropped, so a malformed producer
// cannot silently discard notifications.
func (n *NotificationNode) notificationWorker(
	ctx context.Context,
	msgs <-chan amqp.Delivery,
	logger *zap.Logger,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}

			var entry model.NotifEntry
			if err := json.Unmarshal(msg.Body, &entry); err != nil {
				logger.Warn("discarding unparseable notification",
					zap.Error(err),
					zap.Int("bytes", len(msg.Body)))
				// Reject without requeue: the body is not going to parse on a
				// redelivery either.
				_ = msg.Reject(false)
				continue
			}

			if err := n.HandleNotifSend(ctx, entry); err != nil {
				logger.Warn("notification delivery failed",
					zap.String("service_id", entry.ServiceId),
					zap.Error(err))
				// Return to the queue so another worker can retry.
				_ = msg.Nack(false, true)
				continue
			}

			_ = msg.Ack(false)
		}
	}
}

// brokerConn is the connection consumeOnce works against.
//
// It is a package-level handle because StartMQListener owns the dial and its
// lifetime, while consumeOnce needs the connection for both a channel and the
// close notification.
var brokerConn *amqp.Connection

// SetBrokerConnection installs the connection consumeOnce should use. It exists
// so StartMQListener can keep ownership of the dial while consumeOnce can be
// tested with a stub.
func SetBrokerConnection(conn *amqp.Connection) { brokerConn = conn }
