package connectutil

import (
	"testing"
)

func TestNewLoggingInterceptor(t *testing.T) {
	interceptor := NewLoggingInterceptor()
	if interceptor == nil {
		t.Fatal("expected non-nil interceptor")
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if len(opts) == 0 {
		t.Fatal("expected non-empty options")
	}
}

func TestDefaultClientOptions(t *testing.T) {
	opts := DefaultClientOptions()
	if len(opts) == 0 {
		t.Fatal("expected non-empty client options")
	}
}
