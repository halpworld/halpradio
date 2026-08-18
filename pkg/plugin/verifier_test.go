package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestChecksumVerification(t *testing.T) {
	data := []byte("Hello halpradio plugins!")
	checksum := CalculateSHA256(data)

	if err := VerifyChecksum(data, checksum); err != nil {
		t.Fatalf("expected valid checksum, got error: %v", err)
	}

	if err := VerifyChecksum(data, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Errorf("expected checksum mismatch error, got nil")
	}
}

func TestSignatureVerification(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}

	payload := []byte("plugin-wasm-binary-content")
	sig := ed25519.Sign(privKey, payload)

	sigHex := hex.EncodeToString(sig)
	pubHex := hex.EncodeToString(pubKey)

	if err := VerifySignature(payload, sigHex, pubHex); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}

	// Tampered payload
	tampered := []byte("tampered-wasm-binary-content")
	if err := VerifySignature(tampered, sigHex, pubHex); err == nil {
		t.Errorf("expected verification failure for tampered payload, got nil")
	}
}
