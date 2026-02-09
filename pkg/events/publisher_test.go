package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelopeSerialization(t *testing.T) {
	data := &CallStartedData{
		CallerID:     "+15551234567",
		CalledNumber: "+15559876543",
		Protocol:     "sip",
	}

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}

	env := Envelope{
		ID:        "test-id",
		Type:      CallStarted,
		Source:    "media",
		SessionID: "session-123",
		Timestamp: time.Now().UTC(),
		Data:      raw,
	}

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var decoded Envelope
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if decoded.Type != CallStarted {
		t.Errorf("type = %q, want %q", decoded.Type, CallStarted)
	}
	if decoded.Source != "media" {
		t.Errorf("source = %q, want %q", decoded.Source, "media")
	}
	if decoded.SessionID != "session-123" {
		t.Errorf("session_id = %q, want %q", decoded.SessionID, "session-123")
	}

	var payload CallStartedData
	if err := json.Unmarshal(decoded.Data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.CallerID != "+15551234567" {
		t.Errorf("caller_id = %q, want %q", payload.CallerID, "+15551234567")
	}
}

func TestEventTypeConstants(t *testing.T) {
	types := []EventType{
		CallStarted, CallTerminated,
		SpeechPartial, SpeechFinal,
		DTMFReceived, StateTransition,
		ActionExecuted, HookResult, HookError,
		TTSStarted, TTSCompleted,
		SystemError, WebhookTest,
	}

	seen := make(map[EventType]bool)
	for _, et := range types {
		if et == "" {
			t.Error("empty event type constant")
		}
		if seen[et] {
			t.Errorf("duplicate event type: %q", et)
		}
		seen[et] = true
	}
}
