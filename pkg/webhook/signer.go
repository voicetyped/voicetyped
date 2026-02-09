package webhook

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SignatureHeader is the HTTP header name carrying the HMAC signature.
const SignatureHeader = "X-Voicetyped-Signature-256"

// Sign produces an HMAC-SHA256 signature in the format "sha256=<hex>".
func Sign(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return fmt.Sprintf("sha256=%s", hex.EncodeToString(mac.Sum(nil)))
}

// Verify checks that the given signature matches the expected HMAC.
func Verify(secret string, payload []byte, signature string) bool {
	expected := Sign(secret, payload)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// GenerateSecret returns a cryptographically random 32-byte hex string.
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
