package dialog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoaderLoadAll(t *testing.T) {
	dir := t.TempDir()

	yamlContent := `
name: test-ivr
version: "1.0"
initial_state: welcome
states:
  welcome:
    on_enter:
      - type: play_tts
        params:
          text: "Welcome to our service"
    transitions:
      - event: speech
        target: process
      - event: dtmf
        target: menu
    timeout: "15s"
    timeout_next: goodbye
  process:
    transitions:
      - event: hook_result
        target: respond
  respond:
    transitions:
      - event: tts_complete
        target: welcome
  menu:
    transitions:
      - event: speech
        target: process
  goodbye:
    terminal: true
    on_enter:
      - type: play_tts
        params:
          text: "Goodbye!"
      - type: hangup
`

	if err := os.WriteFile(filepath.Join(dir, "test-ivr.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	loader := NewLoader(dir)
	dialogs, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if len(dialogs) != 1 {
		t.Fatalf("loaded %d dialogs, want 1", len(dialogs))
	}

	sm, ok := dialogs["test-ivr"]
	if !ok {
		t.Fatal("dialog 'test-ivr' not found")
	}

	if sm.InitialState() != "welcome" {
		t.Errorf("initial state = %q, want %q", sm.InitialState(), "welcome")
	}

	state, ok := sm.GetState("welcome")
	if !ok {
		t.Fatal("state 'welcome' not found")
	}

	if len(state.OnEnter) != 1 {
		t.Errorf("on_enter has %d actions, want 1", len(state.OnEnter))
	}

	if state.Timeout != "15s" {
		t.Errorf("timeout = %q, want %q", state.Timeout, "15s")
	}
}

func TestLoaderInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("{{invalid yaml"), 0644)

	loader := NewLoader(dir)
	_, err := loader.LoadAll()
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoaderEmptyDir(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)
	dialogs, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(dialogs) != 0 {
		t.Errorf("loaded %d dialogs, want 0", len(dialogs))
	}
}
