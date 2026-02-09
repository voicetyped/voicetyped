package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"

	dialogv1 "github.com/voicetyped/voicetyped/gen/voicetyped/dialog/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/dialog/v1/dialogv1connect"
	"github.com/voicetyped/voicetyped/pkg/dialog"
	"github.com/voicetyped/voicetyped/pkg/hooks"
)

const testDialogYAML = `
name: test-dialog
version: "1.0"
description: "Test dialog for handler tests"
initial_state: greeting
states:
  greeting:
    on_enter:
      - type: play_tts
        params:
          text: "Hello, how can I help you?"
    transitions:
      - event: speech
        target: handle_input
    timeout: "30s"
    timeout_next: goodbye
  handle_input:
    on_enter:
      - type: play_tts
        params:
          text: "I heard you say something."
    transitions:
      - event: speech
        target: goodbye
  goodbye:
    on_enter:
      - type: play_tts
        params:
          text: "Goodbye!"
      - type: hangup
    terminal: true
`

func setupDialogTestServer(t *testing.T) (dialogv1connect.DialogServiceClient, func()) {
	t.Helper()

	// Write test dialog to temp dir.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test-dialog.yaml"), []byte(testDialogYAML), 0644); err != nil {
		t.Fatalf("write test dialog: %v", err)
	}

	loader := dialog.NewLoader(dir)
	if _, err := loader.LoadAll(); err != nil {
		t.Fatalf("load dialogs: %v", err)
	}

	hookExec := hooks.NewExecutor(nil)
	handler := NewDialogHandler(loader, hookExec, nil, nil)

	mux := http.NewServeMux()
	path, hdlr := dialogv1connect.NewDialogServiceHandler(handler)
	mux.Handle(path, hdlr)

	server := httptest.NewServer(mux)
	client := dialogv1connect.NewDialogServiceClient(http.DefaultClient, server.URL)

	return client, server.Close
}

func TestStartDialog(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	resp, err := client.StartDialog(context.Background(), connect.NewRequest(&dialogv1.StartDialogRequest{
		SessionId:  "session-1",
		DialogName: "test-dialog",
	}))
	if err != nil {
		t.Fatalf("StartDialog: %v", err)
	}

	if resp.Msg.SessionId != "session-1" {
		t.Errorf("got session ID %q, want session-1", resp.Msg.SessionId)
	}
	if resp.Msg.CurrentState != "greeting" {
		t.Errorf("got state %q, want greeting", resp.Msg.CurrentState)
	}
	if len(resp.Msg.Actions) == 0 {
		t.Error("expected on_enter actions for greeting state")
	}
	if resp.Msg.Actions[0].Type != "play_tts" {
		t.Errorf("got action type %q, want play_tts", resp.Msg.Actions[0].Type)
	}

	// Clean up.
	_, _ = client.EndDialog(context.Background(), connect.NewRequest(&dialogv1.EndDialogRequest{
		SessionId: "session-1",
	}))
}

func TestStartDialogNotFound(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	_, err := client.StartDialog(context.Background(), connect.NewRequest(&dialogv1.StartDialogRequest{
		SessionId:  "session-x",
		DialogName: "nonexistent",
	}))
	if err == nil {
		t.Fatal("expected error for nonexistent dialog")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("got code %v, want NotFound", connect.CodeOf(err))
	}
}

func TestSendEvent(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	// Start dialog first.
	_, err := client.StartDialog(context.Background(), connect.NewRequest(&dialogv1.StartDialogRequest{
		SessionId:  "session-2",
		DialogName: "test-dialog",
	}))
	if err != nil {
		t.Fatalf("StartDialog: %v", err)
	}

	// Send speech event to transition from greeting to handle_input.
	resp, err := client.SendEvent(context.Background(), connect.NewRequest(&dialogv1.SendEventRequest{
		SessionId: "session-2",
		EventType: "speech",
		EventData: "hello there",
	}))
	if err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	if resp.Msg.PreviousState != "greeting" {
		t.Errorf("got previous state %q, want greeting", resp.Msg.PreviousState)
	}
	if resp.Msg.CurrentState != "handle_input" {
		t.Errorf("got current state %q, want handle_input", resp.Msg.CurrentState)
	}
	if resp.Msg.Terminal {
		t.Error("expected non-terminal state")
	}

	// Clean up.
	_, _ = client.EndDialog(context.Background(), connect.NewRequest(&dialogv1.EndDialogRequest{
		SessionId: "session-2",
	}))
}

