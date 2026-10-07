package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sharedModels "Piranid/pkg/models"
)

// ContactMethod names a delivery channel.
type ContactMethod string

const (
	Mobile ContactMethod = "Mobile"
	Email  ContactMethod = "Email"
	Slack  ContactMethod = "Slack"
)

// Importance bounds. A notification outside this range is rejected rather than
// silently coerced.
const (
	MinImportance int32 = 1
	MaxImportance int32 = 10
)

// Validation errors. Callers test these with errors.Is rather than matching
// message strings.
var (
	// ErrNoContact reports a missing delivery address.
	ErrNoContact = errors.New("notifications: contact is required")

	// ErrNoMethod reports a missing or unknown delivery method.
	ErrNoMethod = errors.New("notifications: method is required")

	// ErrUnknownMethod reports an unsupported delivery method.
	ErrUnknownMethod = errors.New("notifications: unsupported method")

	// ErrImportanceOutOfRange reports an importance outside 1-10.
	ErrImportanceOutOfRange = errors.New("notifications: importance out of range")

	// ErrNoData reports missing template data.
	ErrNoData = errors.New("notifications: data is required")
)

// NotifEntry is one notification, pending or delivered.
//
// It no longer embeds sharedModels.Entry, whose Id field is a string while the
// old schema declared id INTEGER PRIMARY KEY. The composite key
// (service_id, contact_info) is what the queries actually filter on.
type NotifEntry struct {
	ServiceId   string            `json:"service_id"`
	ContactInfo string            `json:"contact_info"`
	Method      ContactMethod     `json:"method"`
	Data        map[string]string `json:"data"`
	Importance  int32             `json:"importance"`
	Template    string            `json:"template"`
	Sent        bool              `json:"sent"`
	CreatedAt   time.Time         `json:"created_at"`
}

// ID implements the DataManager Entry interface.
func (e NotifEntry) GetID() (string, error) {
	if e.ServiceId == "" {
		return "", errors.New("notifications: service_id is required")
	}
	return e.ServiceId, nil
}

// GetDateCreated implements the DataManager Entry interface.
func (e NotifEntry) GetDateCreated() (*time.Time, error) {
	if e.CreatedAt.IsZero() {
		return nil, errors.New("notifications: created_at is required")
	}
	return &e.CreatedAt, nil
}

// ID returns the service identifier.
func (e NotifEntry) ID() string { return e.ServiceId }

// GetContact returns the delivery address.
func (e *NotifEntry) GetContact() (string, error) {
	if e.ContactInfo == "" {
		return "", ErrNoContact
	}
	return e.ContactInfo, nil
}

// GetMethod returns the validated delivery method.
//
// The handler used to switch on the raw string and default to an error, which
// meant a caller sending "email" in lower case was rejected. Matching is now
// case-insensitive.
func (e *NotifEntry) GetMethod() (ContactMethod, error) {
	if e.Method == "" {
		return "", ErrNoMethod
	}
	switch ContactMethod(normaliseMethod(string(e.Method))) {
	case Mobile:
		return Mobile, nil
	case Email:
		return Email, nil
	case Slack:
		return Slack, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownMethod, e.Method)
	}
}

// SetMethod validates and stores the delivery method.
func (e *NotifEntry) SetMethod(m ContactMethod) error {
	probe := NotifEntry{Method: m}
	validated, err := probe.GetMethod()
	if err != nil {
		return err
	}
	e.Method = validated
	return nil
}

// SetContact stores the delivery address.
func (e *NotifEntry) SetContact(contact string) error {
	if contact == "" {
		return ErrNoContact
	}
	e.ContactInfo = contact
	return nil
}

// GetData returns the template data.
func (e *NotifEntry) GetData() (map[string]string, error) {
	if e.Data == nil {
		return nil, ErrNoData
	}
	return e.Data, nil
}

// SetData stores the template data.
func (e *NotifEntry) SetData(d map[string]string) error {
	e.Data = d
	return nil
}

// GetImportance returns the validated importance.
func (e *NotifEntry) GetImportance() (int32, error) {
	return validateImportance(e.Importance)
}

// SetImportance validates and stores the importance.
//
// This existed as an unexported setImportance and was never called, so the
// documented 1-10 bound was never enforced on any path.
func (e *NotifEntry) SetImportance(v int32) error {
	validated, err := validateImportance(v)
	if err != nil {
		return err
	}
	e.Importance = validated
	return nil
}

// GetTemplate returns the explicitly set template, or "" if none.
func (e *NotifEntry) GetTemplate() (string, error) { return e.Template, nil }

// SetTemplate stores the template identifier.
func (e *NotifEntry) SetTemplate(t string) error {
	e.Template = t
	return nil
}

// ValidateIntegrity checks that an entry can be delivered.
//
// The template is deliberately NOT required here. The original version
// required it, but ConvertToNotifEntry hardcoded an empty template, so this
// function rejected every request the service could receive. Template resolution
// happens in the core, which can consult a configured source.
func (e NotifEntry) ValidateIntegrity() error {
	if _, err := e.GetID(); err != nil {
		return err
	}
	if _, err := e.GetDateCreated(); err != nil {
		return err
	}
	if _, err := e.GetContact(); err != nil {
		return err
	}
	if _, err := e.GetMethod(); err != nil {
		return err
	}
	if _, err := e.GetImportance(); err != nil {
		return err
	}
	return nil
}

// NotifColumns is the exact column list NotifScanner reads, in order. Pass it to
// GetEntry and ConsumeEntry as the SELECT column list.
var NotifColumns = []string{
	"service_id",
	"contact_info",
	"method",
	"message_data",
	"importance",
	"template",
	"sent",
	"created_at",
}

// MarshalData renders the template data for storage.
//
// database/sql cannot marshal a map[string]string, which is why the original
// inserter failed with "unsupported type map[string]string".
func (e NotifEntry) MarshalData() (string, error) {
	if e.Data == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(e.Data)
	if err != nil {
		return "", fmt.Errorf("marshalling notification data: %w", err)
	}
	return string(raw), nil
}

// UnmarshalData restores the template data.
func (e *NotifEntry) UnmarshalData(raw string) error {
	if raw == "" {
		e.Data = nil
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return fmt.Errorf("unmarshalling notification data: %w", err)
	}
	e.Data = out
	return nil
}

func validateImportance(v int32) (int32, error) {
	if v < MinImportance || v > MaxImportance {
		return 0, fmt.Errorf("%w: %d not in [%d, %d]",
			ErrImportanceOutOfRange, v, MinImportance, MaxImportance)
	}
	return v, nil
}

func normaliseMethod(s string) string {
	switch {
	case len(s) == 0:
		return s
	case equalFold(s, string(Mobile)):
		return string(Mobile)
	case equalFold(s, string(Email)):
		return string(Email)
	case equalFold(s, string(Slack)):
		return string(Slack)
	default:
		return s
	}
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// unused keeps the shared models import referenced; NotifEntry no longer embeds
// it because its Id field was incompatible with the schema.
var _ = sharedModels.Entry{}
