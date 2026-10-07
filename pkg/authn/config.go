package authn

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the auth node's token configuration, read from the environment.
//
// Environment variables rather than flags, because the deployment is
// Kubernetes manifests and the Dockerfile sets ENV directly.
type Config struct {
	// Issuer is the iss claim. It must be stable across restarts: it is
	// published in tokens and pinned by every verifying service.
	//
	// It must NOT default to the node's service ID. That value is a fresh
	// UUID generated at every boot (pkg/utils.go NewServiceID), which would
	// invalidate every outstanding token each time the pod restarted.
	Issuer string

	// Audience is the aud claim, identifying the service the tokens are for.
	Audience string

	// PrivateKeyPath and PublicKeyPath point at PEM files.
	PrivateKeyPath string
	PublicKeyPath  string

	// AccessTokenTTL is the access token lifetime.
	AccessTokenTTL time.Duration

	// AuthCodeTTL is how long an authorization code remains redeemable.
	// Authorization codes are short-lived by design: a code is exchanged once
	// for a real credential, so it should not outlive that exchange.
	AuthCodeTTL time.Duration

	// ClockSkew is the tolerance applied to exp and nbf.
	ClockSkew time.Duration
}

// Environment variable names read by LoadConfig.
const (
	EnvIssuer         = "AUTH_ISSUER"
	EnvAudience       = "AUTH_AUDIENCE"
	EnvPrivateKeyPath = "AUTH_PRIVATE_KEY_PATH"
	EnvPublicKeyPath  = "AUTH_PUBLIC_KEY_PATH"
	EnvAccessTokenTTL = "ACCESS_TOKEN_TTL"
	EnvAuthCodeTTL    = "AUTH_CODE_TTL"
	EnvClockSkew      = "AUTH_CLOCK_SKEW"
)

// Defaults applied when the corresponding variable is unset.
const (
	DefaultAccessTokenTTLConfig = 15 * time.Minute
	DefaultAuthCodeTTL          = 2 * time.Minute
	DefaultClockSkew            = 30 * time.Second
)

// LoadConfig reads configuration from the environment, applying defaults.
func LoadConfig() Config {
	return Config{
		Issuer:         os.Getenv(EnvIssuer),
		Audience:       os.Getenv(EnvAudience),
		PrivateKeyPath: os.Getenv(EnvPrivateKeyPath),
		PublicKeyPath:  os.Getenv(EnvPublicKeyPath),
		AccessTokenTTL: durationOrDefault(EnvAccessTokenTTL, DefaultAccessTokenTTLConfig),
		AuthCodeTTL:    durationOrDefault(EnvAuthCodeTTL, DefaultAuthCodeTTL),
		ClockSkew:      durationOrDefault(EnvClockSkew, DefaultClockSkew),
	}
}

// Validate reports configuration that would produce unverifiable tokens.
//
// issuer and audience are required rather than optional. A token without iss
// cannot be attributed to a deployment, and a token without aud cannot be
// scoped to a service; either would silently weaken every verifier.
func (c Config) Validate() error {
	if c.Issuer == "" {
		return fmt.Errorf("authn: %s is required (it is the iss claim every service pins)", EnvIssuer)
	}
	if c.Audience == "" {
		return fmt.Errorf("authn: %s is required (it is the aud claim every service pins)", EnvAudience)
	}
	if c.PrivateKeyPath == "" {
		return fmt.Errorf("authn: %s is required", EnvPrivateKeyPath)
	}
	if c.AccessTokenTTL <= 0 {
		return fmt.Errorf("authn: %s must be positive", EnvAccessTokenTTL)
	}
	if c.AuthCodeTTL <= 0 {
		return fmt.Errorf("authn: %s must be positive", EnvAuthCodeTTL)
	}
	return nil
}

// durationOrDefault parses a Go duration string, falling back on unset or
// unparseable values rather than failing startup on a typo.
func durationOrDefault(env string, fallback time.Duration) time.Duration {
	raw := os.Getenv(env)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}

// Seconds is a small helper for TTLs stored as Unix seconds in the database.
func Seconds(d time.Duration) int64 { return int64(d.Seconds()) }

// ParseSeconds converts a stored Unix second count to a duration.
func ParseSeconds(v int64) time.Duration { return time.Duration(v) * time.Second }

// FormatSeconds renders a duration for the storage layer.
func FormatSeconds(d time.Duration) int64 { return int64(d / time.Second) }

// itoa is used by the JWKS handler for cache headers.
func itoa(v int) string { return strconv.Itoa(v) }
