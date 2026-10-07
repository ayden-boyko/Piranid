package authn

import (
	"crypto/rsa"
	"crypto/x509"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newTestSigner(t *testing.T) (*Signer, *KeyPair) {
	t.Helper()
	pair, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	signer, err := NewSigner(pair, time.Minute)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return signer, pair
}

func issue(t *testing.T, s *Signer, req IssueRequest) string {
	t.Helper()
	if req.Issuer == "" {
		req.Issuer = "https://auth.piranid.local"
	}
	if req.Audience == "" {
		req.Audience = "event-queue"
	}
	if req.Subject == "" {
		req.Subject = "alice"
	}
	tok, _, err := s.Issue(req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return tok
}

func TestSignVerifyRoundTrip(t *testing.T) {
	signer, pair := newTestSigner(t)
	tok := issue(t, signer, IssueRequest{Scope: "events:read events:write", ClientID: "client-abc"})

	v := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)

	claims, err := v.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "alice" {
		t.Errorf("sub = %q, want alice", claims.Subject)
	}
	if claims.ClientID != "client-abc" {
		t.Errorf("client_id = %q, want client-abc", claims.ClientID)
	}
	if claims.TokenType != TokenTypeAccess {
		t.Errorf("token_use = %q, want %q", claims.TokenType, TokenTypeAccess)
	}
	if claims.JTI == "" {
		t.Error("jti is empty")
	}
	if !claims.HasScope("events:read") || !claims.HasScope("events:write") {
		t.Errorf("scopes = %v, want both", claims.Scopes())
	}
}

// The core security property: a service holding only the public key must not be
// able to mint a token the verifier accepts.
func TestVerifierRejectsHS256SignedWithPublicKey(t *testing.T) {
	_, pair := newTestSigner(t)

	// Attacker re-signs the claims with HMAC, using the server's PUBLIC key as
	// the shared secret. A verifier that dispatches on the token's alg header
	// would accept this.
	claims := Claims{
		Issuer:    "https://auth.piranid.local",
		Audience:  "event-queue",
		Subject:   "attacker",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
		IssuedAt:  time.Now().Unix(),
		NotBefore: time.Now().Unix(),
		JTI:       "forged",
		TokenType: TokenTypeAccess,
	}
	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	forged.Header["kid"] = pair.Kid

	hmacKey, err := rsaPublicKeyBytes(pair.Public)
	if err != nil {
		t.Fatalf("deriving hmac key: %v", err)
	}
	signed, err := forged.SignedString(hmacKey)
	if err != nil {
		t.Fatalf("signing forged token: %v", err)
	}

	v := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)

	if _, err := v.Verify(signed); err == nil {
		t.Fatal("verifier accepted an HS256 token: algorithm confusion is not blocked")
	}
}

func TestVerifierRejectsAlgNone(t *testing.T) {
	_, pair := newTestSigner(t)

	claims := Claims{
		Issuer:    "https://auth.piranid.local",
		Audience:  "event-queue",
		Subject:   "attacker",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
		IssuedAt:  time.Now().Unix(),
	}
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	unsigned.Header["kid"] = pair.Kid
	signed, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("signing alg=none token: %v", err)
	}

	v := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)

	if _, err := v.Verify(signed); err == nil {
		t.Fatal("verifier accepted an alg=none token")
	}
}

func TestVerifierRejectsWrongIssuer(t *testing.T) {
	signer, pair := newTestSigner(t)
	tok := issue(t, signer, IssueRequest{Issuer: "https://evil.example"})

	v := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)

	if _, err := v.Verify(tok); err == nil {
		t.Fatal("verifier accepted a token from a different issuer")
	}
}

func TestVerifierRejectsWrongAudience(t *testing.T) {
	signer, pair := newTestSigner(t)
	tok := issue(t, signer, IssueRequest{Audience: "some-other-service"})

	v := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)

	if _, err := v.Verify(tok); err == nil {
		t.Fatal("verifier accepted a token minted for a different audience")
	}
}

func TestVerifierRejectsExpired(t *testing.T) {
	signer, pair := newTestSigner(t)
	tok := issue(t, signer, IssueRequest{TTL: time.Second})

	// Advance the clock past the token's lifetime.
	restore := nowUnix
	nowUnix = func() int64 { return time.Now().Add(2 * time.Hour).Unix() }
	defer func() { nowUnix = restore }()

	v := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)

	if _, err := v.Verify(tok); err == nil {
		t.Fatal("verifier accepted an expired token")
	}
}

