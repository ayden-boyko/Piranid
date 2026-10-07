package authn

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

// nowUnix is a variable so tests can pin the clock.
var nowUnix = func() int64 { return time.Now().Unix() }

// ClockSkew is the default tolerance, in seconds, for exp and nbf comparisons.
//
// Piranid nodes are Raspberry Pis on a local network with no guaranteed NTP
// discipline, so a strict comparison would intermittently reject valid tokens.
const ClockSkew = 30

// DefaultAccessTokenTTL is how long an access token is valid. Access tokens
// are short-lived by design: the original implementation gave every token kind
// the same 24-hour lifetime, so a leaked token stayed useful for a full day.
const DefaultAccessTokenTTL = 15 * time.Minute

// RandomToken returns a URL-safe random string carrying bits of cryptographic
// randomness.
//
// Used for authorization codes and jti values. crypto/rand is the only
// acceptable source here: a predictable code is a guessable credential.
func RandomToken(bits int) (string, error) {
	if bits <= 0 {
		bits = 256
	}
	b := make([]byte, bits/8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("authn: generating random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NewJTI returns a unique token identifier.
func NewJTI() (string, error) {
	return RandomToken(128)
}
