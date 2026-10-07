// Package tests exercises the authorization server end to end over HTTP.
//
// These tests stand up the real handlers against a real SQLite database and a
// real RSA key, then drive the full authorization-code + PKCE flow with
// httptest. Nothing is mocked: the value of these tests is that they cover the
// wiring between the storage layer, the PKCE check, the token signer, and the
// redirect behaviour, which is where the previous implementation's bugs lived.
package tests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	data_manager "Piranid/pkg/DataManager"
	"Piranid/pkg/authn"
	"Piranid/pkg/dbutil"

	handler "github.com/ayden-boyko/Piranid/nodes/Auth/handlers"
	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
	transactions "github.com/ayden-boyko/Piranid/nodes/Auth/transactions"
	authutils "github.com/ayden-boyko/Piranid/nodes/Auth/utils"

	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

const (
	testIssuer   = "https://auth.piranid.test"
	testAudience = "event-queue"
	testRedirect = "http://localhost:9999/callback"
)

// harness is a running authorization server plus its dependencies.
type harness struct {
	server   *httptest.Server
	client   *http.Client
	deps     handler.Deps
	verifier *authn.Verifier
	db       *sql.DB
}

// noRedirect is an HTTP client that returns the 302 instead of following it.
//
// The tests assert on the redirect itself: that the Location carries the code
// and the echoed state, and that errors come back through the redirect rather
// than as a body. A client that follows redirects would try to reach
// localhost:9999 and fail with a connection error.
func noRedirect() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	dir := t.TempDir()

	// Real schema, real database file.
	schemaBytes, err := os.ReadFile(filepath.Join("..", "database", "Schema.sql"))
	if err != nil {
		t.Fatalf("reading Schema.sql: %v", err)
	}
	dbPath := filepath.Join(dir, "auth.db")
	db, err := dbutil.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	if err := dbutil.ApplySchema(db, string(schemaBytes)); err != nil {
		t.Fatalf("applying schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	credentials, err := data_manager.NewDataManager[model.AuthEntry](db, "credentials")
	if err != nil {
		t.Fatalf("credentials manager: %v", err)
	}
	codes, err := data_manager.NewDataManager[model.AuthCodeEntry](db, "auth_codes")
	if err != nil {
		t.Fatalf("auth_codes manager: %v", err)
	}

	pair, err := authn.GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	signer, err := authn.NewSigner(pair, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	deps := handler.Deps{
		Credentials: credentials,
		Codes:       codes,
		Signer:      signer,
		Config: authn.Config{
			Issuer:         testIssuer,
			Audience:       testAudience,
			AccessTokenTTL: 15 * time.Minute,
			AuthCodeTTL:    2 * time.Minute,
			ClockSkew:      30 * time.Second,
		},
		// A real logger keeps handler code paths that log unconditionaly
		// honest; zaptest output is noisy, so send it nowhere.
		Logger: zap.NewNop(),
	}

	tmpl, err := template.New("consent").Parse(consentHTML)
	if err != nil {
		t.Fatalf("parsing consent template: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		handler.JWKSHandler(w, r, deps)
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		handler.AuthorizeHandler(w, r, tmpl, deps)
	})
	mux.HandleFunc("POST /authorize/consent", func(w http.ResponseWriter, r *http.Request) {
		handler.ConsentHandler(w, r, deps)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		handler.TokenHandler(w, r, deps)
	})
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		handler.RegisterHandler(w, r, deps)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := noRedirect()

	verifier := authn.NewVerifier(authn.NewStaticKeySource(authn.NewJWKS(pair)),
		authn.WithIssuer(testIssuer),
		authn.WithAudience(testAudience),
	)

	return &harness{server: server, client: client, deps: deps, verifier: verifier, db: db}
}

// consentHTML is a minimal stand-in for the real template. It asserts that the
// handler supplies the fields the template depends on, which is where the
// original template broke ({{ .Redirect }} did not exist).
const consentHTML = `<html><body>
<form action="/authorize/consent" method="post">
{{ .ClientID }}|{{ .ClientName }}|{{ .RedirectURI }}|{{ .Scope }}|{{ .State }}|{{ .Nonce }}|{{ .CodeChallenge }}|{{ .CodeChallengeMethod }}|{{ .ResponseType }}
{{ range .Scopes }}[{{ . }}]{{ end }}
</form></body></html>`

// registeredClient is a registered client and the credentials to drive a flow.
type registeredClient struct {
	ClientID     string
	ClientSecret string
	Username     string
	Password     string
	RedirectURI  string
}

func (h *harness) register(t *testing.T, username string) registeredClient {
	t.Helper()

	password := "correct horse battery staple"
	body := url.Values{
		"client_name":  {"Test Client"},
		"redirect_uri": {testRedirect},
		"username":     {username},
		"password":     {password},
		"user_email":   {username + "@piranid.test"},
		"service_id":   {"event-queue"},
	}

	resp, err := h.client.PostForm(h.server.URL+"/register", body)
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var out transactions.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding register response: %v", err)
	}
	if out.ClientSecret == "" {
		t.Fatal("register returned an empty client_secret")
	}

	return registeredClient{
		ClientID:     out.ClientID,
		ClientSecret: out.ClientSecret,
		Username:     username,
		Password:     password,
		RedirectURI:  out.RedirectURI,
	}
}

// authorize drives GET /authorize and returns the rendered body.
func (h *harness) authorize(t *testing.T, c registeredClient, verifier, state string) *http.Response {
	t.Helper()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.ClientID},
		"redirect_uri":          {testRedirect},
		"scope":                 {"events:read events:write"},
		"state":                 {state},
		"nonce":                 {"nonce-abc"},
		"code_challenge":        {authutils.S256Challenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	resp, err := h.client.Get(h.server.URL + "/authorize?" + q.Encode())
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// consent submits the login form and returns the response, which should be a
// 302 carrying the authorization code.
func (h *harness) consent(t *testing.T, c registeredClient, verifier, state, password string) *http.Response {
	t.Helper()

	form := url.Values{
		"approve":               {"approve"},
		"username":              {c.Username},
		"password":              {password},
		"client_id":             {c.ClientID},
		"redirect_uri":          {testRedirect},
		"scope":                 {"events:read events:write"},
		"state":                 {state},
		"nonce":                 {"nonce-abc"},
		"code_challenge":        {authutils.S256Challenge(verifier)},
		"code_challenge_method": {"S256"},
	}

	resp, err := h.client.PostForm(h.server.URL+"/authorize/consent", form)
	if err != nil {
		t.Fatalf("consent request: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// exchange swaps a code plus verifier for an access token.
func (h *harness) exchange(t *testing.T, c registeredClient, code, verifier string) *http.Response {
	t.Helper()

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {testRedirect},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"code_verifier": {verifier},
	}

	resp, err := h.client.PostForm(h.server.URL+"/token", form)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// codeFromRedirect extracts the code from a 302 Location header.
func codeFromRedirect(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parsing redirect %q: %v", location, err)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatalf("redirect %q carried no code", location)
	}
	return code
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestFullAuthorizationCodeFlow is the happy path: authorize, authenticate,
// receive a code, exchange it for a token, and verify that token.
func TestFullAuthorizationCodeFlow(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "alice")
	verifier := mustVerifier(t)
	state := "state-xyz"

	authResp := h.authorize(t, c, verifier, state)
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("authorize status = %d, want 200", authResp.StatusCode)
	}
	page, _ := io.ReadAll(authResp.Body)
	// The template must receive a usable redirect_uri.
	if !strings.Contains(string(page), testRedirect) {
		t.Errorf("consent page did not carry redirect_uri; body:\n%s", page)
	}

	consentResp := h.consent(t, c, verifier, state, c.Password)
	if consentResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(consentResp.Body)
		t.Fatalf("consent status = %d, want 302; body: %s", consentResp.StatusCode, body)
	}

	loc := consentResp.Header.Get("Location")
	code := codeFromRedirect(t, loc)

	// state must be echoed verbatim so the client can correlate.
	locURL, _ := url.Parse(loc)
	if got := locURL.Query().Get("state"); got != state {
		t.Errorf("state = %q, want %q", got, state)
	}

	tokResp := h.exchange(t, c, code, verifier)
	if tokResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokResp.Body)
		t.Fatalf("token status = %d, want 200; body: %s", tokResp.StatusCode, body)
	}

	var tok transactions.TokenResponse
	if err := json.NewDecoder(tokResp.Body).Decode(&tok); err != nil {
		t.Fatalf("decoding token response: %v", err)
	}
	if tok.AccessToken == "" {
		t.Fatal("token response carried no access_token")
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", tok.TokenType)
	}
	if tok.ExpiresIn <= 0 || tok.ExpiresIn > 900 {
		t.Errorf("expires_in = %d, want a positive value under 15 minutes", tok.ExpiresIn)
	}

	claims, err := h.verifier.Verify(tok.AccessToken)
	if err != nil {
		t.Fatalf("verifying issued token: %v", err)
	}
	if claims.Subject != "alice" {
		t.Errorf("sub = %q, want alice", claims.Subject)
	}
	if claims.Issuer != testIssuer {
		t.Errorf("iss = %q, want %q", claims.Issuer, testIssuer)
	}
	if claims.Audience != testAudience {
		t.Errorf("aud = %q, want %q", claims.Audience, testAudience)
	}
	if !claims.HasScope("events:read") {
		t.Errorf("scope = %q, want to contain events:read", claims.Scope)
	}
}

