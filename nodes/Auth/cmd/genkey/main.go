// Command genkey generates an RSA signing key pair for the auth node.
//
// The private key is what lets its holder mint access tokens, so it must be
// treated as a secret: keep it out of version control, and distribute it to the
// auth node through a mounted Secret rather than baking it into an image.
//
// Usage:
//
//	genkey -out /path/to/keys [-bits 2048] [-kid-override name]
//
// Writes <out>/jwt_private.pem (mode 0600) and <out>/jwt_public.pem (mode 0644).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"Piranid/pkg/authn"
)

func main() {
	outDir := flag.String("out", "jwt-keys", "directory to write the key pair into")
	bits := flag.Int("bits", 2048, "RSA modulus size in bits")
	kidOverride := flag.String("kid-override", "", "use this key id instead of the derived thumbprint")
	force := flag.Bool("force", false, "overwrite existing keys")
	flag.Parse()

	if err := run(*outDir, *bits, *kidOverride, *force); err != nil {
		fmt.Fprintf(os.Stderr, "genkey: %v\n", err)
		os.Exit(1)
	}
}

func run(outDir string, bits int, kidOverride string, force bool) error {
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}

	privPath := filepath.Join(outDir, "jwt_private.pem")
	pubPath := filepath.Join(outDir, "jwt_public.pem")

	if !force {
		for _, p := range []string{privPath, pubPath} {
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("%s already exists; pass -force to overwrite (overwriting invalidates every issued token)", p)
			}
		}
	}

	fmt.Printf("generating a %d-bit RSA key pair...\n", bits)
	pair, err := authn.GenerateKeyPair(bits)
	if err != nil {
		return err
	}
	if kidOverride != "" {
		pair.Kid = kidOverride
	}

	privPEM, err := authn.EncodePrivateKeyPEM(pair.Private)
	if err != nil {
		return err
	}
	pubPEM, err := authn.EncodePublicKeyPEM(pair.Public)
	if err != nil {
		return err
	}

	// 0600 on the private key: it is readable only by the auth node's user.
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", privPath, err)
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", pubPath, err)
	}

	fmt.Printf("\nwrote %s (mode 0600)\n", privPath)
	fmt.Printf("wrote %s\n", pubPath)
	fmt.Printf("\nkey id (kid): %s\n", pair.Kid)
	fmt.Printf("\nConfigure the auth node with:\n")
	fmt.Printf("  AUTH_PRIVATE_KEY_PATH=%s\n", privPath)
	fmt.Printf("  AUTH_PUBLIC_KEY_PATH=%s\n", pubPath)
	fmt.Printf("  AUTH_ISSUER=https://auth.piranid.local\n")
	fmt.Printf("  AUTH_AUDIENCE=event-queue\n")
	fmt.Printf("\nOnly the public key needs to reach services: they fetch it from\n")
	fmt.Printf("the auth node's /.well-known/jwks.json endpoint.\n")

	return nil
}
