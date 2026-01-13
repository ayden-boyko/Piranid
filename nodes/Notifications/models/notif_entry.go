package models

import (
	sharedModels "Piranid/pkg/models"
	"errors"
)

// TODO double check that methods are courier compatable
type contactMethod string

const (
	Mobile contactMethod = "Mobile"
	Email  contactMethod = "Email"
	// todo discord, whatsapp, etc (magnum opus: ai agent with voice)?
)

// AuthEntry extends Entry with auth-specific metadata
type NotifEntry struct {
	sharedModels.Entry               // Embedded
	Contact            string        `json:"contact"`
	Method             contactMethod `json:"method"`
	Info               string        `json:"info"`
	Importance         uint32        `json:"importance"`
	Template           string        `json:"template"`
}

func (e *NotifEntry) GetContact() (string, error) {
	if e.Contact == "" {
		return "", errors.New("CONTACT NIL")
	}
	return e.Contact, nil
}

func (e *NotifEntry) GetMethod() (contactMethod, error) {
	if e.Method == "" {
		return "", errors.New("METHOD NIL")
	}
	return e.Method, nil
}

func (e *NotifEntry) SetMethod(method contactMethod) error {
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
	e.Contact = info
	return nil
}

func (e *NotifEntry) GetInfo() (string, error) {
	if e.Info == "" {
		return "", errors.New("INFO NIL")
	}
	return e.Info, nil
}

func (e *NotifEntry) SetInfo(data string) error {
	if data == "" {
		return errors.New("INFO SET NIL")
	}
	e.Info = data
	return nil
}

func (e *NotifEntry) GetImportance() (uint32, error) {
	if e.Importance == 0 {
		return 0, errors.New("IMPORTANCE NIL")
	}
	return e.Importance, nil
}

func (e *NotifEntry) setImportance(number uint32) error {
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

	_, err = e.GetInfo()
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
