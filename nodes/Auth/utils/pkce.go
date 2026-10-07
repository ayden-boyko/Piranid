package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
)

// PKCE parameter validation errors (RFC 7636 section 4.4/4.6).
var (
	ErrCodeVerifierTooShort = errors.New("code_verifier must be 43-128 characters")
	ErrCodeVerifierTooLong  = errors.New("code_verifier must be 43-128 characters")
	ErrChallengeMethod      = errors.New("code_challenge_method must be S256")
	ErrCodeChallengeMissing = errors.New("code_challenge is required")
	ErrVerifierMismatch     = errors.New("code_verifier does not match code_challenge")
)

// CodeChallengeLength is the S256 challenge length: SHA-256 output, base64url
// encoded without padding.
const CodeChallengeLength = 43

// S256Challenge derives the PKCE code challenge from a verifier:
//
//	plain = code_verifier
//	challenge = BASE64URL-ENCODE(SHA256(ASCII(plain)))
//
// This lives on the auth server only so tests and tooling can compute a
// challenge. Clients do this in their own language.
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// VerifyPKCE checks a code_verifier against the challenge stored with the
// authorization code.
//
// The comparison uses crypto/subtle.ConstantTimeCompare, not ==. A byte-by-byte
// string compare leaks how many leading characters matched through timing,
// which is enough to recover a verifier one character at a time.
func VerifyPKCE(verifier, challenge string) error {
	if verifier == "" {
		return ErrVerifierMismatch
	}

	// RFC 7636 section 4.1: the verifier is 43-128 characters of the
	// unreserved set. Enforcing the length here means an attacker cannot submit
	// an empty or huge verifier and have the server hash it.
	if len(verifier) < 43 || len(verifier) > 128 {
		return ErrCodeVerifierTooShort
	}

	expected := S256Challenge(verifier)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) != 1 {
		return ErrVerifierMismatch
	}
	return nil
}

// ValidateCodeChallenge checks a challenge presented at /authorize.
//
// Only S256 is accepted. Rejecting "plain" is deliberate: with plain, anyone
// who can see the authorization request also possesses the challenge, so PKCE
// provides no protection against an intercepted code.
func ValidateCodeChallenge(challenge, method string) error {
	if challenge == "" {
		return ErrCodeChallengeMissing
	}
	if method != "S256" {
		return ErrChallengeMethod
	}
	// An S256 challenge is exactly 43 base64url characters.
	if len(challenge) != CodeChallengeLength {
		return ErrCodeChallengeMissing
	}
	return nil
}

// verifierAlphabet is the unreserved character set RFC 7636 section 4.1
// permits in a code_verifier.
const verifierAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// GenerateCodeVerifier returns a random RFC 7636 code_verifier.
//
// Clients generate this in their own language; the auth server only ever needs
// to verify it. It lives here so tests and the local tooling can produce a
// conforming verifier.
//
// Length is fixed at 64 characters, comfortably inside the 43-128 range.
func GenerateCodeVerifier() (string, error) {
	const length = 64
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(verifierAlphabet))))
		if err != nil {
			return "", fmt.Errorf("generating code verifier: %w", err)
		}
		out[i] = verifierAlphabet[n.Int64()]
	}
	return string(out), nil
}
