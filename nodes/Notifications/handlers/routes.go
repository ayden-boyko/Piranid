package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
	notifcore "github.com/ayden-boyko/Piranid/nodes/Notifications/notifcore"
	notifutils "github.com/ayden-boyko/Piranid/nodes/Notifications/utils"

	v1 "Piranid/pkg/proto/notifications/v1"

	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var tracer = otel.Tracer("notifications/handlers")

// NotificationHandler implements notifications.v1.Notifier.
type NotificationHandler struct {
	v1.UnimplementedNotifierServer
	NotificationNode *notifcore.NotificationNode
	Logger           *zap.Logger
}

// NewNotificationHandler builds the handler.
func NewNotificationHandler(node *notifcore.NotificationNode, logger *zap.Logger) *NotificationHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &NotificationHandler{NotificationNode: node, Logger: logger}
}

// RequestNotification delivers a notification over email, SMS or Slack.
//
// Returns either a response or an error, never both. The previous handlers
// returned a populated NotificationResponse together with a non-nil error, and
// gRPC discards the response whenever err is set, so every Status_FAILURE body
// was unreachable by clients.
func (h *NotificationHandler) RequestNotification(
	ctx context.Context,
	req *v1.NotificationRequest,
) (*v1.NotificationResponse, error) {
	_, span := tracer.Start(ctx, "RequestNotification")
	defer span.End()

	if req == nil {
		return failure(span, "request is required")
	}

	entry, err := notifutils.ConvertToNotifEntry(req)
	if err != nil {
		span.RecordError(err)
		return nil, statusError(codes.InvalidArgument, err.Error())
	}

	if err := h.NotificationNode.HandleNotifSend(ctx, entry); err != nil {
		h.Logger.Warn("notification delivery failed",
			zap.String("service_id", entry.ServiceId),
			zap.Error(err))
		span.RecordError(err)
		return nil, statusError(codes.Internal, err.Error())
	}

	// Persisting is best-effort: the notification was delivered, so a storage
	// failure must not be reported to the caller as a delivery failure.
	if err := h.NotificationNode.StoreNotif(ctx, entry); err != nil {
		h.Logger.Warn("could not persist notification record",
			zap.String("service_id", entry.ServiceId),
			zap.Error(err))
	}

	span.SetStatus(otelcodes.Ok, "")
	return &v1.NotificationResponse{Success: v1.Status_SUCCESS}, nil
}

// DeleteUser removes stored notifications for a service and user.
//
// Despite the name, this deletes notification rows. It is not account deletion.
func (h *NotificationHandler) DeleteUser(
	ctx context.Context,
	req *v1.NotificationRequest,
) (*v1.NotificationResponse, error) {
	_, span := tracer.Start(ctx, "DeleteUser")
	defer span.End()

	if req == nil || req.ServiceId == "" {
		return failure(span, "service_id is required")
	}
	if req.Username == "" {
		return failure(span, "username is required")
	}

	// Deletion keys on (service_id, contact_info) only. Routing a delete through
	// the delivery converter would demand a valid method and importance, which
	// are irrelevant to removing a row.
	entry := model.NotifEntry{
		ServiceId:   req.ServiceId,
		ContactInfo: req.Username,
	}

	if err := h.NotificationNode.RemoveNotif(ctx, entry); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, statusError(codes.NotFound,
				fmt.Sprintf("no notifications for service %q and user %q",
					req.ServiceId, req.Username))
		}
		span.RecordError(err)
		return nil, statusError(codes.Internal, err.Error())
	}

	span.SetStatus(otelcodes.Ok, "")
	return &v1.NotificationResponse{Success: v1.Status_SUCCESS}, nil
}