func TestVerifierRejectsNotYetValid(t *testing.T) {
	_, pair := newTestSigner(t)

	future := time.Now().Add(2 * time.Hour).Unix()
	claims := Claims{
		Issuer:    "https://auth.piranid.local",
		Audience:  "event-queue",
		Subject:   "alice",
		IssuedAt:  future,
		NotBefore: future,
		ExpiresAt: future + 3600,
		TokenType: TokenTypeAccess,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = pair.Kid
	signed, err := tok.SignedString(pair.Private)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	v := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)

	if _, err := v.Verify(signed); err == nil {
		t.Fatal("verifier accepted a token whose nbf is in the future")
	}
}

func TestVerifierRejectsWrongTokenType(t *testing.T) {
	signer, pair := newTestSigner(t)
	tok := issue(t, signer, IssueRequest{TokenType: TokenTypeRefresh})

	v := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
		WithTokenType(TokenTypeAccess),
	)

	if _, err := v.Verify(tok); err == nil {
		t.Fatal("verifier accepted a refresh token where an access token was required")
	}
}

func TestVerifierRejectsTamperedPayload(t *testing.T) {
	signer, pair := newTestSigner(t)
	tok := issue(t, signer, IssueRequest{Scope: "events:read"})

	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token is not a compact JWS: %d parts", len(parts))
	}
	// Re-encode the payload with an escalated scope, leaving the signature stale.
	parts[1] = "eyJpc3MiOiJodHRwczovL2F1dGgucGlyYW5pZC5sb2NhbCIsImF1ZCI6ImV2ZW50LXF1ZXVlIiwi"
	tampered := strings.Join(parts, ".")

	v := NewVerifier(NewStaticKeySource(NewJWKS(pair)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)

	if _, err := v.Verify(tampered); err == nil {
		t.Fatal("verifier accepted a tampered payload")
	}
}

func TestVerifierRejectsUnknownKeyID(t *testing.T) {
	signer, _ := newTestSigner(t)
	tok := issue(t, signer, IssueRequest{})

	// A key set that does not contain the signing key.
	other, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	v := NewVerifier(NewStaticKeySource(NewJWKS(other)),
		WithIssuer("https://auth.piranid.local"),
		WithAudience("event-queue"),
	)

	if _, err := v.Verify(tok); err == nil {
		t.Fatal("verifier accepted a token signed by an unknown key")
	}
}

func TestJWKSRoundTrip(t *testing.T) {
	pair, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	set := NewJWKS(pair)
	if len(set.Keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(set.Keys))
	}
	k := set.Keys[0]
	if k.Kty != "RSA" || k.Alg != "RS256" || k.Use != "sig" {
		t.Errorf("unexpected key metadata: %+v", k)
	}

	// The published JWK must reconstruct the original public key exactly, or
	// services will fail signature verification.
	back, err := k.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	if back.N.Cmp(pair.Public.N) != 0 || back.E != pair.Public.E {
		t.Error("JWKS round trip did not preserve the public key")
	}

	// The private key must never appear in the document.
	raw, err := JWKSBytes(set)
	if err != nil {
		t.Fatalf("JWKSBytes: %v", err)
	}
	if strings.Contains(string(raw), `"d"`) {
		t.Error("JWKS document contains a private exponent")
	}

	parsed, err := ParseJWKS(raw)
	if err != nil {
		t.Fatalf("ParseJWKS: %v", err)
	}
	if !parsed.HasKey(pair.Kid) {
		t.Errorf("kid %q missing after round trip", pair.Kid)
	}
}

func TestThumbprintIsStableAndUnique(t *testing.T) {
	a, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	b, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	if a.Kid == "" {
		t.Fatal("kid is empty")
	}
	if a.Kid == b.Kid {
		t.Error("two distinct keys produced the same kid")
	}
	if again := Thumbprint(a.Public); again != a.Kid {
		t.Errorf("thumbprint is not stable: %q then %q", a.Kid, again)
	}
}