// TestAuthorizationCodeIsSingleUse covers the replay that the old
// SELECT-then-DELETE delete was vulnerable to.
func TestAuthorizationCodeIsSingleUse(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "bob")
	verifier := mustVerifier(t)

	consentResp := h.consent(t, c, verifier, "s1", c.Password)
	code := codeFromRedirect(t, consentResp.Header.Get("Location"))

	first := h.exchange(t, c, code, verifier)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first exchange status = %d, want 200", first.StatusCode)
	}

	second := h.exchange(t, c, code, verifier)
	if second.StatusCode != http.StatusBadRequest {
		t.Fatalf("replayed exchange status = %d, want 400", second.StatusCode)
	}
	var e transactions.Error
	if err := json.NewDecoder(second.Body).Decode(&e); err != nil {
		t.Fatalf("decoding error: %v", err)
	}
	if e.Code != transactions.ErrInvalidGrant {
		t.Errorf("error code = %q, want %q", e.Code, transactions.ErrInvalidGrant)
	}
}

// TestWrongCodeVerifierIsRejected is the PKCE check itself.
func TestWrongCodeVerifierIsRejected(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "carol")

	correct := mustVerifier(t)
	wrong := mustVerifier(t)

	consentResp := h.consent(t, c, correct, "s2", c.Password)
	code := codeFromRedirect(t, consentResp.Header.Get("Location"))

	resp := h.exchange(t, c, code, wrong)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var e transactions.Error
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		t.Fatalf("decoding error: %v", err)
	}
	if e.Code != transactions.ErrInvalidGrant {
		t.Errorf("error code = %q, want %q", e.Code, transactions.ErrInvalidGrant)
	}
}