// RequestUserNotificationUpdate records a changed contact address.
func (h *NotificationHandler) RequestUserNotificationUpdate(
	ctx context.Context,
	req *v1.UserNotificationUpdate,
) (*v1.UserNotificationResponse, error) {
	_, span := tracer.Start(ctx, "RequestUserNotificationUpdate")
	defer span.End()

	if req == nil {
		return nil, statusError(codes.InvalidArgument, "request is required")
	}
	if req.ServiceId == "" {
		return nil, statusError(codes.InvalidArgument, "service_id is required")
	}
	if req.ContactInfo == "" {
		return nil, statusError(codes.InvalidArgument, "contact_info is required")
	}

	entry := notifEntryFromUpdate(req)

	// Upsert. A contact change is normally a re-registration of the same
	// (service_id, contact_info) pair, so a duplicate insert means "update",
	// not "fail".
	if err := h.NotificationNode.StoreNotif(ctx, entry); err != nil {
		if errors.Is(err, sql.ErrNoRows) || isUniqueViolation(err) {
			entry.Sent = true
			if err := h.NotificationNode.NotifSent(ctx, entry); err != nil {
				span.RecordError(err)
				return nil, statusError(codes.Internal, err.Error())
			}
		} else {
			span.RecordError(err)
			return nil, statusError(codes.Internal, err.Error())
		}
	}

	span.SetStatus(otelcodes.Ok, "")
	return &v1.UserNotificationResponse{Success: v1.Status_SUCCESS}, nil
}

// RequestTFA is NOT implemented.
//
// The previous implementation returned Status_SUCCESS unconditionally without
// generating, storing or sending anything. A caller gating a login on that
// response would believe a second factor had been delivered when none was, which
// is worse than an outright failure: the failure is loud, the false success is
// silent.
//
// See docs/NOTIFICATIONS_SERVICE.md for the architectural question of where a
// TFA challenge should actually be minted and verified. It belongs in the auth
// service, not in a notification contract.
func (h *NotificationHandler) RequestTFA(
	ctx context.Context,
	req *v1.TFARequest,
) (*v1.TFAResponse, error) {
	_, span := tracer.Start(ctx, "RequestTFA")
	defer span.End()

	err := errors.New(
		"two-factor delivery is not implemented: this RPC returns Unimplemented " +
			"rather than reporting success it did not achieve. Mint and verify TFA " +
			"challenges in the auth service")
	span.RecordError(err)
	h.Logger.Warn("RequestTFA called but not implemented",
		zap.String("username", req.GetUsername()))

	return nil, statusError(codes.Unimplemented, err.Error())
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// failure builds an InvalidArgument status error and marks the span.
func failure(span trace.Span, message string) (*v1.NotificationResponse, error) {
	span.SetStatus(otelcodes.Error, message)
	return nil, statusError(codes.InvalidArgument, message)
}

// statusError wraps a gRPC status code and message.
//
// Every handler returns either a response or an error, never both. gRPC discards
// the response whenever err is non-nil, so the previous pattern of returning a
// populated NotificationResponse alongside an error made every failure body
// unreachable by clients.
func statusError(code codes.Code, message string) error {
	return status.Error(code, message)
}

// notifEntryFromUpdate builds a record from a contact-address update.
//
// The previous handler populated only ContactInfo and Id, silently dropping the
// username and never applying the "@ means email, otherwise phone" heuristic the
// proto describes.
func notifEntryFromUpdate(req *v1.UserNotificationUpdate) model.NotifEntry {
	method := model.Email
	if !looksLikeEmail(req.ContactInfo) {
		method = model.Mobile
	}

	return model.NotifEntry{
		ServiceId:   req.ServiceId,
		ContactInfo: req.ContactInfo,
		Method:      method,
		Importance:  1,
		Data:        map[string]string{"username": req.Username},
		CreatedAt:   time.Now().UTC(),
	}
}

// isUniqueViolation reports a primary-key collision.
//
// SQLite reports these as "UNIQUE constraint failed: ...".
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// looksLikeEmail applies the proto's heuristic.
func looksLikeEmail(s string) bool {
	at := strings.Index(s, "@")
	return at > 0 && at < len(s)-1
}
