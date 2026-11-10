package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// trustedPublisherKeys holds the hex-encoded Ed25519 public keys whose
// signatures the client accepts on registry entries.
//
// It is intentionally empty: no key has been generated for the official
// registry yet, and a key must never be invented here. While it is empty:
//   - an entry that declares a signature fails to install (it cannot be
//     verified, so it is refused rather than ignored);
//   - an unsigned entry installs on the strength of its SHA-256 checksum only.
//
// As soon as at least one key is added, unsigned entries are refused too.
// The registry owner signs SigningPayload() of each entry with the matching
// private key and puts the hex signature in the entry's "signature" field.
var trustedPublisherKeys []string

// SignaturesRequired reports whether registry entries must carry a valid
// publisher signature. It is true once a trusted key is built in.
func SignaturesRequired() bool {
	return len(trustedPublisherKeys) > 0
}

// SigningPayload returns the canonical bytes a publisher signs for a
// registry entry. It covers the ID, the version, the SHA-256 of the binary
// and the normalized permission set, so a registry edit can neither swap the
// binary nor widen the permissions without a new signature.
func (p RegistryPlugin) SigningPayload() []byte {
	perms := p.Permissions.Normalized()
	if perms.Network == nil {
		perms.Network = []string{}
	}
	if perms.Storage == nil {
		perms.Storage = []string{}
	}
	if perms.Events == nil {
		perms.Events = []string{}
	}
	permJSON, _ := json.Marshal(struct {
		Network []string `json:"network"`
		Storage []string `json:"storage"`
		Events  []string `json:"events"`
	}{perms.Network, perms.Storage, perms.Events})
	var b strings.Builder
	b.WriteString("halpradio-plugin-v1\n")
	b.WriteString(p.ID)
	b.WriteByte('\n')
	b.WriteString(p.Version)
	b.WriteByte('\n')
	b.WriteString(strings.ToLower(strings.TrimSpace(p.Checksum)))
	b.WriteByte('\n')
	b.Write(permJSON)
	b.WriteByte('\n')
	return []byte(b.String())
}

// verifyPublisher enforces the signature policy described on
// trustedPublisherKeys against the given key set.
func verifyPublisher(p RegistryPlugin, keys []string) error {
	sig := strings.TrimSpace(p.Signature)
	if sig == "" {
		if len(keys) > 0 {
			return fmt.Errorf("plugin %s is not signed by a trusted publisher", p.ID)
		}
		return nil
	}
	if len(keys) == 0 {
		return fmt.Errorf("plugin %s declares a publisher signature, but this build has no trusted publisher key to verify it", p.ID)
	}
	payload := p.SigningPayload()
	for _, k := range keys {
		if VerifySignature(payload, sig, k) == nil {
			return nil
		}
	}
	return errors.New("publisher signature verification failed for plugin " + p.ID)
}