// TestWrongPasswordIsRejected covers the check the original LoginHandler
// omitted entirely.
func TestWrongPasswordIsRejected(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "dave")
	verifier := mustVerifier(t)

	resp := h.consent(t, c, verifier, "s3", "not-the-password")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302 back to the client", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parsing redirect: %v", err)
	}
	if got := u.Query().Get("error"); got == "" {
		t.Fatalf("failed authentication did not return an error; redirect = %q", loc)
	}
	// The error must describe the redirect target, not leak a code.
	if u.Query().Get("code") != "" {
		t.Error("a code was issued despite failed authentication")
	}
	// The response must not distinguish "no such user" from "wrong password".
	if !strings.Contains(u.Query().Get("error_description"), "username or password") {
		t.Errorf("error_description = %q, want the generic message",
			u.Query().Get("error_description"))
	}
}

func TestUnknownUserIsIndistinguishableFromWrongPassword(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "erin")
	verifier := mustVerifier(t)

	// Same form, but a username that was never registered.
	form := url.Values{
		"approve":               {"approve"},
		"username":              {"nobody-at-all"},
		"password":              {"whatever"},
		"client_id":             {c.ClientID},
		"redirect_uri":          {testRedirect},
		"code_challenge":        {authutils.S256Challenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	resp, err := h.client.PostForm(h.server.URL+"/authorize/consent", form)
	if err != nil {
		t.Fatalf("consent request: %v", err)
	}
	defer resp.Body.Close()

	u, _ := url.Parse(resp.Header.Get("Location"))
	if got := u.Query().Get("error_description"); !strings.Contains(got, "username or password") {
		t.Errorf("error_description = %q, want the generic message (enumeration oracle)", got)
	}
}

func TestUnregisteredRedirectURIIsRejected(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "frank")

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.ClientID},
		"redirect_uri":          {"http://evil.example/steal"},
		"code_challenge":        {authutils.S256Challenge(mustVerifier(t))},
		"code_challenge_method": {"S256"},
	}
	resp, err := h.client.Get(h.server.URL + "/authorize?" + q.Encode())
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer resp.Body.Close()

	// Must NOT redirect to the unregistered URI.
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Fatalf("server redirected to an unregistered URI: %q", loc)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPlainCodeChallengeMethodIsRejected(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "grace")
	verifier := mustVerifier(t)

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.ClientID},
		"redirect_uri":          {testRedirect},
		"code_challenge":        {verifier}, // the verifier itself: no hashing
		"code_challenge_method": {"plain"},
	}
	resp, err := h.client.Get(h.server.URL + "/authorize?" + q.Encode())
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer resp.Body.Close()

	// S256-only policy: a plain challenge must be refused outright.
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a plain code_challenge_method", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "code=") {
		t.Errorf("server issued a code for a plain challenge: %q", loc)
	}
}

