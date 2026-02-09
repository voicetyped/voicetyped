package dialog

import (
	"context"
	"testing"
	"time"
)

func TestEngineRunDialog(t *testing.T) {
	d := sampleDialog()
	sm := NewStateMachine(d)
	dialogs := map[string]*StateMachine{d.Name: sm}

	engine := NewEngine(dialogs, nil, nil)

	session := NewSession("s1", d.Name, d.InitialState)

	speechCh := make(chan ASRResult, 1)
	dtmfCh := make(chan rune, 1)

	var spoken []string
	speakFn := func(text string) error {
		spoken = append(spoken, text)
		return nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	// Send a speech event to move from greeting -> process.
	go func() {
		time.Sleep(50 * time.Millisecond)
		speechCh <- ASRResult{Text: "I need help", IsFinal: true}
		time.Sleep(50 * time.Millisecond)
		// Close channels to end the dialog.
		close(speechCh)
	}()

	err := engine.RunDialog(ctx, session, speechCh, dtmfCh, speakFn)
	if err != nil {
		t.Fatalf("RunDialog: %v", err)
	}

	if len(spoken) == 0 {
		t.Error("expected at least one TTS output")
	}
	if spoken[0] != "Hello!" {
		t.Errorf("first spoken = %q, want %q", spoken[0], "Hello!")
	}

	if session.CurrentState != "process" {
		t.Errorf("final state = %q, want %q", session.CurrentState, "process")
	}

	if len(session.History) != 1 {
		t.Errorf("history length = %d, want 1", len(session.History))
	}
}

func TestEngineTimeout(t *testing.T) {
	d := &Dialog{
		Name:         "timeout-test",
		InitialState: "start",
		States: map[string]State{
			"start": {
				Timeout:     "50ms",
				TimeoutNext: "end",
			},
			"end": {
				Terminal: true,
			},
		},
	}
	sm := NewStateMachine(d)
	dialogs := map[string]*StateMachine{d.Name: sm}

	engine := NewEngine(dialogs, nil, nil)
	session := NewSession("s1", d.Name, d.InitialState)

	speechCh := make(chan ASRResult)
	dtmfCh := make(chan rune)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	err := engine.RunDialog(ctx, session, speechCh, dtmfCh, nil)
	if err != nil {
		t.Fatalf("RunDialog: %v", err)
	}

	if session.CurrentState != "end" {
		t.Errorf("final state = %q, want %q", session.CurrentState, "end")
	}
}

func TestEngineDialogNotFound(t *testing.T) {
	engine := NewEngine(map[string]*StateMachine{}, nil, nil)
	session := NewSession("s1", "nonexistent", "start")

	err := engine.RunDialog(t.Context(), session, nil, nil, nil)
	if err == nil {
		t.Error("expected error for missing dialog")
	}
}
