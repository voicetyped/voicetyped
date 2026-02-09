package webhook

import (
	"strings"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	secret := "test-secret-key"
	payload := []byte(`{"type":"call.started","data":{}}`)

	sig := Sign(secret, payload)

	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("signature should start with 'sha256=', got %q", sig)
	}

	if !Verify(secret, payload, sig) {
		t.Error("Verify should return true for valid signature")
	}

	if Verify("wrong-secret", payload, sig) {
		t.Error("Verify should return false for wrong secret")
	}

	if Verify(secret, []byte("tampered"), sig) {
		t.Error("Verify should return false for tampered payload")
	}
}

func TestGenerateSecret(t *testing.T) {
	s1, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}

	if len(s1) != 64 {
		t.Errorf("secret length = %d, want 64 hex chars", len(s1))
	}

	s2, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}

	if s1 == s2 {
		t.Error("two generated secrets should differ")
	}
}
