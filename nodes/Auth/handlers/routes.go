package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	data_manager "Piranid/pkg/DataManager"
	"Piranid/pkg/authn"
	sharedModels "Piranid/pkg/models"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
	transactions "github.com/ayden-boyko/Piranid/nodes/Auth/transactions"
	authutils "github.com/ayden-boyko/Piranid/nodes/Auth/utils"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

var tracer = otel.Tracer("github.com/ayden-boyko/Piranid/nodes/Auth")

// Deps carries the collaborators every handler needs.
//
// Passing an explicit struct, rather than seven positional arguments, is what
// lets the handlers be exercised from a test without starting a listener.
type Deps struct {
	Credentials *data_manager.DataManagerImpl[model.AuthEntry]
	Codes       *data_manager.DataManagerImpl[model.AuthCodeEntry]
	Signer      *authn.Signer
	Config      authn.Config
	Logger      *zap.Logger
}

// writeJSON sends a JSON body with the given status.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError sends an RFC 6749 section 5.2 error body and logs it.
//
// The client secret and any presented credential are deliberately absent from
// the description: error bodies travel to whoever made the request.
func writeError(w http.ResponseWriter, logger *zap.Logger, err *transactions.Error) {
	if logger != nil {
		logger.Warn("auth request rejected",
			zap.String("error_code", string(err.Code)),
			zap.String("description", err.Description),
			zap.Int("status", err.Status))
	}
	status := err.Status
	if status == 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, err)
}

// badRequest is shorthand for the common case.
func badRequest(code transactions.ErrorCode, format string, args ...any) *transactions.Error {
	return transactions.OAuthError(http.StatusBadRequest, code, format, args...)
}

// clientAuthFailed returns 401 invalid_client.
//
// Used for every client authentication failure, with one generic description.
// Distinguishing "unknown client" from "wrong secret" would let an attacker
// enumerate registered clients.
func clientAuthFailed() *transactions.Error {
	return transactions.OAuthError(http.StatusUnauthorized,
		transactions.ErrInvalidClient, "client authentication failed")
}

// ---------------------------------------------------------------------------
// GET /.well-known/jwks.json
// ---------------------------------------------------------------------------

// JWKSHandler publishes the public signing keys (RFC 7517).
//
// This is how services get the key they need to verify tokens. It exposes only
// public material, and requires no authentication: it must be reachable by
// every service, including ones that have no credentials yet.
func JWKSHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "JWKSHandler")
	defer span.End()

	set := d.Signer.JWKS()
	body, err := authn.JWKSBytes(set)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(w, d.Logger, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not serialize key set"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Clients may cache this briefly. Short, because a rotated key must
	// propagate quickly; a service that sees an unknown kid refetches anyway.
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)

	span.SetStatus(codes.Ok, "")
}

// ---------------------------------------------------------------------------
// GET /authorize
// ---------------------------------------------------------------------------

