package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ayden-boyko/Piranid/nodes/Event_Queue/models"
	transactions "github.com/ayden-boyko/Piranid/nodes/Event_Queue/transactions"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

var tracer = otel.Tracer("event_queue/handlers")

// DefaultDrainTimeout bounds how long a DELETE waits for a queue backlog to
// clear.
//
// The drain loop previously ran with no deadline and no cancellation, holding an
// AMQP channel and an HTTP goroutine while sleeping. One stuck consumer pinned
// the request open indefinitely.
const DefaultDrainTimeout = 30 * time.Second

// DrainPollInterval is how often the drain loop re-checks the message count.
const DrainPollInterval = 500 * time.Millisecond

// Deps carries what the handlers need. Passed explicitly rather than as seven
// positional arguments so the handlers can be exercised without a listener.
type Deps struct {
	Services     *models.Services
	Connection   *amqp.Connection
	Logger       *zap.Logger
	DrainTimeout time.Duration
}

// ---------------------------------------------------------------------------
// responses
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, logger *zap.Logger, status int, code, message string) {
	if logger != nil {
		logger.Warn("request rejected",
			zap.String("code", code),
			zap.String("message", message),
			zap.Int("status", status),
		)
	}
	writeJSON(w, status, transactions.ErrorResponse{Error: code, Message: message})
}

// statusFor maps a registry error onto an HTTP status.
//
// This is the mapping the handlers previously lacked: every failure collapsed
// to 500, so a client could not distinguish a duplicate from a missing resource.
func statusFor(err error) (int, string) {
	switch {
	case errors.Is(err, models.ErrNotFound):
		return http.StatusNotFound, transactions.CodeNotFound
	case errors.Is(err, models.ErrAlreadyExists):
		return http.StatusConflict, transactions.CodeConflict
	case errors.Is(err, models.ErrDraining):
		return http.StatusLocked, transactions.CodeDraining
	default:
		return http.StatusInternalServerError, transactions.CodeInternal
	}
}

// writeErr maps an error to its status and writes the body.
func writeErr(w http.ResponseWriter, logger *zap.Logger, err error) {
	status, code := statusFor(err)
	writeError(w, logger, status, code, err.Error())
}

// decode reads a JSON body into dst.
func decode(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("request body: %w", err)
	}
	return nil
}

// queueResponse renders a queue for the wire.
func queueResponse(q models.ServiceQueue) transactions.GetQueueResponse {
	return transactions.GetQueueResponse{
		Name:      q.QueueName,
		ServiceId: q.ServiceId,
		Loggable:  q.Loggable,
		Tags:      q.Tags,
		Draining:  q.SearchTags(models.DrainingTag),
	}
}

// ---------------------------------------------------------------------------
// handlers
// ---------------------------------------------------------------------------