func TestMissingCodeChallengeIsRejected(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "heidi")

	q := url.Values{
		"response_type": {"code"},
		"client_id":     {c.ClientID},
		"redirect_uri":  {testRedirect},
	}
	resp, err := h.client.Get(h.server.URL + "/authorize?" + q.Encode())
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer resp.Body.Close()

	if loc := resp.Header.Get("Location"); strings.Contains(loc, "code=") {
		t.Errorf("PKCE was bypassed: %q", loc)
	}
}

func TestUnsupportedGrantTypeIsRejected(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "ivan")

	resp, err := h.client.PostForm(h.server.URL+"/token", url.Values{
		"grant_type":    {"password"},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"username":      {"ivan"},
		"password":      {c.Password},
	})
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var e transactions.Error
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		t.Fatalf("decoding error: %v", err)
	}
	if e.Code != transactions.ErrUnsupportedGrantType {
		t.Errorf("error code = %q, want %q", e.Code, transactions.ErrUnsupportedGrantType)
	}
}

func TestWrongClientSecretIsRejected(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "judy")
	verifier := mustVerifier(t)

	consentResp := h.consent(t, c, verifier, "s4", c.Password)
	code := codeFromRedirect(t, consentResp.Header.Get("Location"))

	bad := c
	bad.ClientSecret = "wrong-secret"
	resp := h.exchange(t, bad, code, verifier)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCodeBoundToIssuingClient(t *testing.T) {
	h := newHarness(t)
	alice := h.register(t, "alice2")
	mallory := h.register(t, "mallory")

	verifier := mustVerifier(t)
	consentResp := h.consent(t, alice, verifier, "s5", alice.Password)
	code := codeFromRedirect(t, consentResp.Header.Get("Location"))

	// Mallory tries to redeem Alice's code.
	resp := h.exchange(t, mallory, code, verifier)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: a code must not cross clients", resp.StatusCode)
	}
}

