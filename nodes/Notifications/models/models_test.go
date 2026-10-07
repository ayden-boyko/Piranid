package models

import (
	"errors"
	"testing"
	"time"
)

func validEntry() NotifEntry {
	return NotifEntry{
		ServiceId:   "svc-1",
		ContactInfo: "alice@example.com",
		Method:      Email,
		Data:        map[string]string{"k": "v"},
		Importance:  5,
		CreatedAt:   time.Now().UTC(),
	}
}

// The converter hardcoded Template="", and ValidateIntegrity required a
// template, so every request the service could receive was rejected with
// "TEMPLATE NIL" and Courier was never reached.
func TestValidateIntegrityDoesNotRequireTemplate(t *testing.T) {
	e := validEntry()
	e.Template = ""

	if err := e.ValidateIntegrity(); err != nil {
		t.Fatalf("ValidateIntegrity rejected a valid entry with no template: %v", err)
	}
}

func TestValidateIntegrityAcceptsTemplate(t *testing.T) {
	e := validEntry()
	e.Template = "some-template"
	if err := e.ValidateIntegrity(); err != nil {
		t.Fatalf("ValidateIntegrity: %v", err)
	}
}

func TestValidateIntegrityRejectsMissingContact(t *testing.T) {
	e := validEntry()
	e.ContactInfo = ""
	if err := e.ValidateIntegrity(); !errors.Is(err, ErrNoContact) {
		t.Errorf("err = %v, want ErrNoContact", err)
	}
}

func TestValidateIntegrityRejectsMissingMethod(t *testing.T) {
	e := validEntry()
	e.Method = ""
	if err := e.ValidateIntegrity(); !errors.Is(err, ErrNoMethod) {
		t.Errorf("err = %v, want ErrNoMethod", err)
	}
}

func TestValidateIntegrityRejectsUnknownMethod(t *testing.T) {
	e := validEntry()
	e.Method = "carrier-pigeon"
	if err := e.ValidateIntegrity(); !errors.Is(err, ErrUnknownMethod) {
		t.Errorf("err = %v, want ErrUnknownMethod", err)
	}
}

func TestValidateIntegrityRejectsMissingServiceID(t *testing.T) {
	e := validEntry()
	e.ServiceId = ""
	if err := e.ValidateIntegrity(); err == nil {
		t.Error("ValidateIntegrity accepted an entry with no service_id")
	}
}

func TestValidateIntegrityRejectsMissingCreatedAt(t *testing.T) {
	e := validEntry()
	e.CreatedAt = time.Time{}
	if err := e.ValidateIntegrity(); err == nil {
		t.Error("ValidateIntegrity accepted an entry with no created_at")
	}
}

// The handler switched on the raw string and defaulted to an error, so a caller
// sending "email" in lower case was rejected.
func TestGetMethodIsCaseInsensitive(t *testing.T) {
	for _, in := range []string{"Email", "email", "EMAIL", "eMaIl"} {
		e := NotifEntry{Method: ContactMethod(in)}
		got, err := e.GetMethod()
		if err != nil {
			t.Errorf("GetMethod(%q): %v", in, err)
			continue
		}
		if got != Email {
			t.Errorf("GetMethod(%q) = %q, want Email", in, got)
		}
	}
	for _, in := range []string{"Mobile", "mobile", "SLACK", "slack"} {
		e := NotifEntry{Method: ContactMethod(in)}
		if _, err := e.GetMethod(); err != nil {
			t.Errorf("GetMethod(%q): %v", in, err)
		}
	}
}

func TestSetMethodValidates(t *testing.T) {
	e := NotifEntry{}
	if err := e.SetMethod("nonsense"); !errors.Is(err, ErrUnknownMethod) {
		t.Errorf("SetMethod(nonsense) = %v, want ErrUnknownMethod", err)
	}
	if err := e.SetMethod("mobile"); err != nil {
		t.Fatalf("SetMethod(mobile): %v", err)
	}
	if e.Method != Mobile {
		t.Errorf("Method = %q, want Mobile (normalised)", e.Method)
	}
}