// AuthorizeHandler serves the consent/login page.
//
// Flow: the client sends the user here with its parameters in the query
// string. This handler validates them, then renders an HTML form that posts to
// ConsentHandler.
//
// Errors before the redirect URI is known are rendered as a plain error page.
// Once the redirect URI is validated, errors must instead be redirected back to
// the client (see redirectError), otherwise the user is stranded.
func AuthorizeHandler(w http.ResponseWriter, r *http.Request, templates *template.Template, d Deps) {
	_, span := tracer.Start(r.Context(), "AuthorizeHandler")
	defer span.End()

	q := r.URL.Query()
	req := transactions.AuthorizeRequest{
		ResponseType:        q.Get("response_type"),
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		Scope:               q.Get("scope"),
		State:               q.Get("state"),
		Nonce:               q.Get("nonce"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
	}

	client, err := validateAuthorizeRequest(req, d)
	if err != nil {
		var oauthErr *transactions.Error
		if errors.As(err, &oauthErr) && oauthErr.Code == transactions.ErrAccessDenied {
			// The redirect URI was valid, so the error goes back to the client.
			redirectError(w, r, req, oauthErr)
			span.SetStatus(codes.Error, oauthErr.Error())
			return
		}
		// redirect_uri is unknown or invalid: rendering directly is the only
		// safe option. Redirecting here would let an attacker use this
		// endpoint as an open redirector.
		http.Error(w, oauthErr.Description, oauthErr.Status)
		span.SetStatus(codes.Error, oauthErr.Error())
		return
	}

	data := map[string]any{
		"ClientID":            client.ClientId,
		"ClientName":          client.ClientName,
		"RedirectURI":         req.RedirectURI,
		"Scope":               req.Scope,
		"Scopes":              splitScopes(req.Scope),
		"State":               req.State,
		"Nonce":               req.Nonce,
		"CodeChallenge":       req.CodeChallenge,
		"CodeChallengeMethod": req.CodeChallengeMethod,
		"ResponseType":        req.ResponseType,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := templates.Execute(w, data); err != nil {
		span.SetStatus(codes.Error, err.Error())
		d.Logger.Error("rendering consent page", zap.Error(err))
	}

}

// validateAuthorizeRequest checks the authorize query string and loads the
// client. It returns the client on success.
//
// redirect_uri is checked first and separately because its validity decides
// where errors are reported.
func validateAuthorizeRequest(req transactions.AuthorizeRequest, d Deps) (*model.AuthEntry, *transactions.Error) {
	client, lookupErr := loadClient(d, "client_id", req.ClientID)
	if lookupErr != nil {
		if errors.Is(lookupErr, sql.ErrNoRows) || errors.Is(lookupErr, data_manager.ErrNoMatch) {
			// Without a known client there is no trustworthy redirect target,
			// so this must not be redirected.
			return nil, badRequest(transactions.ErrInvalidRequest, "unknown client_id")
		}
		return nil, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not load client")
	}

	// redirect_uri must match a registered value exactly. Prefix or suffix
	// matching would let an attacker register example.com and redirect to
	// example.com.evil.test.
	if req.RedirectURI == "" {
		return nil, badRequest(transactions.ErrInvalidRequest, "redirect_uri is required")
	}
	if !client.AllowsRedirect(req.RedirectURI) {
		return nil, badRequest(transactions.ErrInvalidRequest,
			"redirect_uri is not registered for this client")
	}

	if req.ResponseType != transactions.ResponseTypeCode {
		return nil, transactions.OAuthError(http.StatusBadRequest,
			transactions.ErrUnsupportedRespType,
			"response_type must be %q", transactions.ResponseTypeCode)
	}

	// PKCE is mandatory. A confidential client with a secret would be
	// protected without it, but making it universal means there is one path
	// through the server to get right, and public clients cannot be told to
	// skip it.
	if err := authutils.ValidateCodeChallenge(req.CodeChallenge, req.CodeChallengeMethod); err != nil {
		return nil, badRequest(transactions.ErrInvalidRequest, "%v", err)
	}

	return &client, nil
}

// ---------------------------------------------------------------------------
// POST /authorize/consent
// ---------------------------------------------------------------------------

// ConsentHandler authenticates the user and issues an authorization code.
//
// On success it redirects (302) to the client's redirect_uri with code and
// state. The previous implementation returned the code as a JSON body, which
// no OAuth client expects and which leaves the user on the auth server's page
// with nothing to do.
func ConsentHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "ConsentHandler")
	defer span.End()

	if err := r.ParseForm(); err != nil {
		// Form bodies are x-www-form-urlencoded. The old handlers decoded
		// JSON, so every browser submission failed to parse.
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, "malformed form submission", http.StatusBadRequest)
		return
	}

	form := transactions.ConsentForm{
		Username:            r.FormValue("username"),
		Password:            r.FormValue("password"),
		Approve:             r.FormValue("approve"),
		ClientID:            r.FormValue("client_id"),
		RedirectURI:         r.FormValue("redirect_uri"),
		Scope:               r.FormValue("scope"),
		State:               r.FormValue("state"),
		Nonce:               r.FormValue("nonce"),
		CodeChallenge:       r.FormValue("code_challenge"),
		CodeChallengeMethod: r.FormValue("code_challenge_method"),
	}

	authorize := transactions.AuthorizeRequest{
		ResponseType:        transactions.ResponseTypeCode,
		ClientID:            form.ClientID,
		RedirectURI:         form.RedirectURI,
		Scope:               form.Scope,
		State:               form.State,
		Nonce:               form.Nonce,
		CodeChallenge:       form.CodeChallenge,
		CodeChallengeMethod: form.CodeChallengeMethod,
	}

	// Revalidate everything. The form fields are user-controlled input: a
	// tampered form must not be able to redirect elsewhere or skip PKCE.
	client, oauthErr := validateAuthorizeRequest(authorize, d)
	if oauthErr != nil {
		redirectError(w, r, authorize, oauthErr)
		span.SetStatus(codes.Error, oauthErr.Error())
		return
	}

	// Explicit denial is a legitimate outcome, not an error.
	if form.Approve != "approve" {
		redirectError(w, r, authorize, transactions.OAuthError(http.StatusOK,
			transactions.ErrAccessDenied, "the user denied the request"))
		span.SetStatus(codes.Ok, "denied by user")
		return
	}

	// Authenticate the resource owner.
	//
	// The old LoginHandler only checked that the username existed; it decoded
	// the password field and never compared it. That is not authentication.
	user, lookupErr := loadClient(d, "username", form.Username)
	if lookupErr != nil {
		redirectError(w, r, authorize, denied("invalid username or password"))
		span.SetStatus(codes.Error, "user lookup failed")
		return
	}
	if err := authutils.CheckPassword(user.HashedPassword, form.Password); err != nil {
		d.Logger.Warn("failed authentication attempt",
			zap.String("username", form.Username),
			zap.String("client_id", form.ClientID))
		redirectError(w, r, authorize, denied("invalid username or password"))
		span.SetStatus(codes.Error, "bad password")
		return
	}

	// The signed-in user must be the one whose client is authenticating. A
	// user who knows another client's id should not be able to drive that
	// client's authorization code.
	if user.ClientId != client.ClientId {
		redirectError(w, r, authorize, denied("client does not belong to this user"))
		span.SetStatus(codes.Error, "client/user mismatch")
		return
	}

	code, err := issueAuthorizationCode(authorize, user, d)
	if err != nil {
		redirectError(w, r, authorize, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not issue authorization code"))
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// Success: hand the code back to the client via redirect.
	//
	// state is echoed verbatim so the client can correlate the response with
	// its original request (RFC 6749 section 10.12).
	target, err := appendCodeToRedirect(authorize.RedirectURI, code, authorize.State)
	if err != nil {
		redirectError(w, r, authorize, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not build redirect"))
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// Never log the code: it is a single-use bearer credential.
	d.Logger.Info("authorization code issued",
		zap.String("client_id", client.ClientId),
		zap.String("subject", user.Username))

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusFound)
	span.SetStatus(codes.Ok, "")
}

// denied builds the error returned to the client when a user fails to
// authenticate. The description is deliberately vague.
func denied(description string) *transactions.Error {
	return transactions.OAuthError(http.StatusFound, transactions.ErrAccessDenied, "%s", description)
}

// issueAuthorizationCode generates an opaque code, stores it with its PKCE
// binding, and returns it.
func issueAuthorizationCode(req transactions.AuthorizeRequest, user model.AuthEntry, d Deps) (string, error) {
	code, err := authutils.GenerateAuthCode()
	if err != nil {
		return "", err
	}

	now := nowUnix()
	entry := model.AuthCodeEntry{
		Code:                code,
		ClientId:            req.ClientID,
		Subject:             user.Username,
		Scope:               req.Scope,
		RedirectURI:         req.RedirectURI,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		State:               req.State,
		Nonce:               req.Nonce,
		IssuedAt:            now,
		ExpiresAt:           now + int64(d.Config.AuthCodeTTL.Seconds()),
	}

	if err := d.Codes.PushData(entry, authutils.AuthCodeInserter); err != nil {
		return "", fmt.Errorf("storing authorization code: %w", err)
	}
	return code, nil
}

// appendCodeToRedirect builds the redirect target carrying the code and state.
func appendCodeToRedirect(redirectURI, code, state string) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", fmt.Errorf("parsing redirect_uri: %w", err)
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// redirectError sends an error back to the client per RFC 6749 section 4.1.2.1.
//
// Error responses go in the query string. The body is deliberately not used, and
// the status is a normal 302, because the client only ever sees the redirect.
//
// state is echoed so the client can still correlate the failure with its
// original request.
func redirectError(w http.ResponseWriter, r *http.Request, req transactions.AuthorizeRequest, e *transactions.Error) {
	u, err := url.Parse(req.RedirectURI)
	if err != nil {
		http.Error(w, e.Description, http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", string(e.Code))
	if e.Description != "" {
		q.Set("error_description", e.Description)
	}
	if req.State != "" {
		q.Set("state", req.State)
	}
	u.RawQuery = q.Encode()

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// ---------------------------------------------------------------------------
// POST /token
// ---------------------------------------------------------------------------

// TokenHandler exchanges an authorization code for an access token
// (RFC 6749 section 4.1.3).
func TokenHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "TokenHandler")
	defer span.End()

	req, oauthErr := parseTokenRequest(r)
	if oauthErr != nil {
		span.SetStatus(codes.Error, oauthErr.Error())
		writeError(w, d.Logger, oauthErr)
		return
	}

	if req.GrantType != transactions.GrantTypeAuthorizationCode {
		span.SetStatus(codes.Error, "unsupported grant type")
		writeError(w, d.Logger, transactions.OAuthError(http.StatusBadRequest,
			transactions.ErrUnsupportedGrantType,
			"grant_type must be %q", transactions.GrantTypeAuthorizationCode))
		return
	}
	if req.Code == "" {
		span.SetStatus(codes.Error, "missing code")
		writeError(w, d.Logger, badRequest(transactions.ErrInvalidRequest, "code is required"))
		return
	}

	// Authenticate the client. Public clients relying on PKCE have no secret,
	// so client_id alone is accepted when no secret is presented.
	client, oauthErr := authenticateClient(d, req)
	if oauthErr != nil {
		span.SetStatus(codes.Error, oauthErr.Error())
		writeError(w, d.Logger, oauthErr)
		return
	}

	// Atomically consume the code. A second request with the same code finds
	// nothing, because ConsumeEntry deletes it in the same transaction as the
	// read. The previous SELECT-then-DELETE let two concurrent requests both
	// redeem one code.
	stored, consumeErr := d.Codes.ConsumeEntry(
		model.AuthCodeColumns, "code", req.Code,
		authutils.AuthCodeScanner, authutils.AuthCodeDeleter,
	)
	if consumeErr != nil {
		// ErrNoMatch covers both "never existed" and "already redeemed".
		// They are reported identically so a caller cannot probe which codes
		// are real.
		if errors.Is(consumeErr, data_manager.ErrNoMatch) || errors.Is(consumeErr, sql.ErrNoRows) {
			span.SetStatus(codes.Error, "unknown or used code")
			writeError(w, d.Logger, transactions.OAuthError(http.StatusBadRequest,
				transactions.ErrInvalidGrant, "authorization code is invalid, expired, or already used"))
			return
		}
		span.SetStatus(codes.Error, consumeErr.Error())
		writeError(w, d.Logger, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not redeem authorization code"))
		return
	}

	// Every binding recorded at /authorize is re-checked here. This is what
	// stops a code intercepted for one client from being redeemed by another,
	// or against a different redirect target.
	if stored.ClientId != client.ClientId {
		span.SetStatus(codes.Error, "code/client mismatch")
		writeError(w, d.Logger, transactions.OAuthError(http.StatusBadRequest,
			transactions.ErrInvalidGrant, "authorization code was not issued to this client"))
		return
	}
	if stored.RedirectURI != req.RedirectURI {
		span.SetStatus(codes.Error, "redirect_uri mismatch")
		writeError(w, d.Logger, transactions.OAuthError(http.StatusBadRequest,
			transactions.ErrInvalidGrant, "redirect_uri does not match the authorization request"))
		return
	}
	if stored.IsExpired(time.Now()) {
		span.SetStatus(codes.Error, "code expired")
		writeError(w, d.Logger, transactions.OAuthError(http.StatusBadRequest,
			transactions.ErrInvalidGrant, "authorization code has expired"))
		return
	}

	// PKCE: the client proves it can derive the challenge from a secret that
	// never travelled in the authorize request.
	if err := authutils.VerifyPKCE(req.CodeVerifier, stored.CodeChallenge); err != nil {
		span.SetStatus(codes.Error, "pkce failed")
		writeError(w, d.Logger, transactions.OAuthError(http.StatusBadRequest,
			transactions.ErrInvalidGrant, "%v", err))
		return
	}

	accessToken, expiresAt, err := d.Signer.Issue(authn.IssueRequest{
		Subject:   stored.Subject,
		ClientID:  stored.ClientId,
		Scope:     stored.Scope,
		TokenType: authn.TokenTypeAccess,
		Issuer:    d.Config.Issuer,
		Audience:  d.Config.Audience,
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(w, d.Logger, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not issue access token"))
		return
	}

	// Log metadata only. Token values are bearer credentials.
	d.Logger.Info("access token issued",
		zap.String("client_id", stored.ClientId),
		zap.String("subject", stored.Subject))

	writeJSON(w, http.StatusOK, transactions.TokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   expiresAt - nowUnix(),
		Scope:       stored.Scope,
	})

	span.SetStatus(codes.Ok, "")
}

// parseTokenRequest reads the token request body.
//
// Both form-encoded and JSON bodies are accepted. RFC 6749 section 4.1.3
// specifies form encoding, but JSON is friendlier for curl-based debugging.
func parseTokenRequest(r *http.Request) (transactions.TokenRequest, *transactions.Error) {
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "application/json") {
		var req transactions.TokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return transactions.TokenRequest{}, badRequest(transactions.ErrInvalidRequest,
				"could not parse request body")
		}
		return req, nil
	}

	if err := r.ParseForm(); err != nil {
		return transactions.TokenRequest{}, badRequest(transactions.ErrInvalidRequest,
			"could not parse request body")
	}

	req := transactions.TokenRequest{
		GrantType:    r.FormValue("grant_type"),
		Code:         r.FormValue("code"),
		RedirectURI:  r.FormValue("redirect_uri"),
		ClientID:     r.FormValue("client_id"),
		ClientSecret: r.FormValue("client_secret"),
		CodeVerifier: r.FormValue("code_verifier"),
	}

	// RFC 6749 section 2.3.1 permits client credentials in a Basic
	// Authorization header. Supporting it is what lets a client keep its
	// secret out of the request body, where it is more likely to be logged.
	if req.ClientID == "" || req.ClientSecret == "" {
		if id, secret, ok := r.BasicAuth(); ok {
			if req.ClientID == "" {
				req.ClientID = id
			}
			if req.ClientSecret == "" {
				req.ClientSecret = secret
			}
		}
	}

	return req, nil
}

// authenticateClient resolves and verifies the client credentials.
//
// A client that presents no secret is treated as a public client relying on
// PKCE alone, which the RFC permits. One that presents a secret must present
// the right one, compared in constant time.
func authenticateClient(d Deps, req transactions.TokenRequest) (*model.AuthEntry, *transactions.Error) {
	if req.ClientID == "" {
		return nil, clientAuthFailed()
	}

	client, err := loadClient(d, "client_id", req.ClientID)
	if err != nil {
		if errors.Is(err, data_manager.ErrNoMatch) || errors.Is(err, sql.ErrNoRows) {
			return nil, clientAuthFailed()
		}
		return nil, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not load client")
	}

	if req.ClientSecret == "" {
		// Public client: PKCE is the only thing protecting the code, and the
		// token endpoint has already required code_verifier.
		return &client, nil
	}

	if err := authutils.CheckSecret(client.ClientSecret, req.ClientSecret); err != nil {
		d.Logger.Warn("client authentication failed", zap.String("client_id", req.ClientID))
		return nil, clientAuthFailed()
	}

	return &client, nil
}

// ---------------------------------------------------------------------------
// POST /register
// ---------------------------------------------------------------------------

// RegisterHandler registers a client and its owning user, returning the
// generated client credentials.
//
// This replaces the previous signup endpoint, which accepted a
// client-supplied "hashed_password" and stored it verbatim. A caller that
// chooses its own hash can choose a weak one, or a hash of something it does
// not have to prove later.
func RegisterHandler(w http.ResponseWriter, r *http.Request, d Deps) {
	_, span := tracer.Start(r.Context(), "RegisterHandler")
	defer span.End()

	var req transactions.RegisterRequest
	contentType := r.Header.Get("Content-Type")

	switch {
	case strings.HasPrefix(contentType, "application/json"):
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			span.SetStatus(codes.Error, err.Error())
			writeError(w, d.Logger, badRequest(transactions.ErrInvalidRequest,
				"could not parse request body"))
			return
		}
	default:
		if err := r.ParseForm(); err != nil {
			span.SetStatus(codes.Error, err.Error())
			writeError(w, d.Logger, badRequest(transactions.ErrInvalidRequest,
				"could not parse request body"))
			return
		}
		req = transactions.RegisterRequest{
			ClientName:  r.FormValue("client_name"),
			RedirectURI: r.FormValue("redirect_uri"),
			Username:    r.FormValue("username"),
			Password:    r.FormValue("password"),
			UserEmail:   r.FormValue("user_email"),
			ServiceID:   r.FormValue("service_id"),
		}
	}

	if req.Username == "" || req.Password == "" || req.RedirectURI == "" {
		span.SetStatus(codes.Error, "missing fields")
		writeError(w, d.Logger, badRequest(transactions.ErrInvalidRequest,
			"username, password and redirect_uri are required"))
		return
	}
	if !strings.HasPrefix(req.RedirectURI, "https://") && !strings.HasPrefix(req.RedirectURI, "http://") {
		span.SetStatus(codes.Error, "bad redirect_uri")
		writeError(w, d.Logger, badRequest(transactions.ErrInvalidRequest,
			"redirect_uri must be an absolute http or https URI"))
		return
	}

	passwordHash, err := authutils.HashPassword(req.Password)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(w, d.Logger, badRequest(transactions.ErrInvalidRequest, "%v", err))
		return
	}

	clientSecret, err := authutils.GenerateSecret()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(w, d.Logger, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not generate client secret"))
		return
	}
	secretHash, err := authutils.HashSecret(clientSecret)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(w, d.Logger, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not hash client secret"))
		return
	}

	clientID, err := authutils.GenerateClientID()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(w, d.Logger, transactions.OAuthError(http.StatusInternalServerError,
			transactions.ErrServerError, "could not generate client id"))
		return
	}

	serviceID := req.ServiceID
	if serviceID == "" {
		serviceID = d.Config.Issuer
	}

	entry := model.AuthEntry{
		Entry:          newEntry(),
		ClientId:       clientID,
		ClientSecret:   secretHash,
		ClientName:     req.ClientName,
		Username:       req.Username,
		UserEmail:      req.UserEmail,
		HashedPassword: passwordHash,
		ServiceId:      serviceID,
		RedirectURI:    req.RedirectURI,
	}

	if err := d.Credentials.PushData(entry, authutils.CredentialsInserter); err != nil {
		// A UNIQUE constraint violation means the username or client id is
		// taken.
		span.SetStatus(codes.Error, err.Error())
		writeError(w, d.Logger, badRequest(transactions.ErrInvalidRequest,
			"could not register: username or client_id already exists"))
		return
	}

	d.Logger.Info("client registered",
		zap.String("client_id", clientID),
		zap.String("username", req.Username))

	// The only time the plaintext secret is ever available.
	writeJSON(w, http.StatusCreated, transactions.RegisterResponse{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		ClientName:   req.ClientName,
		RedirectURI:  req.RedirectURI,
		Username:     req.Username,
		UserEmail:    req.UserEmail,
	})

	span.SetStatus(codes.Ok, "")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// splitScopes breaks a space-separated scope string into individual scopes for
// display on the consent page.
func splitScopes(scope string) []string {
	return strings.Fields(scope)
}

// loadClient fetches one credentials row by one of its unique columns.
func loadClient(d Deps, column, value string) (model.AuthEntry, error) {
	return d.Credentials.GetEntry(
		authutils.CredentialsColumns, column, value,
		authutils.CredentialsScanner,
	)
}

// nowUnix is indirected so tests can control time.
var nowUnix = func() int64 { return time.Now().Unix() }

// newEntry builds a fresh credentials row with its creation timestamp set.
func newEntry() sharedModels.Entry {
	return sharedModels.Entry{Date_Created: time.Now()}
}