func TestRotatingKeySourcePicksUpNewKey(t *testing.T) {
	first, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	var current = NewJWKS(first)
	fetches := 0
	// A long minimum age models production: an unknown kid must not trigger an
	// outbound fetch per request, or a stream of forged kids becomes a
	// request amplifier against the auth node.
	src := NewRotatingKeySource(func() (JWKS, error) {
		fetches++
		return current, nil
	}, time.Hour)
	// Relax the throttle for this test so rotation is observed in one step.
	src.minAge = 0

	// Seed the cache with the first key.
	if _, err := src.KeyFor(first.Kid); err != nil {
		t.Fatalf("seeding cache: %v", err)
	}

	// Rotate.
	second, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	current = NewJWKS(first, second)

	if _, err := src.KeyFor(second.Kid); err != nil {
		t.Fatalf("KeyFor after rotation: %v", err)
	}
	if fetches < 2 {
		t.Errorf("expected a refetch after rotation, got %d fetches", fetches)
	}
}

func TestKeyPairPEMRoundTrip(t *testing.T) {
	privPEM, err := EncodePrivateKeyPEM(mustGenerate(t).Private)
	if err != nil {
		t.Fatalf("EncodePrivateKeyPEM: %v", err)
	}
	pubPEM, err := EncodePublicKeyPEM(mustGenerate(t).Public)
	if err != nil {
		t.Fatalf("EncodePublicKeyPEM: %v", err)
	}

	dir := t.TempDir()
	privPath := dir + "/jwt_private.pem"
	pubPath := dir + "/jwt_public.pem"
	if err := writeFile(privPath, privPEM); err != nil {
		t.Fatalf("writing private key: %v", err)
	}
	if err := writeFile(pubPath, pubPEM); err != nil {
		t.Fatalf("writing public key: %v", err)
	}

	pair, err := LoadKeyPair(privPath, pubPath)
	if err != nil {
		t.Fatalf("LoadKeyPair: %v", err)
	}
	if pair.Kid == "" {
		t.Error("kid is empty after loading from PEM")
	}
}

func TestLoadKeyPairRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.pem"
	if err := writeFile(path, []byte("this is not a PEM file")); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	if _, err := LoadKeyPair(path, ""); err == nil {
		t.Fatal("LoadKeyPair accepted a non-PEM file")
	}
}

func TestConfigValidateRequiresIssuerAndAudience(t *testing.T) {
	c := Config{
		Issuer:         "",
		Audience:       "event-queue",
		PrivateKeyPath: "/keys/priv.pem",
		AccessTokenTTL: time.Minute,
		AuthCodeTTL:    time.Minute,
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted an empty issuer")
	}

	c.Issuer = "https://auth.piranid.local"
	c.Audience = ""
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted an empty audience")
	}

	c.Audience = "event-queue"
	c.PrivateKeyPath = ""
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted an empty key path")
	}

	c.PrivateKeyPath = "/keys/priv.pem"
	if err := c.Validate(); err != nil {
		t.Errorf("Validate rejected a complete config: %v", err)
	}
}

func TestRandomTokenIsUnique(t *testing.T) {
	seen := make(map[string]bool, 256)
	for i := 0; i < 256; i++ {
		v, err := RandomToken(256)
		if err != nil {
			t.Fatalf("RandomToken: %v", err)
		}
		if seen[v] {
			t.Fatalf("RandomToken returned a duplicate after %d draws", i)
		}
		seen[v] = true
	}
}

func TestLoadKeyPairFromPrivateOnly(t *testing.T) {
	pair := mustGenerate(t)
	privPEM, err := EncodePrivateKeyPEM(pair.Private)
	if err != nil {
		t.Fatalf("EncodePrivateKeyPEM: %v", err)
	}
	path := t.TempDir() + "/priv.pem"
	if err := writeFile(path, privPEM); err != nil {
		t.Fatalf("writing: %v", err)
	}

	// publicPath empty: the public key must be derived from the private key.
	loaded, err := LoadKeyPair(path, "")
	if err != nil {
		t.Fatalf("LoadKeyPair: %v", err)
	}
	if loaded.Public.N.Cmp(pair.Public.N) != 0 {
		t.Error("public key was not derived from the private key")
	}
}

// helpers

func mustGenerate(t *testing.T) *KeyPair {
	t.Helper()
	pair, err := GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	return pair
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// rsaPublicKeyBytes renders a public key as PKIX DER, so an attacker-supplied
// HMAC key can be derived from it for the algorithm-confusion test.
func rsaPublicKeyBytes(pub *rsa.PublicKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}
