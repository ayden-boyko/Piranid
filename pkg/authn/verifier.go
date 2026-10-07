package authn

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Verification errors.
var (
	// ErrUnsignedAlgorithm means the token declared "alg":"none".
	ErrUnsignedAlgorithm = errors.New("authn: token uses the unsigned 'none' algorithm")

	// ErrUnexpectedAlgorithm means the token declared an algorithm other than
	// RS256, most importantly HMAC. This is the classic JWT confusion attack:
	// an attacker signs a token with HS256 using the server's *public* key as
	// the HMAC secret, and a verifier that dispatches on the token's own alg
	// header will accept it.
	ErrUnexpectedAlgorithm = errors.New("authn: unexpected signing algorithm")
)

// KeySource supplies verification keys. A static set is enough for a single
// auth node; a JWKS fetcher keeps a service working across key rotation.
type KeySource interface {
	// KeyFor returns the public key for a kid, or an error wrapping
	// ErrNoMatchingKey if it is unknown.
	KeyFor(kid string) (*rsa.PublicKey, error)
}

// StaticKeySource serves a fixed set of keys. The authorization node uses this.
type StaticKeySource struct {
	set JWKS
}

// NewStaticKeySource builds a KeySource from a key set.
func NewStaticKeySource(set JWKS) *StaticKeySource { return &StaticKeySource{set: set} }

// KeyFor implements KeySource.
func (s *StaticKeySource) KeyFor(kid string) (*rsa.PublicKey, error) { return s.set.KeyByID(kid) }

// HasKey implements the optional rotation probe used by the middleware.
func (s *StaticKeySource) HasKey(kid string) bool { return s.set.HasKey(kid) }

// KeySet exposes the underlying keys, for building a JWKS response.
func (s *StaticKeySource) KeySet() JWKS { return s.set }

// RotatingKeySource caches keys from a remote JWKS endpoint and refetches when
// it meets an unknown kid. This is how a running service picks up a rotated
// signing key without a restart.
type RotatingKeySource struct {
	mu      sync.RWMutex
	set     JWKS
	fetch   func() (JWKS, error)
	fetched time.Time
	minAge  time.Duration
	refetch func()
}

// NewRotatingKeySource builds a caching key source.
//
// fetch retrieves the current key set. minAge is the shortest interval between
// automatic refetches, which bounds how hard a stream of unknown kids can drive
// outbound requests.
func NewRotatingKeySource(fetch func() (JWKS, error), minAge time.Duration) *RotatingKeySource {
	if minAge <= 0 {
		minAge = 5 * time.Minute
	}
	return &RotatingKeySource{fetch: fetch, minAge: minAge}
}

// KeyFor returns a key, refetching the set if the kid is unknown and the cache
// is stale.
func (s *RotatingKeySource) KeyFor(kid string) (*rsa.PublicKey, error) {
	s.mu.RLock()
	set := s.set
	s.mu.RUnlock()

	if pub, err := set.KeyByID(kid); err == nil {
		return pub, nil
	} else if !errors.Is(err, ErrNoMatchingKey) {
		return nil, err
	}

	// Unknown kid: this is the signal that the auth node rotated its key.
	// Refetch, subject to the minimum interval.
	if err := s.refresh(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.set.KeyByID(kid)
}

// HasKey reports whether the cached set already contains the kid, without
// triggering a refetch.
func (s *RotatingKeySource) HasKey(kid string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.set.HasKey(kid)
}

// Refresh forces a refetch, bypassing the minimum interval.
func (s *RotatingKeySource) Refresh() error { return s.refresh() }

func (s *RotatingKeySource) refresh() error {
	if s.fetch == nil {
		return ErrNoMatchingKey
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.fetched.IsZero() && time.Since(s.fetched) < s.minAge {
		// Within the minimum interval: do not hammer the endpoint. Report
		// whatever is cached and let the caller fail with unknown kid.
		return nil
	}

	set, err := s.fetch()
	if err != nil {
		return fmt.Errorf("authn: fetching JWKS: %w", err)
	}
	s.set = set
	s.fetched = time.Now()
	return nil
}

// Verifier checks access tokens. Every service that consumes tokens builds one.
//
// A Verifier is safe for concurrent use.
type Verifier struct {
	keys     KeySource
	issuer   string
	audience string
	leeway   time.Duration
	wantType string
	required bool
}

// VerifierOption customises a Verifier.
type VerifierOption func(*Verifier)

// WithIssuer pins the expected iss claim.
func WithIssuer(issuer string) VerifierOption {
	return func(v *Verifier) { v.issuer = issuer }
}

// WithAudience pins the expected aud claim.
func WithAudience(audience string) VerifierOption {
	return func(v *Verifier) { v.audience = audience }
}

// WithLeeway overrides the clock-skew tolerance.
func WithLeeway(d time.Duration) VerifierOption {
	return func(v *Verifier) { v.leeway = d }
}

// WithTokenType requires a specific token_use claim, so a token of another kind
// cannot be presented in its place.
func WithTokenType(t string) VerifierOption {
	return func(v *Verifier) { v.wantType = t }
}

// NewVerifier builds a Verifier over a key source.
func NewVerifier(keys KeySource, opts ...VerifierOption) *Verifier {
	v := &Verifier{
		keys:     keys,
		leeway:   ClockSkew * time.Second,
		wantType: TokenTypeAccess,
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Verify checks a compact JWS and returns its claims.
//
// Checks, in order:
//  1. the token is well-formed and parses;
//  2. the alg header is RS256 — never "none", never HMAC;
//  3. the signature is valid under the key named by the kid header;
//  4. sub and exp are present;
//  5. iss and aud match this deployment;
//  6. exp has passed and nbf has arrived, within the skew tolerance;
//  7. token_use is the expected kind.
//
// The signature is checked before any claim is trusted, so an attacker cannot
// learn anything from the response to a token they did not sign.
func (v *Verifier) Verify(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, errors.New("authn: empty token")
	}
	if v == nil || v.keys == nil {
		return nil, errors.New("authn: verifier has no key source")
	}

	claims := &Claims{}
	parser := jwt.NewParser(
		// Pin the algorithm from the server's configuration, never from the
		// token. jwt.WithValidMethods is what blocks the alg-confusion
		// attack: golang-jwt will otherwise happily verify an HS256 token
		// with whatever key the keyfunc returns.
		jwt.WithValidMethods([]string{SigningAlgorithm}),

		// Claim validation is done explicitly below so each failure has a
		// distinct error type; disable the parser's own exp/nbf checks.
		jwt.WithoutClaimsValidation(),
	)

	parsed, err := parser.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		// Belt and braces: assert the concrete method even though
		// WithValidMethods already filtered the set.
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("%w: %v", ErrUnexpectedAlgorithm, t.Header["alg"])
		}

		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("authn: token has no kid header")
		}

		pub, err := v.keys.KeyFor(kid)
		if err != nil {
			return nil, err
		}
		return pub, nil
	})
	if err != nil {
		if errors.Is(err, ErrUnsignedAlgorithm) || errors.Is(err, ErrUnexpectedAlgorithm) {
			return nil, err
		}
		return nil, fmt.Errorf("authn: token verification failed: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("authn: token is not valid")
	}

	leeway := int64(v.leeway.Seconds())
	if err := claims.VerifyClaims(v.issuer, v.audience, leeway); err != nil {
		return nil, err
	}
	if v.wantType != "" {
		if err := claims.RequireTokenType(v.wantType); err != nil {
			return nil, err
		}
	}

	return claims, nil
}
