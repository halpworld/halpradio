package radio

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// CatalogSigningPublicKey is the base64 (standard encoding) Ed25519 public
// key that signs the remote station catalog. It is empty until the project
// publishes a signing key. While it is empty, catalog updates are protected by
// https, size limits, per-entry validation and the rollback and plausibility
// checks only. Once it is set, every downloaded catalog must come with a
// valid "<url>.sig" signature, and a cached catalog without a valid ".sig"
// next to it is ignored. A key that is set but malformed fails closed: no
// catalog update is accepted.
var CatalogSigningPublicKey = ""

var errBadSigningKey = errors.New("catalog signing key is malformed")

// catalogSigningKey returns the configured public key, nil when none is
// configured, or an invalid (zero-length, non-nil) key when the configured
// value is malformed, so that verification fails closed.
func catalogSigningKey() ed25519.PublicKey {
	raw := strings.TrimSpace(CatalogSigningPublicKey)
	if raw == "" {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return ed25519.PublicKey{}
	}
	return ed25519.PublicKey(key)
}

// verifyCatalogSignature checks a base64 Ed25519 signature over body
// against CatalogSigningPublicKey. With no key configured it accepts.
func verifyCatalogSignature(body, sigText []byte) error {
	key := catalogSigningKey()
	if key == nil {
		return nil
	}
	if len(key) != ed25519.PublicKeySize {
		return errBadSigningKey
	}
	sig, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(sigText)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("catalog signature is malformed")
	}
	if !ed25519.Verify(key, body, sig) {
		return fmt.Errorf("catalog signature does not verify")
	}
	return nil
}

// verifyCachedCatalog re-checks the signature of a cached catalog before it
// is loaded. With no key configured it accepts.
func verifyCachedCatalog(cacheFile string, data []byte) error {
	if catalogSigningKey() == nil {
		return nil
	}
	sig, err := os.ReadFile(cacheFile + ".sig")
	if err != nil {
		return fmt.Errorf("cached catalog has no signature")
	}
	if len(sig) > 1024 {
		return fmt.Errorf("cached catalog signature is malformed")
	}
	return verifyCatalogSignature(data, sig)
}
