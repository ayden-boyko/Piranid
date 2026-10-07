package authn

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// echoClaims is a protected handler that reports the claims it received, so a
// test can assert both that the request was allowed and what was attached to
// the context.
func echoClaims() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFrom(r.Context())
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(claims)
	})
}

func newTestMiddleware(t *testing.T, scopes ...string) (*Middleware, *Signer, *KeyPair) {
	t.Helper()
	signer, pair := newTestSigner(t)
	verifier := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)
	m := NewMiddleware(verifier, nil)
	for _, s := range scopes {
		m.RequireScope(s)
	}
	return m, signer, pair
}

func TestMiddlewareAcceptsValidToken(t *testing.T) {
	m, signer, _ := newTestMiddleware(t)
	srv := httptest.NewServer(m.Middleware(echoClaims()))
	defer srv.Close()

	tok := issue(t, signer, IssueRequest{Scope: "events:read"})

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Claims must reach the handler.
	var claims Claims
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		t.Fatalf("decoding claims: %v", err)
	}
	if claims.Subject != "alice" {
		t.Errorf("sub = %q, want alice", claims.Subject)
	}
}

func TestMiddlewareRejectsMissingToken(t *testing.T) {
	m, _, _ := newTestMiddleware(t)
	srv := httptest.NewServer(m.Middleware(echoClaims()))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if h := resp.Header.Get("WWW-Authenticate"); h == "" {
		t.Error("WWW-Authenticate header missing on rejection")
	}
}

func TestMiddlewareRejectsMalformedHeader(t *testing.T) {
	m, _, _ := newTestMiddleware(t)
	srv := httptest.NewServer(m.Middleware(echoClaims()))
	defer srv.Close()

	cases := []string{
		"Bearer",             // no token
		"Bearer ",            // empty token
		"Basic dXNlcjpwYXNz", // wrong scheme
		tokWithoutScheme,     // raw token, no scheme
	}

	for _, header := range cases {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		req.Header.Set("Authorization", header)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %q: %v", header, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("header %q: status = %d, want 401", header, resp.StatusCode)
		}
	}
}

const tokWithoutScheme = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.sig"

func TestMiddlewareAcceptsCaseInsensitiveBearerScheme(t *testing.T) {
	m, signer, _ := newTestMiddleware(t)
	srv := httptest.NewServer(m.Middleware(echoClaims()))
	defer srv.Close()

	tok := issue(t, signer, IssueRequest{})

	// RFC 7235 makes the scheme token case-insensitive.
	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		req.Header.Set("Authorization", scheme+" "+tok)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("scheme %q: status = %d, want 200", scheme, resp.StatusCode)
		}
	}
}

func TestMiddlewareRejectsExpiredToken(t *testing.T) {
	m, signer, _ := newTestMiddleware(t)
	srv := httptest.NewServer(m.Middleware(echoClaims()))
	defer srv.Close()

	tok := issue(t, signer, IssueRequest{TTL: time.Second})

	restore := nowUnix
	nowUnix = func() int64 { return time.Now().Add(2 * time.Hour).Unix() }
	defer func() { nowUnix = restore }()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an expired token", resp.StatusCode)
	}
}

// The rejection body must not reveal why verification failed, or it becomes an
// oracle for probing token provenance.
func TestRejectionBodyDoesNotLeakReason(t *testing.T) {
	m, signer, pair := newTestMiddleware(t)
	srv := httptest.NewServer(m.Middleware(echoClaims()))
	defer srv.Close()

	// A token with the wrong audience.
	wrongAud := issue(t, signer, IssueRequest{Audience: "somewhere-else"})

	for name, tok := range map[string]string{
		"wrong audience": wrongAud,
		"garbage":        "not.a.jwt",
		"empty-ish":      "a.b.c",
	} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, resp.StatusCode)
		}
		if body["error"] != "invalid_token" {
			t.Errorf("%s: error = %q, want invalid_token", name, body["error"])
		}
		if desc := body["error_description"]; desc != "the access token is not valid" {
			t.Errorf("%s: description %q leaks the failure reason", name, desc)
		}
	}
	_ = pair
}

func TestMiddlewareEnforcesScopes(t *testing.T) {
	m, signer, _ := newTestMiddleware(t, "events:write")
	srv := httptest.NewServer(m.Middleware(echoClaims()))
	defer srv.Close()

	// Wrong scope: 403, not 401. The caller is authenticated but not permitted.
	tok := issue(t, signer, IssueRequest{Scope: "events:read"})
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a missing scope", resp.StatusCode)
	}

	// Right scope.
	tok2 := issue(t, signer, IssueRequest{Scope: "events:write"})
	req2, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req2.Header.Set("Authorization", "Bearer "+tok2)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp2.StatusCode)
	}
}

// TestAllowAllPassthrough documents the development escape hatch.
func TestAllowAllPassthrough(t *testing.T) {
	srv := httptest.NewServer(AllowAll(nil).Middleware(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 with AllowAll", resp.StatusCode)
	}
}

// TestJWKSFetcherFollowsRotation exercises the fetch-and-cache path against a
// real HTTP JWKS endpoint.
func TestJWKSFetcherFollowsRotation(t *testing.T) {
	_, first := newTestSigner(t)

	var active *KeyPair = first
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := JWKSBytes(NewJWKS(active))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	fetcher := NewJWKSFetcher(server.URL, server.Client(), time.Hour)
	if err := fetcher.Warm(); err != nil {
		t.Fatalf("Warm: %v", err)
	}

	// The currently published key verifies.
	if _, err := fetcher.KeyFor(first.Kid); err != nil {
		t.Fatalf("KeyFor: %v", err)
	}

	// Rotate the published key.
	second, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	active = second
	fetcher.cache.minAge = 0 // allow immediate refetch

	if _, err := fetcher.KeyFor(second.Kid); err != nil {
		t.Errorf("KeyFor after rotation: %v", err)
	}
}

// TestEventQueueTokenIsVerifiedEndToEnd proves a token minted by the signer is
// accepted by the middleware, which is the contract every protected node relies
// on.
func TestEventQueueTokenIsVerifiedEndToEnd(t *testing.T) {
	m, signer, _ := newTestMiddleware(t)
	srv := httptest.NewServer(m.Middleware(echoClaims()))
	defer srv.Close()

	tok := issue(t, signer, IssueRequest{Subject: "event-queue-service"})

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
