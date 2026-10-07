package models

import (
	sharedModels "Piranid/pkg/models"
	"time"
)

// AuthEntry is one registered OAuth client plus the user who owns it.
//
// ClientSecret and HashedPassword hold bcrypt hashes, never plaintext. The
// plaintext client secret is returned once at registration time and is not
// recoverable afterwards.
type AuthEntry struct {
	sharedModels.Entry        // Embedded
	ClientId           string `json:"client_id"`
	ClientSecret       string `json:"client_secret"` // bcrypt hash
	ClientName         string `json:"client_name"`
	Username           string `json:"username"`
	UserEmail          string `json:"user_email"`
	HashedPassword     string `json:"hashed_password"` // bcrypt hash
	ServiceId          string `json:"service_id"`
	RedirectURI        string `json:"redirect_uri"` // space-separated
}

func (e AuthEntry) GetID() (string, error) {
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

// RedirectURIs splits the stored redirect_uri list into individual URIs, so the
// token endpoint can compare the request against the registered set.
// Registration may register several; the authorize request must match one
// exactly (exact match, not prefix — prefix matching is a known redirect
// hijacking vector).
func (e *AuthEntry) RedirectURIs() []string {
	return splitSpaceList(e.RedirectURI)
}

// AllowsRedirect reports whether uri was registered for this client.
func (e *AuthEntry) AllowsRedirect(uri string) bool {
	for _, allowed := range e.RedirectURIs() {
		if allowed == uri {
			return true
		}
	}
	return false
}

func splitSpaceList(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}