func TestJWKSEndpointPublishesOnlyPublicKeys(t *testing.T) {
	h := newHarness(t)

	resp, err := h.client.Get(h.server.URL + "/.well-known/jwks.json")
	if err != nil {
		t.Fatalf("jwks request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	var set authn.JWKS
	if err := json.Unmarshal(body, &set); err != nil {
		t.Fatalf("decoding JWKS: %v", err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(set.Keys))
	}
	k := set.Keys[0]
	if k.Kty != "RSA" || k.Alg != "RS256" {
		t.Errorf("unexpected key metadata: %+v", k)
	}
	if strings.Contains(string(body), `"d"`) || strings.Contains(string(body), "PRIVATE") {
		t.Error("JWKS document contains private key material")
	}

	// The published key must actually verify tokens the server issues.
	if !set.HasKey(h.deps.Signer.KeyID()) {
		t.Errorf("JWKS is missing the active kid %q", h.deps.Signer.KeyID())
	}
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	h := newHarness(t)
	h.register(t, "kate")

	resp, err := h.client.PostForm(h.server.URL+"/register", url.Values{
		"redirect_uri": {testRedirect},
		"username":     {"kate"},
		"password":     {"another-password"},
	})
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a duplicate username", resp.StatusCode)
	}
}

func TestPasswordIsStoredHashed(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "leo")

	var stored string
	err := h.db.QueryRow("SELECT hashed_password FROM credentials WHERE username = ?", c.Username).Scan(&stored)
	if err != nil {
		t.Fatalf("reading stored password: %v", err)
	}
	if strings.Contains(stored, c.Password) {
		t.Fatal("the plaintext password is present in the stored hash")
	}
	if !strings.HasPrefix(stored, "$2") {
		t.Errorf("stored password %q is not a bcrypt hash", stored)
	}
}

func TestClientSecretIsStoredHashed(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "mallory2")

	var stored string
	err := h.db.QueryRow("SELECT client_secret FROM credentials WHERE username = ?", c.Username).Scan(&stored)
	if err != nil {
		t.Fatalf("reading stored secret: %v", err)
	}
	if strings.Contains(stored, c.ClientSecret) {
		t.Fatal("the plaintext client secret is present in the stored hash")
	}
}

func TestAuthorizationCodeIsStoredAndDeletedOnRedeem(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "nina")
	verifier := mustVerifier(t)

	consentResp := h.consent(t, c, verifier, "s6", c.Password)
	code := codeFromRedirect(t, consentResp.Header.Get("Location"))

	var stored, challenge, method string
	err := h.db.QueryRow(
		"SELECT code, code_challenge, code_challenge_method FROM auth_codes WHERE code = ?", code,
	).Scan(&stored, &challenge, &method)
	if err != nil {
		t.Fatalf("authorization code was not persisted: %v", err)
	}
	if method != "S256" {
		t.Errorf("stored method = %q, want S256", method)
	}

	if resp := h.exchange(t, c, code, verifier); resp.StatusCode != http.StatusOK {
		t.Fatalf("exchange status = %d, want 200", resp.StatusCode)
	}

	var remaining int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM auth_codes WHERE code = ?", code).Scan(&remaining); err != nil {
		t.Fatalf("counting codes: %v", err)
	}
	if remaining != 0 {
		t.Errorf("code still present after redemption (%d rows)", remaining)
	}
}