func TestSendEventToTerminal(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	_, err := client.StartDialog(context.Background(), connect.NewRequest(&dialogv1.StartDialogRequest{
		SessionId:  "session-3",
		DialogName: "test-dialog",
	}))
	if err != nil {
		t.Fatalf("StartDialog: %v", err)
	}

	// Move to handle_input.
	_, err = client.SendEvent(context.Background(), connect.NewRequest(&dialogv1.SendEventRequest{
		SessionId: "session-3",
		EventType: "speech",
		EventData: "hello",
	}))
	if err != nil {
		t.Fatalf("first SendEvent: %v", err)
	}

	// Move to goodbye (terminal).
	resp, err := client.SendEvent(context.Background(), connect.NewRequest(&dialogv1.SendEventRequest{
		SessionId: "session-3",
		EventType: "speech",
		EventData: "goodbye",
	}))
	if err != nil {
		t.Fatalf("second SendEvent: %v", err)
	}

	if resp.Msg.CurrentState != "goodbye" {
		t.Errorf("got state %q, want goodbye", resp.Msg.CurrentState)
	}
	if !resp.Msg.Terminal {
		t.Error("expected terminal state")
	}

	// Clean up.
	_, _ = client.EndDialog(context.Background(), connect.NewRequest(&dialogv1.EndDialogRequest{
		SessionId: "session-3",
	}))
}

func TestSendEventInvalidType(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	_, err := client.StartDialog(context.Background(), connect.NewRequest(&dialogv1.StartDialogRequest{
		SessionId:  "session-4",
		DialogName: "test-dialog",
	}))
	if err != nil {
		t.Fatalf("StartDialog: %v", err)
	}

	_, err = client.SendEvent(context.Background(), connect.NewRequest(&dialogv1.SendEventRequest{
		SessionId: "session-4",
		EventType: "invalid",
		EventData: "foo",
	}))
	if err == nil {
		t.Fatal("expected error for invalid event type")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("got code %v, want InvalidArgument", connect.CodeOf(err))
	}

	_, _ = client.EndDialog(context.Background(), connect.NewRequest(&dialogv1.EndDialogRequest{
		SessionId: "session-4",
	}))
}

func TestSendEventSessionNotFound(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	_, err := client.SendEvent(context.Background(), connect.NewRequest(&dialogv1.SendEventRequest{
		SessionId: "nonexistent",
		EventType: "speech",
		EventData: "hello",
	}))
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("got code %v, want NotFound", connect.CodeOf(err))
	}
}

func TestGetSession(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	_, err := client.StartDialog(context.Background(), connect.NewRequest(&dialogv1.StartDialogRequest{
		SessionId:  "session-5",
		DialogName: "test-dialog",
		Variables:  map[string]string{"key": "value"},
	}))
	if err != nil {
		t.Fatalf("StartDialog: %v", err)
	}

	resp, err := client.GetSession(context.Background(), connect.NewRequest(&dialogv1.GetSessionRequest{
		SessionId: "session-5",
	}))
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	if resp.Msg.SessionId != "session-5" {
		t.Errorf("got session ID %q, want session-5", resp.Msg.SessionId)
	}
	if resp.Msg.DialogName != "test-dialog" {
		t.Errorf("got dialog name %q, want test-dialog", resp.Msg.DialogName)
	}
	if resp.Msg.CurrentState != "greeting" {
		t.Errorf("got state %q, want greeting", resp.Msg.CurrentState)
	}
	if resp.Msg.Variables["key"] != "value" {
		t.Errorf("got variables %v, want key=value", resp.Msg.Variables)
	}

	_, _ = client.EndDialog(context.Background(), connect.NewRequest(&dialogv1.EndDialogRequest{
		SessionId: "session-5",
	}))
}

func TestEndDialog(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	_, err := client.StartDialog(context.Background(), connect.NewRequest(&dialogv1.StartDialogRequest{
		SessionId:  "session-6",
		DialogName: "test-dialog",
	}))
	if err != nil {
		t.Fatalf("StartDialog: %v", err)
	}

	_, err = client.EndDialog(context.Background(), connect.NewRequest(&dialogv1.EndDialogRequest{
		SessionId: "session-6",
	}))
	if err != nil {
		t.Fatalf("EndDialog: %v", err)
	}

	// Session should no longer exist.
	_, err = client.GetSession(context.Background(), connect.NewRequest(&dialogv1.GetSessionRequest{
		SessionId: "session-6",
	}))
	if err == nil {
		t.Fatal("expected error after EndDialog")
	}
}

func TestEndDialogNotFound(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	_, err := client.EndDialog(context.Background(), connect.NewRequest(&dialogv1.EndDialogRequest{
		SessionId: "nonexistent",
	}))
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestListDialogs(t *testing.T) {
	client, cleanup := setupDialogTestServer(t)
	defer cleanup()

	resp, err := client.ListDialogs(context.Background(), connect.NewRequest(&dialogv1.ListDialogsRequest{}))
	if err != nil {
		t.Fatalf("ListDialogs: %v", err)
	}

	if len(resp.Msg.Dialogs) == 0 {
		t.Fatal("expected at least one dialog")
	}

	found := false
	for _, d := range resp.Msg.Dialogs {
		if d.Name == "test-dialog" {
			found = true
			if d.InitialState != "greeting" {
				t.Errorf("got initial state %q, want greeting", d.InitialState)
			}
			if len(d.States) != 3 {
				t.Errorf("got %d states, want 3", len(d.States))
			}
		}
	}
	if !found {
		t.Error("test-dialog not found in list")
	}
}
