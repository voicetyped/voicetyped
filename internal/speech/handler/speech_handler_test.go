package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	speechv1 "github.com/voicetyped/voicetyped/gen/voicetyped/speech/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/speech/v1/speechv1connect"
	"github.com/voicetyped/voicetyped/internal/speech/registry"

	// Register backends for testing.
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/deepgram"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/elevenlabs"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/google"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/openai"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/piper"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/whisper"
)

func setupSpeechTestServer(t *testing.T) (speechv1connect.SpeechServiceClient, func()) {
	t.Helper()
	handler := NewSpeechHandler("whisper", "piper", nil, nil)

	mux := http.NewServeMux()
	path, hdlr := speechv1connect.NewSpeechServiceHandler(handler)
	mux.Handle(path, hdlr)

	server := httptest.NewServer(mux)
	client := speechv1connect.NewSpeechServiceClient(http.DefaultClient, server.URL)

	return client, server.Close
}

func TestListBackends(t *testing.T) {
	client, cleanup := setupSpeechTestServer(t)
	defer cleanup()

	resp, err := client.ListBackends(context.Background(), connect.NewRequest(&speechv1.ListBackendsRequest{}))
	if err != nil {
		t.Fatalf("ListBackends: %v", err)
	}

	if len(resp.Msg.AsrBackends) == 0 {
		t.Error("expected at least one ASR backend")
	}
	if len(resp.Msg.TtsBackends) == 0 {
		t.Error("expected at least one TTS backend")
	}

	// Verify known backends are present.
	asrNames := make(map[string]bool)
	for _, b := range resp.Msg.AsrBackends {
		asrNames[b.Name] = true
		if b.Type != "asr" {
			t.Errorf("ASR backend %q has type %q, want 'asr'", b.Name, b.Type)
		}
	}

	ttsNames := make(map[string]bool)
	for _, b := range resp.Msg.TtsBackends {
		ttsNames[b.Name] = true
		if b.Type != "tts" {
			t.Errorf("TTS backend %q has type %q, want 'tts'", b.Name, b.Type)
		}
	}

	expectedASR := []string{"whisper", "deepgram", "google", "openai"}
	for _, name := range expectedASR {
		if !asrNames[name] {
			t.Errorf("expected ASR backend %q to be registered", name)
		}
	}

	expectedTTS := []string{"piper", "google", "elevenlabs", "openai"}
	for _, name := range expectedTTS {
		if !ttsNames[name] {
			t.Errorf("expected TTS backend %q to be registered", name)
		}
	}
}

func TestListVoices(t *testing.T) {
	client, cleanup := setupSpeechTestServer(t)
	defer cleanup()

	resp, err := client.ListVoices(context.Background(), connect.NewRequest(&speechv1.ListVoicesRequest{}))
	if err != nil {
		t.Fatalf("ListVoices: %v", err)
	}

	// At minimum piper should report voices.
	if len(resp.Msg.Voices) == 0 {
		t.Error("expected at least one voice")
	}

	for _, v := range resp.Msg.Voices {
		if v.Id == "" {
			t.Error("voice ID should not be empty")
		}
		if v.Backend == "" {
			t.Error("voice Backend should not be empty")
		}
	}
}

func TestListVoicesFilterByBackend(t *testing.T) {
	client, cleanup := setupSpeechTestServer(t)
	defer cleanup()

	resp, err := client.ListVoices(context.Background(), connect.NewRequest(&speechv1.ListVoicesRequest{
		Backend: "piper",
	}))
	if err != nil {
		t.Fatalf("ListVoices: %v", err)
	}

	for _, v := range resp.Msg.Voices {
		if v.Backend != "piper" {
			t.Errorf("got voice from backend %q, expected only piper", v.Backend)
		}
	}
}

func TestNewSpeechHandlerDefaults(t *testing.T) {
	h := NewSpeechHandler("", "", nil, nil)
	if h.defaultASRBackend != "whisper" {
		t.Errorf("default ASR = %q, want whisper", h.defaultASRBackend)
	}
	if h.defaultTTSBackend != "piper" {
		t.Errorf("default TTS = %q, want piper", h.defaultTTSBackend)
	}
}

func TestRegistryHasExpectedBackends(t *testing.T) {
	asrList := registry.ASR.List()
	ttsList := registry.TTS.List()

	if len(asrList) < 4 {
		t.Errorf("expected at least 4 ASR backends, got %d: %v", len(asrList), asrList)
	}
	if len(ttsList) < 4 {
		t.Errorf("expected at least 4 TTS backends, got %d: %v", len(ttsList), ttsList)
	}

	// Verify key backends are registered.
	if !registry.ASR.Has("whisper") {
		t.Error("whisper ASR backend not registered")
	}
	if !registry.ASR.Has("openai") {
		t.Error("openai ASR backend not registered")
	}
	if !registry.TTS.Has("piper") {
		t.Error("piper TTS backend not registered")
	}
	if !registry.TTS.Has("openai") {
		t.Error("openai TTS backend not registered")
	}
}

func TestListModels(t *testing.T) {
	client, cleanup := setupSpeechTestServer(t)
	defer cleanup()

	// List all models.
	resp, err := client.ListModels(context.Background(), connect.NewRequest(&speechv1.ListModelsRequest{}))
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	if len(resp.Msg.Models) == 0 {
		t.Fatal("expected at least one model")
	}

	// Verify we get both ASR and TTS models.
	hasASR, hasTTS := false, false
	for _, m := range resp.Msg.Models {
		if m.Type == "asr" {
			hasASR = true
		}
		if m.Type == "tts" {
			hasTTS = true
		}
		if m.Id == "" {
			t.Error("model ID should not be empty")
		}
		if m.Backend == "" {
			t.Error("model backend should not be empty")
		}
	}
	if !hasASR {
		t.Error("expected at least one ASR model")
	}
	if !hasTTS {
		t.Error("expected at least one TTS model")
	}

	// Test filter by type.
	resp, err = client.ListModels(context.Background(), connect.NewRequest(&speechv1.ListModelsRequest{
		Type: "asr",
	}))
	if err != nil {
		t.Fatalf("ListModels(type=asr): %v", err)
	}
	for _, m := range resp.Msg.Models {
		if m.Type != "asr" {
			t.Errorf("got model type %q, expected asr", m.Type)
		}
	}

	// Test filter by backend.
	resp, err = client.ListModels(context.Background(), connect.NewRequest(&speechv1.ListModelsRequest{
		Backend: "whisper",
	}))
	if err != nil {
		t.Fatalf("ListModels(backend=whisper): %v", err)
	}
	if len(resp.Msg.Models) == 0 {
		t.Error("expected whisper models")
	}
	for _, m := range resp.Msg.Models {
		if m.Backend != "whisper" {
			t.Errorf("got model from backend %q, expected whisper", m.Backend)
		}
	}
}

func TestListBackendsIncludesModelInfo(t *testing.T) {
	client, cleanup := setupSpeechTestServer(t)
	defer cleanup()

	resp, err := client.ListBackends(context.Background(), connect.NewRequest(&speechv1.ListBackendsRequest{}))
	if err != nil {
		t.Fatalf("ListBackends: %v", err)
	}

	// Whisper should have models listed.
	for _, b := range resp.Msg.AsrBackends {
		if b.Name == "whisper" {
			if len(b.Models) == 0 {
				t.Error("whisper backend should list models")
			}
			if b.DefaultModel == "" {
				t.Error("whisper backend should have a default model")
			}
			return
		}
	}
	t.Error("whisper backend not found in ListBackends response")
}
