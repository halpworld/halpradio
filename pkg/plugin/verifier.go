package plugin

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// CalculateSHA256 returns the hex-encoded SHA-256 checksum of data.
func CalculateSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// VerifyChecksum checks whether the given bytes match the expected SHA-256 hex string.
func VerifyChecksum(data []byte, expectedHex string) error {
	expectedHex = strings.TrimSpace(strings.ToLower(expectedHex))
	if expectedHex == "" {
		return fmt.Errorf("expected checksum is empty")
	}

	actualHex := CalculateSHA256(data)
	if actualHex != expectedHex {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHex, actualHex)
	}

	return nil
}

// VerifySignature verifies an Ed25519 signature for the given payload against a hex-encoded public key.
func VerifySignature(data []byte, signatureHex string, pubKeyHex string) error {
	signatureHex = strings.TrimSpace(signatureHex)
	pubKeyHex = strings.TrimSpace(pubKeyHex)

	if signatureHex == "" || pubKeyHex == "" {
		return fmt.Errorf("signature and public key cannot be empty")
	}

	sigBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("invalid signature hex: %w", err)
	}

	if len(sigBytes) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature length %d (expected %d)", len(sigBytes), ed25519.SignatureSize)
	}

	pubKeyBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return fmt.Errorf("invalid public key hex: %w", err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key length %d (expected %d)", len(pubKeyBytes), ed25519.PublicKeySize)
	}

	if !ed25519.Verify(pubKeyBytes, data, sigBytes) {
		return fmt.Errorf("cryptographic signature verification failed")
	}

	return nil
}
