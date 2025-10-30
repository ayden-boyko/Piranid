package models

import (
	sharedModels "Piranid/pkg/models"
)

// AuthEntry extends Entry with auth-specific metadata
type AuthCodeEntry struct {
	sharedModels.Entry        // Embedded
	AuthCode           string `json:"auth_code"`
	Expires            int64  `json:"expires"`
}

func (e AuthCodeEntry) GetAuthCode() string {
	return e.AuthCode
}

func (e AuthCodeEntry) GetExpires() int64 {
	return e.Expires
}
