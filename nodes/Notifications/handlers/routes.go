package handlers

import (
	v1 "Piranid/pkg/proto/notifications/v1"
	"context"
	"errors"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"

	core "github.com/ayden-boyko/Piranid/nodes/Notifications/notifcore"
)

type NotificationHandler struct {
	v1.UnimplementedNotifierServer
	NotificationNode *core.NotificationNode
}

// TODO Caching

func NewNotificationHandler(node *core.NotificationNode) *NotificationHandler {
	return &NotificationHandler{NotificationNode: node}
}

func (h *NotificationHandler) RequestNotification(ctx context.Context, req *v1.NotificationRequest) (*v1.NotificationResponse, error) {
	var responseMessage string
	var notifReq = &model.NotifEntry{}

	notifReq.Entry.Id = req.ServiceId
	notifReq.ContactInfo = req.Username
	notifReq.Importance = req.Importance

	switch req.Method {
	case "Mobile":
		notifReq.Method = model.Mobile
	case "Email":
		notifReq.Method = model.Email
	// handle other cases as needed
	default:
		responseMessage = "Invalid method"
		return &v1.NotificationResponse{Success: v1.Status_FAILURE, ResponseMessage: &responseMessage}, errors.New("Invalid method")
	}

	err := h.NotificationNode.HandleNotifSend(ctx, *notifReq)
	if err != nil {
		responseMessage = err.Error()
		return &v1.NotificationResponse{Success: v1.Status_FAILURE, ResponseMessage: &responseMessage}, err
	}

	responseMessage = "Notification sent successfully"
	return &v1.NotificationResponse{Success: v1.Status_SUCCESS, ResponseMessage: &responseMessage}, nil
}

// !Will need to convert NotificationRequest to NotifEntry type
// TODO fill these out VVVV

func (h *NotificationHandler) DeleteUser(ctx context.Context, req *v1.NotificationRequest) (*v1.NotificationResponse, error) {
	var responseMessage string

	responseMessage = "User deleted successfully"
	return &v1.NotificationResponse{Success: v1.Status_SUCCESS, ResponseMessage: &responseMessage}, nil
}

func (h *NotificationHandler) RequestUserNotificationUpdate(ctx context.Context, req *v1.UserNotificationUpdate) (*v1.UserNotificationResponse, error) {
	var responseMessage string

	responseMessage = "User notification updated successfully"
	return &v1.UserNotificationResponse{Success: v1.Status_SUCCESS, ResponseMessage: &responseMessage}, nil
}

// TODO requires P2P interface with auth service
func (h *NotificationHandler) RequestTFA(ctx context.Context, req *v1.TFARequest) (*v1.TFAResponse, error) {

	return &v1.TFAResponse{ServiceId: h.NotificationNode.Service_ID, Username: req.Username, Success: v1.Status_SUCCESS}, nil
}
