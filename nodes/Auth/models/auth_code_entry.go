package models

import (
	sharedModels "Piranid/pkg/models"
	"time"
)

// AuthEntry extends Entry with auth-specific metadata
type AuthCodeEntry struct {
	sharedModels.Entry        // Embedded
	AuthCode           string `json:"auth_code"`
	Expires            int64  `json:"expires"`
}

func (e AuthCodeEntry) GetID() (uint64, error) {
	return e.Entry.Id, nil
}

func (e AuthCodeEntry) GetDateCreated() (*time.Time, error) {
	return &e.Entry.Date_Created, nil
}

func (e *AuthCodeEntry) GetAuthCode() string {
	return e.AuthCode
}

func (e *AuthCodeEntry) GetExpires() int64 {
	return e.Expires
}