// setImportance was unexported and never called, so the documented 1-10 bound
// was never enforced on any path.
func TestImportanceBoundsAreEnforced(t *testing.T) {
	e := NotifEntry{}

	for _, bad := range []int32{0, -1, 11, 1000} {
		if err := e.SetImportance(bad); !errors.Is(err, ErrImportanceOutOfRange) {
			t.Errorf("SetImportance(%d) = %v, want ErrImportanceOutOfRange", bad, err)
		}
	}
	for _, ok := range []int32{1, 5, 10} {
		if err := e.SetImportance(ok); err != nil {
			t.Errorf("SetImportance(%d): %v", ok, err)
		}
		if _, err := e.GetImportance(); err != nil {
			t.Errorf("GetImportance after SetImportance(%d): %v", ok, err)
		}
	}
}

func TestSetContactRejectsEmpty(t *testing.T) {
	e := NotifEntry{}
	if err := e.SetContact(""); !errors.Is(err, ErrNoContact) {
		t.Errorf("SetContact(\"\") = %v, want ErrNoContact", err)
	}
	if err := e.SetContact("bob@example.com"); err != nil {
		t.Fatalf("SetContact: %v", err)
	}
	if e.ContactInfo != "bob@example.com" {
		t.Errorf("ContactInfo = %q", e.ContactInfo)
	}
}

// database/sql cannot marshal a map, which is why the original inserter failed
// with "unsupported type map[string]string".
func TestDataMarshalRoundTrip(t *testing.T) {
	e := validEntry()

	raw, err := e.MarshalData()
	if err != nil {
		t.Fatalf("MarshalData: %v", err)
	}

	var back NotifEntry
	if err := back.UnmarshalData(raw); err != nil {
		t.Fatalf("UnmarshalData: %v", err)
	}
	if back.Data["k"] != "v" {
		t.Errorf("Data = %v, want k=v", back.Data)
	}
}

func TestMarshalDataNilMap(t *testing.T) {
	e := NotifEntry{}
	raw, err := e.MarshalData()
	if err != nil {
		t.Fatalf("MarshalData: %v", err)
	}
	if raw != "{}" {
		t.Errorf("raw = %q, want {}", raw)
	}
}

func TestUnmarshalDataInvalid(t *testing.T) {
	var e NotifEntry
	if err := e.UnmarshalData("{not json"); err == nil {
		t.Error("UnmarshalData accepted malformed JSON")
	}
}

func TestUnmarshalDataEmpty(t *testing.T) {
	var e NotifEntry
	if err := e.UnmarshalData(""); err != nil {
		t.Errorf("UnmarshalData(\"\"): %v", err)
	}
	if e.Data != nil {
		t.Errorf("Data = %v, want nil", e.Data)
	}
}

func TestNotifColumnsMatchTheModel(t *testing.T) {
	// Guards against the SELECT/scanner arity mismatch class of bug: the column
	// list and the struct must stay in agreement.
	want := 8
	if len(NotifColumns) != want {
		t.Errorf("len(NotifColumns) = %d, want %d", len(NotifColumns), want)
	}
	if NotifColumns[0] != "service_id" {
		t.Errorf("first column = %q, want service_id", NotifColumns[0])
	}
	if NotifColumns[1] != "contact_info" {
		t.Errorf("second column = %q, want contact_info", NotifColumns[1])
	}
}

func TestGetIDRequiresServiceID(t *testing.T) {
	e := NotifEntry{}
	if _, err := e.GetID(); err == nil {
		t.Error("GetID accepted an empty service_id")
	}
	e.ServiceId = "svc"
	if _, err := e.GetID(); err != nil {
		t.Errorf("GetID: %v", err)
	}
}

func TestGetDateCreatedRequiresTimestamp(t *testing.T) {
	e := NotifEntry{}
	if _, err := e.GetDateCreated(); err == nil {
		t.Error("GetDateCreated accepted a zero timestamp")
	}
	e.CreatedAt = time.Now()
	if _, err := e.GetDateCreated(); err != nil {
		t.Errorf("GetDateCreated: %v", err)
	}
}
