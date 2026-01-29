package utils

import (
	v1 "Piranid/pkg/proto/notifications/v1"
	"time"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
)

/*
 1. 	ServiceId     string
 2. 	Username      string
 3. 	Method        string
 4. 	Data          map[string]string
 5. 	Importance    int32
*/

func ConvertToNotifEntry(req *v1.NotificationRequest) (*model.NotifEntry, error) {
	result := &model.NotifEntry{}

	result.Entry.Id = req.ServiceId
	result.Entry.Date_Created = time.Now() // TODO, use NTP, NOT LOCAL SERVER TIME
	result.ContactInfo = req.Username
	result.Method = model.ContactMethod(req.Method)
	result.Data = req.Data
	result.Importance = req.Importance
	result.Template = "" //TODO, create templates based on method (i.e. email, mobile, etc...)

	return result, nil
}
