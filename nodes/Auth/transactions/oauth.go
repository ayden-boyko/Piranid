package transactions

import "fmt"

// OAuth 2.0 error codes (RFC 6749 section 5.2).
type ErrorCode string

const (
	ErrInvalidRequest         ErrorCode = "invalid_request"
	ErrInvalidClient          ErrorCode = "invalid_client"
	ErrInvalidGrant           ErrorCode = "invalid_grant"
	ErrUnauthorizedClient     ErrorCode = "unauthorized_client"
	ErrUnsupportedGrantType   ErrorCode = "unsupported_grant_type"
	ErrInvalidScope           ErrorCode = "invalid_scope"
	ErrAccessDenied           ErrorCode = "access_denied"
	ErrUnsupportedRespType    ErrorCode = "unsupported_response_type"
	ErrServerError            ErrorCode = "server_error"
	ErrTemporarilyUnavailable ErrorCode = "temporarily_unavailable"
)

// Error is an OAuth error response body.
//
// The previous implementation reported failures with http.Error, which writes
// text/plain and carries no machine-readable code. A client cannot distinguish
// "your code expired" from "your client secret is wrong" from a prose string,
// and RFC 6749 section 5.2 requires a JSON body with an "error" member.
type Error struct {
	Code        ErrorCode `json:"error"`
	Description string    `json:"error_description,omitempty"`
	URI         string    `json:"error_uri,omitempty"`

	// Status is the HTTP status to send alongside the body. RFC 6749 wants 400
	// for most codes; invalid_client uses 401 when the client presented
	// credentials via the Authorization header.
	Status int `json:"-"`
}

func (e *Error) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Description)
	}
	return string(e.Code)
}

// OAuthError builds an Error with the given status and description.
func OAuthError(status int, code ErrorCode, format string, args ...any) *Error {
	return &Error{
		Code:        code,
		Description: fmt.Sprintf(format, args...),
		Status:      status,
	}
}

// AuthorizeRequest is the parsed and validated GET /authorize query string
// (RFC 6749 section 4.1.1).
//
// Validation order matters here: redirect_uri is validated against the
// registered set *before* any other error is returned. Once the server is
// willing to redirect, every subsequent error must be reported back to the
// client rather than rendered directly, or the user ends up staring at a raw
// error page with no way back to the application.
type AuthorizeRequest struct {
	ResponseType        string `json:"response_type"`
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope"`
	State               string `json:"state"`
	Nonce               string `json:"nonce"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
}

// ResponseTypeCode is the only supported response_type. The implicit flow is
// deliberately not implemented: it returns tokens in a URL fragment, where they
// leak into browser history and Referer headers.
const ResponseTypeCode = "code"

// CodeChallengeMethodS256 is the only accepted PKCE transform.
//
// "plain" is rejected because it gives no protection: an attacker who
// intercepts the authorization request also sees the challenge, and can
// therefore use the code. S256 forces the client to prove it held a secret
// that never travelled in the request.
const CodeChallengeMethodS256 = "S256"

// GrantTypeAuthorizationCode is the only supported grant_type in this pass.
//
// Refresh tokens are not implemented yet, so grant_type=refresh_token returns
// unsupported_grant_type rather than being silently ignored.
const GrantTypeAuthorizationCode = "authorization_code"

// TokenRequest is the parsed POST /token body (RFC 6749 section 4.1.3).
type TokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	CodeVerifier string `json:"code_verifier"`
}

// TokenResponse is a successful token response (RFC 6749 section 5.1).
//
// expires_in is required by the spec and was missing before; a client cannot
// schedule a refresh without knowing the lifetime.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}

// ConsentForm is the POST body submitted by the consent/login HTML form.
//
// The browser posts application/x-www-form-urlencoded, which is what
// r.ParseForm handles. The previous handlers decoded JSON bodies while the
// templates posted form data, so no browser flow ever completed.
type ConsentForm struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Approve  string `json:"approve"`

	// Echoed back from the original authorize request so the handler can
	// rebuild the redirect without trusting client-supplied context again.
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope"`
	State               string `json:"state"`
	Nonce               string `json:"nonce"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
}

// RegisterRequest registers a client and its owning user.
type RegisterRequest struct {
	ClientName  string `json:"client_name"`
	RedirectURI string `json:"redirect_uri"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	UserEmail   string `json:"user_email"`
	ServiceID   string `json:"service_id"`
}

// RegisterResponse returns the generated client credentials.
//
// ClientSecret is populated exactly once, here. Only its bcrypt hash is stored,
// so the plaintext is unrecoverable afterwards. Losing it means re-registering.
type RegisterResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	ClientName   string `json:"client_name"`
	RedirectURI  string `json:"redirect_uri"`
	Username     string `json:"username"`
	UserEmail    string `json:"user_email"`
}
