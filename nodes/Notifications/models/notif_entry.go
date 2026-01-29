package models

import (
	sharedModels "Piranid/pkg/models"
	"errors"
)

// TODO double check that methods are courier compatable
type ContactMethod string

const (
	Mobile ContactMethod = "Mobile"
	Email  ContactMethod = "Email"
	// todo discord, whatsapp, etc (magnum opus: ai agent with voice)?
)

// AuthEntry extends Entry with auth-specific metadata
type NotifEntry struct {
	sharedModels.Entry                   // Embedded
	ContactInfo        string            `json:"contact_info"`
	Method             ContactMethod     `json:"method"`
	Data               map[string]string `json:"data"`
	Importance         int32             `json:"importance"`
	Template           string            `json:"template"`
}

func (e *NotifEntry) GetContact() (string, error) {
	if e.ContactInfo == "" {
		return "", errors.New("CONTACT NIL")
	}
	return e.ContactInfo, nil
}

func (e *NotifEntry) GetMethod() (ContactMethod, error) {
	if e.Method == "" {
		return "", errors.New("METHOD NIL")
	}
	return e.Method, nil
}

func (e *NotifEntry) SetMethod(method ContactMethod) error {
	if method == "" {
		return errors.New("METHOD SET NIL")
	}
	e.Method = method
	return nil
}

func (e *NotifEntry) SetContact(info string) error {
	if info == "" {
		return errors.New("CONTACT SET NIL")
	}
	e.ContactInfo = info
	return nil
}

func (e *NotifEntry) GetData() (map[string]string, error) {
	if len(e.Data) == 0 {
		return nil, errors.New("INFO NIL")
	}
	return e.Data, nil
}

func (e *NotifEntry) SetData(data map[string]string) error {
	if len(data) == 0 {
		return errors.New("INFO SET NIL")
	}
	e.Data = data
	return nil
}

func (e *NotifEntry) GetImportance() (int32, error) {
	if e.Importance == 0 {
		return 0, errors.New("IMPORTANCE NIL")
	}
	return e.Importance, nil
}

func (e *NotifEntry) setImportance(number int32) error {
	if number < 1 || number > 10 {
		return errors.New("IMPORTANCE NOT WITHIN BOUNDS (1-10)")
	}
	e.Importance = number
	return nil
}

func (e *NotifEntry) GetTemplate() (string, error) {
	if e.Template == "" {
		return "", errors.New("TEMPLATE NIL")
	}
	return e.Template, nil
}

func (e *NotifEntry) SetTemplate(template string) error {
	if template == "" {
		return errors.New("TEMPLATE SET NIL")
	}
	e.Template = template
	return nil
}

func (e *NotifEntry) ValidateIntegrity() error {
	_, err := e.GetID()
	if err != nil {
		return err
	}

	_, err = e.GetDateCreated()
	if err != nil {
		return err
	}

	_, err = e.GetContact()
	if err != nil {
		return err
	}

	_, err = e.GetMethod()
	if err != nil {
		return err
	}

	_, err = e.GetData()
	if err != nil {
		return err
	}

	_, err = e.GetImportance()
	if err != nil {
		return err
	}

	_, err = e.GetTemplate()
	if err != nil {
		return err
	}

	return nil
}
