package authn

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
)

// JWK is a single JSON Web Key (RFC 7517).
//
// Only public RSA signing keys are ever serialized. There is deliberately no
// field for private key material, so a JWKS document cannot leak a signing key
// even if one is passed in by mistake.
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKS is a JSON Web Key Set.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// NewJWK builds a public JWK for an RSA key.
func NewJWK(pub *rsa.PublicKey, kid string) JWK {
	return JWK{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: kid,
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(bigEndianExponent(pub)),
	}
}

// NewJWKS builds a key set from one or more public keys. The kid of each key
// is computed from its own material, so a caller cannot publish a key under a
// mismatched identifier.
func NewJWKS(keys ...*KeyPair) JWKS {
	set := JWKS{Keys: make([]JWK, 0, len(keys))}
	for _, k := range keys {
		if k == nil || k.Public == nil {
			continue
		}
		set.Keys = append(set.Keys, NewJWK(k.Public, k.Kid))
	}
	return set
}

// JWKSBytes renders a key set as indented JSON, for the discovery endpoint.
func JWKSBytes(set JWKS) ([]byte, error) {
	b, err := marshalJSON(set)
	if err != nil {
		return nil, fmt.Errorf("authn: encoding JWKS: %w", err)
	}
	return b, nil
}

// ErrNoMatchingKey reports that no key in the set matches a requested kid.
var ErrNoMatchingKey = errors.New("authn: no JWKS key matches the requested kid")

// KeyByID returns the public key with the given kid.
func (s JWKS) KeyByID(kid string) (*rsa.PublicKey, error) {
	for _, jwk := range s.Keys {
		if jwk.Kid != kid {
			continue
		}
		if jwk.Kty != "RSA" {
			return nil, fmt.Errorf("authn: unsupported key type %q for kid %q", jwk.Kty, kid)
		}
		pub, err := jwk.PublicKey()
		if err != nil {
			return nil, err
		}
		return pub, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrNoMatchingKey, kid)
}

// PublicKey decodes a JWK's "n" and "e" members into an RSA public key.
func (j JWK) PublicKey() (*rsa.PublicKey, error) {
	if j.N == "" || j.E == "" {
		return nil, errors.New("authn: JWKS key is missing n or e")
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(j.N)
	if err != nil {
		return nil, fmt.Errorf("authn: decoding JWK modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(j.E)
	if err != nil {
		return nil, fmt.Errorf("authn: decoding JWK exponent: %w", err)
	}
	if len(eBytes) == 0 || len(eBytes) > 8 {
		return nil, errors.New("authn: JWK exponent has an implausible length")
	}

	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e < 3 {
		return nil, errors.New("authn: JWK exponent is too small")
	}

	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

// ParseJWKS decodes a JWKS document fetched from a remote endpoint.
func ParseJWKS(data []byte) (JWKS, error) {
	var set JWKS
	if err := unmarshalJSON(data, &set); err != nil {
		return JWKS{}, fmt.Errorf("authn: decoding JWKS document: %w", err)
	}
	if len(set.Keys) == 0 {
		return JWKS{}, errors.New("authn: JWKS document contains no keys")
	}
	return set, nil
}

// HasKey reports whether the set contains a key with the given kid. A verifier
// uses this to decide whether to refetch after an unknown kid, which is how key
// rotation propagates without restarting every service.
func (s JWKS) HasKey(kid string) bool {
	for _, jwk := range s.Keys {
		if jwk.Kid == kid {
			return true
		}
	}
	return false
}
