package utils

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// randomToken returns a URL-safe random string from bits of cryptographic
// randomness. Used for authorization codes, client ids, and client secrets.
//
// crypto/rand is the only acceptable source: a predictable code or secret is a
// guessable credential.
func randomToken(bits int) (string, error) {
	if bits <= 0 {
		bits = 256
	}
	b := make([]byte, bits/8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CodeBits is the entropy of an authorization code.
//
// RFC 6749 section 10.10 requires at least 128 bits; 256 bits leaves generous
// headroom against guessing within the code's two-minute lifetime.
const CodeBits = 256

// GenerateAuthCode returns a new opaque authorization code.
func GenerateAuthCode() (string, error) {
	return randomToken(CodeBits)
}

// ClientIDBits is the entropy of a client identifier. Client ids are not
// secrets, but unpredictable ones avoid collisions and enumeration.
const ClientIDBits = 128

// GenerateClientID returns a new OAuth client identifier.
func GenerateClientID() (string, error) {
	return randomToken(ClientIDBits)
}
