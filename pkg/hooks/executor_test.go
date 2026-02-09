package hooks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voicetyped/voicetyped/pkg/urlvalidation"
)

func TestExecutorSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected application/json content type")
		}

		var req HookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if req.SessionID != "sess-1" {
			t.Errorf("session_id = %q, want %q", req.SessionID, "sess-1")
		}

		resp := HookResponse{
			Variables: map[string]string{"intent": "greeting"},
			Data:      map[string]any{"confidence": 0.95},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	exec := NewExecutor(nil, urlvalidation.AllowPrivateIPs())
	cfg := HookConfig{
		URL:        ts.URL,
		TimeoutSec: 5,
	}
	req := HookRequest{
		SessionID: "sess-1",
		State:     "greeting",
		Event:     "speech",
		Variables: map[string]string{"name": "Alice"},
	}

	resp, err := exec.Execute(t.Context(), cfg, req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if resp.Variables["intent"] != "greeting" {
		t.Errorf("intent = %q, want %q", resp.Variables["intent"], "greeting")
	}
}

func TestExecutorBearerAuth(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(HookResponse{})
	}))
	defer ts.Close()

	exec := NewExecutor(nil, urlvalidation.AllowPrivateIPs())
	cfg := HookConfig{
		URL:        ts.URL,
		AuthType:   "bearer",
		AuthSecret: "my-token",
		TimeoutSec: 5,
	}

	_, err := exec.Execute(t.Context(), cfg, HookRequest{SessionID: "s1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotAuth != "Bearer my-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-token")
	}
}

func TestExecutorHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	exec := NewExecutor(nil, urlvalidation.AllowPrivateIPs())
	cfg := HookConfig{URL: ts.URL, TimeoutSec: 5}

	_, err := exec.Execute(t.Context(), cfg, HookRequest{SessionID: "s1"})
	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}
