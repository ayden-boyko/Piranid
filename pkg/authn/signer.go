package authn

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SigningAlgorithm is the only JWT signing algorithm this package emits or
// accepts.
//
// It was HS256 with a shared JWT_SECRET. That has two problems: the same
// secret let every service that verified tokens also mint them, and a JWKS
// document has no way to publish a symmetric key without publishing the secret
// itself. RS256 lets verification be public.
const SigningAlgorithm = "RS256"

// ErrNoSigner is returned when a Signer was built without a key.
var ErrNoSigner = errors.New("authn: signer has no key")

// Signer mints RS256 access tokens with one key pair.
//
// It is used only by the authorization server. Services verify, they do not
// sign.
type Signer struct {
	pair *KeyPair
	ttl  time.Duration
}

// NewSigner builds a Signer for the given key pair. A ttl of zero selects
// DefaultAccessTokenTTL.
func NewSigner(pair *KeyPair, ttl time.Duration) (*Signer, error) {
	if pair == nil || pair.Private == nil || pair.Public == nil {
		return nil, ErrNoSigner
	}
	if ttl <= 0 {
		ttl = DefaultAccessTokenTTL
	}
	return &Signer{pair: pair, ttl: ttl}, nil
}

// KeyID returns the kid of the signing key, for the token header and the JWKS.
func (s *Signer) KeyID() string { return s.pair.Kid }

// PublicKey returns the verification key.
func (s *Signer) PublicKey() *KeyPair { return s.pair }

// TTL returns the access token lifetime.
func (s *Signer) TTL() time.Duration { return s.ttl }

// JWKS renders the public half of the signing key as a key set.
func (s *Signer) JWKS() JWKS { return NewJWKS(s.pair) }

// IssueRequest carries the per-token values that vary.
type IssueRequest struct {
	Subject   string
	ClientID  string
	Scope     string
	TokenType string

	// TTL overrides the signer's default when non-zero.
	TTL time.Duration

	// Issuer and Audience default to the signer's configured values.
	Issuer   string
	Audience string
}

// Issue signs an access token.
//
// The returned string is the compact JWS. ttl and expiresAt describe its
// lifetime.
func (s *Signer) Issue(req IssueRequest) (token string, expiresAt int64, err error) {
	if s == nil || s.pair == nil || s.pair.Private == nil {
		return "", 0, ErrNoSigner
	}
	if req.Subject == "" {
		return "", 0, fmt.Errorf("%w: sub", ErrMissingClaim)
	}

	ttl := req.TTL
	if ttl <= 0 {
		ttl = s.ttl
	}
	tokenType := req.TokenType
	if tokenType == "" {
		tokenType = TokenTypeAccess
	}

	jti, err := NewJTI()
	if err != nil {
		return "", 0, err
	}

	now := nowUnix()
	claims := Claims{
		Issuer:    req.Issuer,
		Audience:  req.Audience,
		Subject:   req.Subject,
		IssuedAt:  now,
		NotBefore: now,
		ExpiresAt: now + int64(ttl.Seconds()),
		JTI:       jti,
		Scope:     req.Scope,
		ClientID:  req.ClientID,
		TokenType: tokenType,
	}

	t := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// The kid header tells a verifier which JWKS key to check the signature
	// with. Without it, a verifier holding several keys must try all of them.
	t.Header["kid"] = s.pair.Kid
	t.Header["typ"] = "JWT"

	signed, err := t.SignedString(s.pair.Private)
	if err != nil {
		return "", 0, fmt.Errorf("authn: signing token: %w", err)
	}

	return signed, claims.ExpiresAt, nil
}
