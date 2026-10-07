package utils

import (
	"time"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"

	v1 "Piranid/pkg/proto/notifications/v1"
)

// ConvertToNotifEntry maps a gRPC request onto the internal model.
//
// Two behaviours changed:
//
//   - Template is no longer hardcoded to "". The core resolves it from a
//     configured source, because the original empty value was the reason every
//     send failed with "TEMPLATE NIL".
//   - Method and Importance are validated. Previously an unsupported method
//     reached the Courier call, and importance was never range-checked because
//     the setter that checked it was unexported and uncalled.
//
// ContactInfo is taken from the request's username field. The proto names it
// username while the rest of the system treats it as an email or phone address;
// that mismatch is a known wart in the contract, documented rather than silently
// reinterpreted.
func ConvertToNotifEntry(req *v1.NotificationRequest) (model.NotifEntry, error) {
	if req == nil {
		return model.NotifEntry{}, model.ErrNoContact
	}

	entry := model.NotifEntry{
		ServiceId:   req.ServiceId,
		ContactInfo: req.Username,
		Method:      model.ContactMethod(req.Method),
		Data:        req.Data,
		Importance:  req.Importance,
		CreatedAt:   time.Now().UTC(),
	}

	if err := entry.ValidateIntegrity(); err != nil {
		return model.NotifEntry{}, err
	}

	// Write back the normalised method so downstream sees the canonical form.
	normalised, err := entry.GetMethod()
	if err != nil {
		return model.NotifEntry{}, err
	}
	entry.Method = normalised

	return entry, nil
}
