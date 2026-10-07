// Package authn implements the token format shared by the Piranid auth node
// and every service that consumes its tokens.
//
// The authorization server signs with RS256 and publishes the matching public
// key at a JWKS endpoint. Services never share a signing secret with the server:
// they hold only the public key, so a compromised service can verify tokens but
// cannot mint them.
//
// Verification deliberately checks, in order: the algorithm is RS256, the
// signature is valid for the key named by the token's kid, and the issuer,
// audience, and expiry claims match what this deployment expects.
package authn

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// DefaultKeyBits is the RSA modulus size for generated signing keys.
// 2048 bits is the current minimum acceptable size; 4096 is the safer choice
// on hardware with a crypto accelerator and acceptable as the default.
const DefaultKeyBits = 2048

// KeyPair is a loaded RSA signing key and its public half.
type KeyPair struct {
	Private *rsa.PrivateKey
	Public  *rsa.PublicKey

	// Kid is the RFC 7638 JWK thumbprint of the public key. It identifies this
	// key in the JWKS document and in the "kid" header of every token signed
	// with it. Deriving it from the key material rather than assigning it by
	// hand means it stays consistent across restarts and across nodes.
	Kid string
}

// ErrNoPrivateKey is returned when a private key file is missing or empty.
var ErrNoPrivateKey = errors.New("authn: no private key available")

// LoadKeyPair reads an RSA key pair from PEM files.
//
// privatePath must contain a PKCS#1 or PKCS#8 RSA private key. publicPath is
// optional: when empty, or when the file cannot be read, the public key is
// derived from the private key.
func LoadKeyPair(privatePath, publicPath string) (*KeyPair, error) {
	if privatePath == "" {
		return nil, fmt.Errorf("authn: %w: private key path is empty", ErrNoPrivateKey)
	}

	privPEM, err := os.ReadFile(privatePath)
	if err != nil {
		return nil, fmt.Errorf("authn: reading private key: %w", err)
	}

	priv, err := parsePrivateKey(privPEM)
	if err != nil {
		return nil, err
	}

	pair := &KeyPair{Private: priv, Public: &priv.PublicKey}

	if publicPath != "" {
		pubPEM, err := os.ReadFile(publicPath)
		if err == nil {
			pub, err := parsePublicKey(pubPEM)
			if err != nil {
				return nil, err
			}
			pair.Public = pub
		}
	}

	pair.Kid = Thumbprint(pair.Public)
	return pair, nil
}

// GenerateKeyPair creates a new RSA key pair, for development and for key
// rotation. The private key is never written to disk by this package; writing
// it is the caller's decision, because where secrets live should be an
// explicit choice.
func GenerateKeyPair(bits int) (*KeyPair, error) {
	if bits < DefaultKeyBits {
		bits = DefaultKeyBits
	}
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, fmt.Errorf("authn: generating RSA key: %w", err)
	}
	return &KeyPair{
		Private: priv,
		Public:  &priv.PublicKey,
		Kid:     Thumbprint(&priv.PublicKey),
	}, nil
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("authn: private key is not valid PEM")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("authn: parsing private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("authn: private key is %T, want *rsa.PrivateKey", parsed)
	}
	return key, nil
}

func parsePublicKey(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("authn: public key is not valid PEM")
	}

	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("authn: public key is %T, want *rsa.PublicKey", key)
		}
		return rsaKey, nil
	}

	// Also accept PKCS#1, which is what some key generators emit.
	key, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("authn: parsing public key: %w", err)
	}
	return key, nil
}

// EncodePrivateKeyPEM renders a private key as PKCS#8 PEM.
func EncodePrivateKeyPEM(key *rsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("authn: marshalling private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// EncodePublicKeyPEM renders a public key as PKIX PEM.
func EncodePublicKeyPEM(key *rsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, fmt.Errorf("authn: marshalling public key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// Thumbprint computes the RFC 7638 JWK thumbprint of an RSA public key, which
// this package uses as the key id.
//
// RFC 7638 requires hashing a JSON object containing only kty, e and n, with
// the members in lexicographic order and no whitespace. Any change to the key
// changes the thumbprint, which is what makes it a stable identifier.
func Thumbprint(pub *rsa.PublicKey) string {
	canonical := fmt.Sprintf(
		`{"e":"%s","kty":"RSA","n":"%s"}`,
		base64.RawURLEncoding.EncodeToString(bigEndianExponent(pub)),
		base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
	)
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// bigEndianExponent renders the public exponent as big-endian bytes with no
// leading zero bytes, as required for the JWK "e" member. A zero-length result
// would be invalid, so the minimum value is clamped to a single byte.
func bigEndianExponent(pub *rsa.PublicKey) []byte {
	e := pub.E
	switch {
	case e == 0:
		return []byte{0}
	case e < 0x100:
		return []byte{byte(e)}
	case e < 0x10000:
		return []byte{byte(e >> 8), byte(e)}
	default:
		return []byte{
			byte(e >> 16), byte(e >> 8), byte(e),
		}
	}
}

// marshalJSON is used for JWKS serialization; kept separate so the indent
// constant lives in one place.
func marshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// unmarshalJSON is the decode counterpart, so both JWKS directions go through
// one place.
func unmarshalJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
