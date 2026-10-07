package authn

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ContextKey is the type of the context keys this package sets.
type contextKey string

// ContextClaimsKey is where verified claims are stashed for a downstream
// handler.
const ContextClaimsKey contextKey = "authn.claims"

// BearerScheme is the Authorization scheme RFC 6750 defines.
const BearerScheme = "bearer"

// Errors returned by the middleware. Exported so services can distinguish
// "no credential" from "bad credential" without string matching.
var (
	ErrNoToken          = fmt.Errorf("authn: no bearer token in request")
	ErrMalformedHeader  = fmt.Errorf("authn: malformed Authorization header")
	ErrUnsupportedSchem = fmt.Errorf("authn: Authorization scheme is not Bearer")
)

// WithClaims returns a context carrying the verified claims.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, ContextClaimsKey, claims)
}

// ClaimsFrom retrieves verified claims from a request context.
func ClaimsFrom(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(ContextClaimsKey).(*Claims)
	return c, ok
}

// ExtractBearerToken pulls the token out of an Authorization header.
//
// Only the Bearer scheme is accepted. Header parsing is case-insensitive on the
// scheme name per RFC 7235, but the token value is not trimmed of meaningful
// characters beyond surrounding whitespace.
func ExtractBearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", ErrNoToken
	}

	scheme, token, found := strings.Cut(header, " ")
	if !found {
		return "", ErrMalformedHeader
	}

	if !strings.EqualFold(scheme, BearerScheme) {
		return "", ErrUnsupportedSchem
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return "", ErrNoToken
	}

	return token, nil
}

// Middleware wraps a handler so it only runs for requests carrying a valid
// access token.
//
// A Middleware is safe for concurrent use. It exists so services do not each
// reimplement bearer parsing and claim checks, which is how token validation
// ends up subtly different between services.
type Middleware struct {
	verifier *Verifier
	logger   ErrorLogger

	// RequiredScopes, when non-empty, must all be present on the token.
	RequiredScopes []string

	// allowAll disables verification. Development only; see AllowAll.
	allowAll bool
}

// ErrorLogger is the minimal logging surface the middleware needs, so it does
// not force a dependency on a specific logger.
type ErrorLogger interface {
	Warn(msg string, fields ...any)
}

// NewMiddleware builds a Middleware over a verifier.
func NewMiddleware(v *Verifier, logger ErrorLogger) *Middleware {
	return &Middleware{verifier: v, logger: logger}
}

// RequireScope adds a scope requirement.
func (m *Middleware) RequireScope(scope string) *Middleware {
	m.RequiredScopes = append(m.RequiredScopes, scope)
	return m
}

// errorBody is the rejection body.
//
// It deliberately says nothing about why verification failed. Distinguishing
// "expired" from "bad signature" from "unknown issuer" in an unauthenticated
// response is a free oracle for an attacker probing tokens.
type errorBody struct {
	Error     string `json:"error"`
	ErrorDesc string `json:"error_description"`
}

// Middleware wraps next.
//
// On success the verified claims are placed in the request context and next
// runs. On failure it writes 401 and does not call next.
//
// The WWW-Authenticate header is set on every rejection, as RFC 6750 section 3
// requires, so a client knows a credential is expected here.
func (m *Middleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.allowAll {
			next.ServeHTTP(w, r)
			return
		}

		token, err := ExtractBearerToken(r)
		if err != nil {
			m.reject(w, r, http.StatusUnauthorized,
				"invalid_request", "a Bearer access token is required")
			return
		}

		claims, err := m.verifier.Verify(token)
		if err != nil {
			m.log(r, "token verification failed", err)
			// "invalid_token" is the RFC 6750 code for a credential that was
			// presented and rejected.
			m.reject(w, r, http.StatusUnauthorized,
				"invalid_token", "the access token is not valid")
			return
		}

		// Authorization failure is 403, distinct from authentication failure:
		// the caller is known, they just lack permission.
		for _, required := range m.RequiredScopes {
			if !claims.HasScope(required) {
				m.reject(w, r, http.StatusForbidden,
					"insufficient_scope", "the token lacks a required scope")
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}

func (m *Middleware) reject(w http.ResponseWriter, r *http.Request, status int, code, description string) {
	w.Header().Set("WWW-Authenticate",
		fmt.Sprintf(`Bearer realm="%s", error="%s", error_description="%s"`,
			"piranid", code, description))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: code, ErrorDesc: description})
}

func (m *Middleware) log(r *http.Request, msg string, err error) {
	if m.logger == nil {
		return
	}
	m.logger.Warn(msg,
		"path", r.URL.Path,
		"remote", r.RemoteAddr,
		"error", err.Error(),
	)
}

// AllowAll returns a Middleware that passes every request through without
// verification.
//
// This exists solely for local development, where a service is exercised before
// an auth node is available. It is deliberately awkward to reach: a service must
// opt in via an environment variable and log a warning at startup, so the state
// is visible rather than silent.
//
// It must never be enabled in a deployed environment. Any request reaching a
// protected handler through it is unauthenticated.
func AllowAll(logger ErrorLogger) *Middleware {
	if logger != nil {
		logger.Warn("token verification is DISABLED: all requests will be accepted")
	}
	return &Middleware{verifier: nil, allowAll: true}
}

// ---------------------------------------------------------------------------
// JWKS fetching
// ---------------------------------------------------------------------------

// JWKSFetcher retrieves the key set over HTTP and caches it.
//
// This is how a service picks up a rotated signing key without a restart: an
// unknown kid triggers a refetch, subject to a minimum interval so a stream of
// forged kids cannot be used to hammer the auth node.
type JWKSFetcher struct {
	url    string
	client *http.Client
	cache  *RotatingKeySource
	minAge time.Duration
}

// NewJWKSFetcher builds a fetcher for a JWKS URL.
func NewJWKSFetcher(url string, client *http.Client, minAge time.Duration) *JWKSFetcher {
	if client == nil {
		// A short timeout matters: this client sits on the request path of
		// every protected endpoint.
		client = &http.Client{Timeout: 5 * time.Second}
	}
	f := &JWKSFetcher{
		url:    url,
		client: client,
		minAge: minAge,
	}
	f.cache = NewRotatingKeySource(f.fetch, minAge)
	return f
}

// KeyFor implements KeySource.
func (f *JWKSFetcher) KeyFor(kid string) (*rsa.PublicKey, error) {
	return f.cache.KeyFor(kid)
}

// HasKey reports whether the cached set contains a kid.
func (f *JWKSFetcher) HasKey(kid string) bool { return f.cache.HasKey(kid) }

// Warm fetches the key set once at startup, so the first protected request does
// not pay the latency. A failure here is not fatal: the fetcher will retry on
// the first unknown kid.
func (f *JWKSFetcher) Warm() error { return f.cache.Refresh() }

// Refresh forces an immediate refetch.
func (f *JWKSFetcher) Refresh() error { return f.cache.Refresh() }

func (f *JWKSFetcher) fetch() (JWKS, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, f.url, nil)
	if err != nil {
		return JWKS{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return JWKS{}, fmt.Errorf("fetching %s: %w", f.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return JWKS{}, fmt.Errorf("fetching %s: status %d", f.url, resp.StatusCode)
	}

	var set JWKS
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return JWKS{}, fmt.Errorf("decoding JWKS from %s: %w", f.url, err)
	}
	if len(set.Keys) == 0 {
		return JWKS{}, fmt.Errorf("JWKS from %s contained no keys", f.url)
	}
	return set, nil
}
