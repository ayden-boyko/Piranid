package handlers

import (
	v1 "Piranid/pkg/proto/notifications/v1"
	"context"
	"errors"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
	"github.com/ayden-boyko/Piranid/nodes/Notifications/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	core "github.com/ayden-boyko/Piranid/nodes/Notifications/notifcore"

	telemetry "Piranid/pkg/telemetry"
)

type NotificationHandler struct {
	v1.UnimplementedNotifierServer
	NotificationNode *core.NotificationNode
	Logger           *zap.Logger
}

var tracer = otel.Tracer("notifications/handlers")

// TODO Caching

func NewNotificationHandler(node *core.NotificationNode, logger *zap.Logger) *NotificationHandler {
	return &NotificationHandler{NotificationNode: node, Logger: logger}
}

func (h *NotificationHandler) RequestNotification(ctx context.Context, req *v1.NotificationRequest) (*v1.NotificationResponse, error) {
	ctx, span := tracer.Start(ctx, "RequestNotification")
	defer span.End()

	h.Logger.Info("Received notification request")

	var responseMessage string
	notifReq, err := utils.ConvertToNotifEntry(req)
	if err != nil {
		h.Logger.Error("Failed to convert request to notification entry", zap.Error(err))
		telemetry.WithTraceID(ctx, h.Logger).Error("Failed to convert request to notification entry", zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		responseMessage = err.Error()
		return &v1.NotificationResponse{Success: v1.Status_FAILURE, ResponseMessage: &responseMessage}, err
	}

	switch req.Method {
	case "Mobile":
		notifReq.Method = model.Mobile
	case "Email":
		notifReq.Method = model.Email
	// handle other cases as needed
	default:
		errs := errors.New("Invalid notification method")
		h.Logger.Error("Invalid notification method", zap.Error(errs))
		telemetry.WithTraceID(ctx, h.Logger).Error("Invalid notification method", zap.Error(errs))
		span.RecordError(errs)
		span.SetStatus(codes.Error, errs.Error())
		responseMessage = "Invalid method"
		return &v1.NotificationResponse{Success: v1.Status_FAILURE, ResponseMessage: &responseMessage}, errors.New("Invalid method")
	}

	err = h.NotificationNode.HandleNotifSend(ctx, *notifReq)
	if err != nil {
		h.Logger.Error("Failed to send notification", zap.Error(err))
		telemetry.WithTraceID(ctx, h.Logger).Error("Failed to send notification", zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		responseMessage = err.Error()
		return &v1.NotificationResponse{Success: v1.Status_FAILURE, ResponseMessage: &responseMessage}, err
	}

	h.Logger.Info("Notification sent successfully")

	span.SetStatus(codes.Ok, "")
	responseMessage = "Notification sent successfully"
	return &v1.NotificationResponse{Success: v1.Status_SUCCESS, ResponseMessage: &responseMessage}, nil
}

func (h *NotificationHandler) DeleteUser(ctx context.Context, req *v1.NotificationRequest) (*v1.NotificationResponse, error) {
	ctx, span := tracer.Start(ctx, "DeleteUser")
	defer span.End()

	h.Logger.Info("Received delete user request")
	telemetry.WithTraceID(ctx, h.Logger).Info("Received delete user request")

	var responseMessage string

	notifReq, err := utils.ConvertToNotifEntry(req)
	if err != nil {
		h.Logger.Error("Failed to convert request to notification entry", zap.Error(err))
		telemetry.WithTraceID(ctx, h.Logger).Error("Failed to convert request to notification entry", zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		responseMessage = err.Error()
		return &v1.NotificationResponse{Success: v1.Status_FAILURE, ResponseMessage: &responseMessage}, err
	}

	err = h.NotificationNode.RemoveNotif(ctx, *notifReq)
	if err != nil {
		h.Logger.Error("Failed to delete user", zap.Error(err))
		telemetry.WithTraceID(ctx, h.Logger).Error("Failed to delete user", zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		responseMessage = err.Error()
		return &v1.NotificationResponse{Success: v1.Status_FAILURE, ResponseMessage: &responseMessage}, err
	}

	h.Logger.Info("User deleted successfully")
	telemetry.WithTraceID(ctx, h.Logger).Info("User deleted successfully")
	span.SetStatus(codes.Ok, "")
	responseMessage = "User deleted successfully"
	return &v1.NotificationResponse{Success: v1.Status_SUCCESS, ResponseMessage: &responseMessage}, nil
}

func (h *NotificationHandler) RequestUserNotificationUpdate(ctx context.Context, req *v1.UserNotificationUpdate) (*v1.UserNotificationResponse, error) {
	ctx, span := tracer.Start(ctx, "RequestUserNotificationUpdate")
	defer span.End()

	h.Logger.Info("Received user notification update request")
	telemetry.WithTraceID(ctx, h.Logger).Info("Received user notification update request")

	var responseMessage string
	notifReq := &model.NotifEntry{}

	notifReq.ContactInfo = req.ContactInfo
	notifReq.Id = req.ServiceId

	err := h.NotificationNode.NotifSent(ctx, *notifReq)
	if err != nil {
		h.Logger.Error("Failed to update user notification", zap.Error(err))
		telemetry.WithTraceID(ctx, h.Logger).Error("Failed to update user notification", zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		responseMessage = err.Error()
		return &v1.UserNotificationResponse{Success: v1.Status_FAILURE, ResponseMessage: &responseMessage}, err
	}

	h.Logger.Info("User notification updated successfully")
	telemetry.WithTraceID(ctx, h.Logger).Info("User notification updated successfully")
	span.SetStatus(codes.Ok, "")
	responseMessage = "User notification updated successfully"
	return &v1.UserNotificationResponse{Success: v1.Status_SUCCESS, ResponseMessage: &responseMessage}, nil
}

// TODO requires P2P interface with auth service
func (h *NotificationHandler) RequestTFA(ctx context.Context, req *v1.TFARequest) (*v1.TFAResponse, error) {
	ctx, span := tracer.Start(ctx, "RequestTFA")
	defer span.End()

	return &v1.TFAResponse{ServiceId: h.NotificationNode.Service_ID, Username: req.Username, Success: v1.Status_SUCCESS}, nil
}