// EventTestHandler is an unauthenticated liveness echo.
func EventTestHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// AddServiceHandler registers a service.
//
// The service id comes from the URL path. It previously came from the JSON body
// while the route pattern also declared {service_id}, so a GET or DELETE with an
// empty body failed at the decode step before reaching any logic.
func AddServiceHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "AddService")
	defer span.End()

	serviceID := r.PathValue("service_id")
	if serviceID == "" {
		writeError(w, d.Logger, http.StatusBadRequest, transactions.CodeBadRequest,
			"service_id is required in the path")
		span.SetStatus(codes.Error, "missing service_id")
		return
	}

	// A body is optional, but if present the path takes precedence.
	var req transactions.AddServiceRequest
	if err := decode(r, &req); err != nil {
		writeError(w, d.Logger, http.StatusBadRequest, transactions.CodeBadRequest, err.Error())
		span.SetStatus(codes.Error, err.Error())
		return
	}
	req.ServiceId = serviceID

	if d.Connection == nil {
		writeError(w, d.Logger, http.StatusServiceUnavailable, transactions.CodeInternal,
			"no broker connection available")
		span.SetStatus(codes.Error, "no broker connection")
		return
	}

	channel, err := d.Connection.Channel()
	if err != nil {
		writeError(w, d.Logger, http.StatusServiceUnavailable, transactions.CodeBrokerError,
			fmt.Sprintf("opening AMQP channel: %v", err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// A duplicate registration must not leak the channel we just opened.
	service, err := d.Services.AddService(serviceID, channel)
	if err != nil {
		_ = channel.Close()
		writeErr(w, d.Logger, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	d.Logger.Info("service registered",
		zap.String("service_id", serviceID),
		zap.String("name", req.Name),
	)

	writeJSON(w, http.StatusCreated, transactions.GetServiceResponse{
		ServiceId: service.ID(),
		Queues:    map[string]transactions.GetQueueResponse{},
	})
	span.SetStatus(codes.Ok, "")
}

// GetServiceHandler returns one service and its queues.
func GetServiceHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "GetService")
	defer span.End()

	serviceID := r.PathValue("service_id")
	service, err := d.Services.GetService(serviceID)
	if err != nil {
		writeErr(w, d.Logger, err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// SnapshotQueue copies under the service's own lock, so concurrent drains
	// cannot race this iteration. The previous code read service.Queues
	// directly after GetService released the registry lock.
	queues := make(map[string]transactions.GetQueueResponse)
	for name, q := range service.SnapshotQueue() {
		queues[name] = queueResponse(q)
	}

	writeJSON(w, http.StatusOK, transactions.GetServiceResponse{
		ServiceId: service.ID(),
		Queues:    queues,
	})
	span.SetStatus(codes.Ok, "")
}

// GetAllServicesHandler returns every registered service.
func GetAllServicesHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "GetAllServices")
	defer span.End()

	snapshots := d.Services.SnapshotServices()
	resp := transactions.GetAllServicesResponse{
		Services: make([]transactions.GetServiceResponse, 0, len(snapshots)),
	}
	for id, queues := range snapshots {
		converted := make(map[string]transactions.GetQueueResponse, len(queues))
		for name, q := range queues {
			converted[name] = queueResponse(q)
		}
		resp.Services = append(resp.Services, transactions.GetServiceResponse{
			ServiceId: id,
			Queues:    converted,
		})
	}

	writeJSON(w, http.StatusOK, resp)
	span.SetStatus(codes.Ok, "")
}

// AddQueueHandler declares a queue for a service.
func AddQueueHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "AddQueue")
	defer span.End()

	serviceID := r.PathValue("service_id")
	queueName := r.PathValue("queue_id")

	var req transactions.AddQueueRequest
	if err := decode(r, &req); err != nil {
		writeError(w, d.Logger, http.StatusBadRequest, transactions.CodeBadRequest, err.Error())
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// The path is authoritative; the body may override the queue name only when
	// the path carries none.
	if queueName == "" {
		queueName = req.QueueName
	}
	if queueName == "" {
		writeError(w, d.Logger, http.StatusBadRequest, transactions.CodeBadRequest,
			"queue_id is required in the path")
		span.SetStatus(codes.Error, "missing queue_id")
		return
	}

	decl := models.QueueDeclaration{
		Name:       queueName,
		Durable:    req.Durable,
		AutoDelete: req.AutoDelete,
		Exclusive:  req.Exclusive,
		NoWait:     req.NoWait,
		Loggable:   req.Loggable,
		Tags:       req.Tags,
		Args:       req.Args,
	}

	queue, err := d.Services.AddServiceQueue(serviceID, decl)
	if err != nil {
		// ErrAlreadyExists now means the queue genuinely exists, because
		// GetServiceQueue reports ErrNotFound rather than (nil, nil).
		writeErr(w, d.Logger, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	d.Logger.Info("queue declared",
		zap.String("service_id", serviceID),
		zap.String("queue", queueName),
		zap.Bool("durable", req.Durable),
	)

	writeJSON(w, http.StatusCreated, queueResponse(queue))
	span.SetStatus(codes.Ok, "")
}

// GetQueueHandler returns one queue.
func GetQueueHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "GetQueue")
	defer span.End()

	serviceID := r.PathValue("service_id")
	queueName := r.PathValue("queue_id")

	queue, err := d.Services.GetServiceQueue(serviceID, queueName)
	if err != nil {
		writeErr(w, d.Logger, err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, queueResponse(queue))
	span.SetStatus(codes.Ok, "")
}

// RemoveQueueHandler drains a queue, deletes it from the broker, then removes
// it from the registry.
//
// The five-step contract was documented but never achieved: step 1 marked the
// queue "draining" and step 5 called a method that refused anything so marked,
// so the broker queue was deleted while the model kept it, permanently out of
// sync. The marker is now cleared between the broker delete and the registry
// delete.
func RemoveQueueHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "RemoveQueue")
	defer span.End()

	serviceID := r.PathValue("service_id")
	queueName := r.PathValue("queue_id")

	service, err := d.Services.GetService(serviceID)
	if err != nil {
		writeErr(w, d.Logger, err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// Step 1: mark draining so producers stop enqueuing.
	if err := service.SetQueueDraining(queueName); err != nil {
		writeErr(w, d.Logger, err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	drainCtx, cancel := context.WithTimeout(r.Context(), d.drainTimeout())
	defer cancel()

	// Steps 2-3: wait for the backlog to clear.
	if err := drainQueue(drainCtx, service, queueName, d.Logger); err != nil {
		writeError(w, d.Logger, drainStatus(err), drainCode(err),
			fmt.Sprintf("draining queue %s: %v", queueName, err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// Step 4: delete from the broker.
	if err := service.DeleteQueue(queueName); err != nil {
		writeError(w, d.Logger, http.StatusBadGateway, transactions.CodeBrokerError,
			fmt.Sprintf("deleting queue %s from broker: %v", queueName, err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// Clear the marker we set in step 1, so the removal below is permitted.
	if err := service.ClearQueueDraining(queueName); err != nil {
		d.Logger.Warn("could not clear draining marker",
			zap.String("queue", queueName), zap.Error(err))
	}

	// Step 5: remove from the registry.
	if err := d.Services.RemoveServiceQueue(serviceID, queueName); err != nil {
		writeErr(w, d.Logger, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	d.Logger.Info("queue removed",
		zap.String("service_id", serviceID),
		zap.String("queue", queueName),
	)

	w.WriteHeader(http.StatusNoContent)
	span.SetStatus(codes.Ok, "")
}

// RemoveServiceHandler drains every queue on a service, then removes it.
func RemoveServiceHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "RemoveService")
	defer span.End()

	serviceID := r.PathValue("service_id")

	service, err := d.Services.GetService(serviceID)
	if err != nil {
		writeErr(w, d.Logger, err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	if err := service.SetServiceDraining(); err != nil {
		writeErr(w, d.Logger, err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	drainCtx, cancel := context.WithTimeout(r.Context(), d.drainTimeout())
	defer cancel()

	// Drain each queue in turn. Snapshot first so the set is stable.
	queues := service.SnapshotQueue()
	for name := range queues {
		if err := drainQueue(drainCtx, service, name, d.Logger); err != nil {
			writeError(w, d.Logger, drainStatus(err), drainCode(err),
				fmt.Sprintf("draining queue %s: %v", name, err))
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return
		}
		if err := service.DeleteQueue(name); err != nil {
			writeError(w, d.Logger, http.StatusBadGateway, transactions.CodeBrokerError,
				fmt.Sprintf("deleting queue %s from broker: %v", name, err))
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return
		}
	}

	service.ClearServiceDraining()

	if err := d.Services.RemoveService(serviceID); err != nil {
		writeErr(w, d.Logger, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	d.Logger.Info("service removed", zap.String("service_id", serviceID))
	w.WriteHeader(http.StatusNoContent)
	span.SetStatus(codes.Ok, "")
}

// ---------------------------------------------------------------------------
// drain
// ---------------------------------------------------------------------------

// ErrDrainTimeout reports a backlog that did not clear within the deadline.
var ErrDrainTimeout = errors.New("drain deadline exceeded")

// drainQueue waits until a queue holds no messages.
//
// It respects ctx, so a client disconnect or the drain deadline ends the wait.
// The previous loop slept forever, holding a channel and an HTTP goroutine.
func drainQueue(ctx context.Context, service *models.Service, name string, logger *zap.Logger) error {
	logger = loggerOrNop(logger)

	for {
		pending, err := service.PendingMessages(name)
		if err != nil {
			return err
		}
		if pending == 0 {
			return nil
		}

		logger.Debug("draining queue",
			zap.String("queue", name),
			zap.Int("messages", pending),
		)

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: queue %s still held %d messages: %v",
				ErrDrainTimeout, name, pending, ctx.Err())
		case <-time.After(DrainPollInterval):
		}
	}
}

func drainStatus(err error) int {
	if errors.Is(err, ErrDrainTimeout) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func drainCode(err error) string {
	if errors.Is(err, ErrDrainTimeout) {
		return transactions.CodeDrainTimeout
	}
	return transactions.CodeBrokerError
}

func (d Deps) drainTimeout() time.Duration {
	if d.DrainTimeout > 0 {
		return d.DrainTimeout
	}
	return DefaultDrainTimeout
}

func loggerOrNop(l *zap.Logger) *zap.Logger {
	if l == nil {
		return zap.NewNop()
	}
	return l
}