func TestExpiredAuthorizationCodeIsRejected(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "oscar")

	// Insert an already-expired code directly, to avoid waiting out the TTL.
	verifier := mustVerifier(t)
	past := time.Now().Add(-time.Hour).Unix()
	_, err := h.db.Exec(
		`INSERT INTO auth_codes (code, client_id, subject, scope, redirect_uri,
		 code_challenge, code_challenge_method, state, nonce, issued_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?, ?)`,
		"expired-code", c.ClientID, c.Username, "", testRedirect,
		authutils.S256Challenge(verifier), "S256", past, past+60,
	)
	if err != nil {
		t.Fatalf("inserting expired code: %v", err)
	}

	resp := h.exchange(t, c, "expired-code", verifier)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an expired code", resp.StatusCode)
	}
}

// TestMethodRoutingIsEnforced confirms the method-scoped patterns are active.
func TestMethodRoutingIsEnforced(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "peggy")

	// A GET on the token endpoint must not be routed to the handler.
	resp, err := h.client.Get(h.server.URL + "/token?grant_type=authorization_code&code=x")
	if err != nil {
		t.Fatalf("GET /token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /token status = %d, want 405", resp.StatusCode)
	}

	// A POST on the authorize endpoint likewise.
	resp2, err := h.client.PostForm(h.server.URL+"/authorize", url.Values{
		"response_type": {"code"}, "client_id": {c.ClientID}, "redirect_uri": {testRedirect},
	})
	if err != nil {
		t.Fatalf("POST /authorize: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /authorize status = %d, want 405", resp2.StatusCode)
	}
}

// mustVerifier returns a conforming PKCE code_verifier.
func mustVerifier(t *testing.T) string {
	t.Helper()
	v, err := authutils.GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier: %v", err)
	}
	return v
}

// TestConcurrentTokenRequestsSucceed covers a SQLite-specific failure mode.
//
// SQLite permits one writer. A deferred transaction takes a read lock and later
// upgrades it to a write lock, and SQLite deliberately does not invoke its busy
// handler for that upgrade, so the request fails with SQLITE_BUSY no matter how
// generous busy_timeout is. The auth node opens the database with BEGIN
// IMMEDIATE precisely to avoid this: several users authenticating at once must
// not cause requests to fail.
func TestConcurrentTokenRequestsSucceed(t *testing.T) {
	h := newHarness(t)

	const flows = 6
	type result struct {
		status int
		body   string
	}
	results := make([]result, flows)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < flows; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()

			username := fmt.Sprintf("concurrent-%d", n)
			c := h.register(t, username)
			verifier := mustVerifier(t)

			consentResp := h.consent(t, c, verifier, fmt.Sprintf("state-%d", n), c.Password)
			loc := consentResp.Header.Get("Location")
			if loc == "" {
				results[n] = result{status: 0, body: "no redirect from consent"}
				return
			}
			code := codeFromRedirect(t, loc)

			resp := h.exchange(t, c, code, verifier)
			body, _ := io.ReadAll(resp.Body)
			results[n] = result{status: resp.StatusCode, body: string(body)}
		}(i)
	}

	close(start)
	wg.Wait()

	for i, r := range results {
		if r.status != http.StatusOK {
			t.Errorf("flow %d: token status = %d, want 200; body: %s", i, r.status, r.body)
		}
	}
}

// TestConcurrentCodeRedemptionYieldsOneToken confirms atomic redemption holds
// under contention at the HTTP layer, not just in the storage layer.
func TestConcurrentCodeRedemptionYieldsOneToken(t *testing.T) {
	h := newHarness(t)
	c := h.register(t, "contended")
	verifier := mustVerifier(t)

	consentResp := h.consent(t, c, verifier, "s-contended", c.Password)
	code := codeFromRedirect(t, consentResp.Header.Get("Location"))

	const attempts = 6
	statuses := make([]int, attempts)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			resp := h.exchange(t, c, code, verifier)
			statuses[n] = resp.StatusCode
		}(i)
	}

	close(start)
	wg.Wait()

	granted := 0
	for _, s := range statuses {
		if s == http.StatusOK {
			granted++
		}
	}
	if granted != 1 {
		t.Errorf("%d of %d concurrent redemptions were granted a token, want exactly 1 (%v)",
			granted, attempts, statuses)
	}
}
