package authn

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the payload of a Piranid access token (JWT, RFC 7519).
//
// The original implementation put only client_id, iat and exp in the token,
// which left a verifier with nothing to check but the signature and the
// expiry. These claims are the contract a service verifies against.
type Claims struct {
	// Issuer. Identifies the auth node that minted the token. Verifiers pin
	// this, so a token signed by a different deployment is rejected even if
	// the signing key is somehow shared.
	Issuer string `json:"iss"`

	// Audience. Identifies the service the token is intended for. A token
	// minted for one service must not be accepted by another.
	Audience string `json:"aud"`

	// Subject. The username of the authenticated resource owner.
	Subject string `json:"sub"`

	// ExpiresAt. Unix seconds. Rejected once reached.
	ExpiresAt int64 `json:"exp"`

	// IssuedAt. Unix seconds.
	IssuedAt int64 `json:"iat"`

	// NotBefore. Unix seconds. Rejected before reached. Usually equal to
	// IssuedAt; present so a service can require that a token was minted
	// after, for example, a password change.
	NotBefore int64 `json:"nbf"`

	// JTI. Unique token id. Lets a service detect replay or support revocation
	// by id without keeping a deny-list of whole tokens.
	JTI string `json:"jti"`

	// Scope. Space-separated list of granted scopes (RFC 6749 3.3).
	Scope string `json:"scope,omitempty"`

	// ClientID. The OAuth client the token was issued to.
	ClientID string `json:"client_id"`

	// TokenType. Distinguishes access tokens from other token kinds this
	// authorization server might issue, so a refresh token cannot be replayed
	// as an access token.
	TokenType string `json:"token_use"`
}

// Token type values for Claims.TokenType.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// The methods below satisfy golang-jwt's Claims interface. The library needs
// them to parse a token into this struct; the library's own validation of them
// is disabled in the verifier (jwt.WithoutClaimsValidation) so that each
// failure surfaces as a distinct, matchable error from VerifyClaims.

// GetExpirationTime implements jwt.Claims.
func (c Claims) GetExpirationTime() (*jwt.NumericDate, error) {
	return numericDate(c.ExpiresAt), nil
}

// GetIssuedAt implements jwt.Claims.
func (c Claims) GetIssuedAt() (*jwt.NumericDate, error) {
	return numericDate(c.IssuedAt), nil
}

// GetNotBefore implements jwt.Claims.
func (c Claims) GetNotBefore() (*jwt.NumericDate, error) {
	if c.NotBefore == 0 {
		return nil, nil
	}
	return numericDate(c.NotBefore), nil
}

// GetIssuer implements jwt.Claims.
func (c Claims) GetIssuer() (string, error) { return c.Issuer, nil }

// GetSubject implements jwt.Claims.
func (c Claims) GetSubject() (string, error) { return c.Subject, nil }

// GetAudience implements jwt.Claims.
func (c Claims) GetAudience() (jwt.ClaimStrings, error) {
	if c.Audience == "" {
		return nil, nil
	}
	return jwt.ClaimStrings{c.Audience}, nil
}

// numericDate converts Unix seconds to the library's NumericDate. A zero
// timestamp maps to nil so the library treats the claim as absent.
func numericDate(sec int64) *jwt.NumericDate {
	if sec == 0 {
		return nil
	}
	return jwt.NewNumericDate(time.Unix(sec, 0))
}

// Scopes splits the space-separated scope string.
func (c *Claims) Scopes() []string {
	if c.Scope == "" {
		return nil
	}
	return strings.Fields(c.Scope)
}

// HasScope reports whether the token carries the named scope.
func (c *Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes() {
		if s == scope {
			return true
		}
	}
	return false
}

// Claim verification errors. These are distinct types so a caller can tell a
// bad signature from a wrong audience without string matching.
var (
	ErrInvalidIssuer   = errors.New("authn: issuer mismatch")
	ErrInvalidAudience = errors.New("authn: audience mismatch")
	ErrExpired         = errors.New("authn: token expired")
	ErrNotYetValid     = errors.New("authn: token not yet valid")
	ErrWrongTokenType  = errors.New("authn: unexpected token type")
	ErrMissingClaim    = errors.New("authn: required claim is missing")
)

// VerifyClaims checks the claims that do not depend on the signature.
//
// issuer and audience are compared exactly. The previous implementation had
// neither claim at all, so any token minted by any issuer was accepted by any
// service, provided the shared HMAC secret happened to match.
//
// leeway is subtracted from expiry and added to not-before, to tolerate modest
// clock skew between nodes. Piranid runs on a Raspberry Pi cluster where the
// nodes' clocks are not NTP-synchronized tightly.
func (c *Claims) VerifyClaims(expectedIssuer, expectedAudience string, leeway int64) error {
	if c.Subject == "" {
		return fmt.Errorf("%w: sub", ErrMissingClaim)
	}
	if c.ExpiresAt == 0 {
		return fmt.Errorf("%w: exp", ErrMissingClaim)
	}

	if expectedIssuer != "" && c.Issuer != expectedIssuer {
		return fmt.Errorf("%w: got %q, want %q", ErrInvalidIssuer, c.Issuer, expectedIssuer)
	}

	if expectedAudience != "" && c.Audience != expectedAudience {
		return fmt.Errorf("%w: got %q, want %q", ErrInvalidAudience, c.Audience, expectedAudience)
	}

	now := nowUnix()

	if c.ExpiresAt+leeway <= now {
		return fmt.Errorf("%w: expired at %d, now %d", ErrExpired, c.ExpiresAt, now)
	}
	if c.NotBefore != 0 && c.NotBefore-leeway > now {
		return fmt.Errorf("%w: valid from %d, now %d", ErrNotYetValid, c.NotBefore, now)
	}

	return nil
}

// RequireTokenType returns ErrWrongTokenType unless the token is of the
// expected kind. Without this, a token issued for one purpose could be
// presented where another is expected.
func (c *Claims) RequireTokenType(want string) error {
	if c.TokenType != want {
		return fmt.Errorf("%w: got %q, want %q", ErrWrongTokenType, c.TokenType, want)
	}
	return nil
}
