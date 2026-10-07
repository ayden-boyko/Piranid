package models

import (
	sharedModels "Piranid/pkg/models"
	"time"
)

// AuthCodeEntry is a single-use authorization code with its PKCE binding.
//
// The code is an opaque random string rather than a JWT. A self-contained code
// cannot be revoked before it expires, and the RFC requires the server to be
// able to reject an already-used code.
type AuthCodeEntry struct {
	sharedModels.Entry         // Embedded (Id and Date_Created unused; see GetID)
	Code                string `json:"code"`
	ClientId            string `json:"client_id"`
	Subject             string `json:"subject"`
	Scope               string `json:"scope"`
	RedirectURI         string `json:"redirect_uri"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	State               string `json:"state"`
	Nonce               string `json:"nonce"`
	IssuedAt            int64  `json:"issued_at"`
	ExpiresAt           int64  `json:"expires_at"`
	ConsumedAt          *int64 `json:"consumed_at,omitempty"`
}

func (e AuthCodeEntry) GetID() (string, error) {
	return e.Code, nil
}

// GetDateCreated derives the creation time from issued_at, since the code is
// keyed by its own value and carries no autoincrement id.
func (e AuthCodeEntry) GetDateCreated() (*time.Time, error) {
	t := time.Unix(e.IssuedAt, 0).UTC()
	return &t, nil
}

func (e *AuthCodeEntry) GetAuthCode() string {
	return e.Code
}

// IsExpired reports whether the code is past its lifetime. Authorization codes
// are short-lived (seconds, not hours): the code is only ever presented once,
// over a back channel, to obtain the real credential.
func (e *AuthCodeEntry) IsExpired(now time.Time) bool {
	return now.Unix() >= e.ExpiresAt
}

// AuthCodeColumns is the exact column list AuthCodeScanner reads, in order.
// GetEntry and ConsumeEntry must be given this same list.
var AuthCodeColumns = []string{
	"code",
	"client_id",
	"subject",
	"scope",
	"redirect_uri",
	"code_challenge",
	"code_challenge_method",
	"state",
	"nonce",
	"issued_at",
	"expires_at",
	"consumed_at",
}
