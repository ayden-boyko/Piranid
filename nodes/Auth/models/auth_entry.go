package models

import (
	sharedModels "Piranid/pkg/models"
	"time"
)

// AuthEntry extends Entry with auth-specific metadata
type AuthEntry struct {
	sharedModels.Entry        // Embedded
	ClientId           string `json:"client_id"`
	ClientSecret       string `json:"client_secret"`
	Username           string `json:"username"`
	UserEmail          string `json:"useremail"`
	HashedPassword     string `json:"hashed_password"`
	ServiceId          string `json:"service_id"`
	RedirectURI        string `json:"redirect_uri"`
}

func (e AuthEntry) GetID() (uint64, error) {
	return e.Entry.Id, nil
}

func (e AuthEntry) GetDateCreated() (*time.Time, error) {
	return &e.Entry.Date_Created, nil
}

func (e *AuthEntry) GetClientId() string {
	return e.ClientId
}

func (e *AuthEntry) GetClientSecret() string {
	return e.ClientSecret
}

func (e *AuthEntry) GetUsername() string {
	return e.Username
}

func (e *AuthEntry) GetUserEmail() string {
	return e.UserEmail
}

func (e *AuthEntry) GetHashedPassword() string {
	return e.HashedPassword
}

func (e *AuthEntry) GetServiceId() string {
	return e.ServiceId
}
