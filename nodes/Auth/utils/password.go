package utils

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost is the work factor for password and client-secret hashing.
//
// 12 is roughly 250ms on the hardware this is deployed to, which is a
// deliberate cost: it is what makes an offline attack against a stolen
// database expensive. Raise it as hardware improves.
const BcryptCost = 12

// HashPassword returns a bcrypt hash of a plaintext password.
//
// The previous signup handler accepted a client-supplied "hashed_password" and
// stored it verbatim, and the login handler never checked the password at all.
func HashPassword(plaintext string) (string, error) {
	if plaintext == "" {
		return "", errors.New("password must not be empty")
	}
	// bcrypt silently truncates at 72 bytes. Refuse longer input rather than
	// accepting a password whose tail is ignored, which would let two distinct
	// passwords be equivalent.
	if len(plaintext) > 72 {
		return "", errors.New("password must be at most 72 bytes")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plaintext), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(h), nil
}

// CheckPassword compares a plaintext password against a stored bcrypt hash.
//
// On mismatch it returns the sentinel ErrInvalidCredentials rather than
// bcrypt's error, so a caller cannot distinguish "no such user" from "wrong
// password" by inspecting the error. That distinction is a user-enumeration
// oracle.
func CheckPassword(hash, plaintext string) error {
	if hash == "" || plaintext == "" {
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// ErrInvalidCredentials is returned for any failed credential check.
var ErrInvalidCredentials = errors.New("invalid credentials")

// HashSecret hashes a client secret for storage.
func HashSecret(secret string) (string, error) {
	if secret == "" {
		return "", errors.New("client secret must not be empty")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(secret), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing client secret: %w", err)
	}
	return string(h), nil
}

// CheckSecret compares a presented client secret against its stored hash.
//
// Same user-enumeration consideration as CheckPassword.
func CheckSecret(hash, presented string) error {
	return CheckPassword(hash, presented)
}

// GenerateSecret returns a new random client secret.
func GenerateSecret() (string, error) {
	return randomToken(32)
}
