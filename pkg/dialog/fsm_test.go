package dialog

import "testing"

func sampleDialog() *Dialog {
	return &Dialog{
		Name:         "test-dialog",
		Version:      "1.0",
		InitialState: "greeting",
		States: map[string]State{
			"greeting": {
				OnEnter: []Action{
					{Type: "play_tts", Params: map[string]string{"text": "Hello!"}},
				},
				Transitions: []Transition{
					{Event: "speech", Target: "process"},
					{Event: "dtmf", Condition: `{{ eq (printf "%c" .Event) "1" }}`, Target: "menu"},
				},
				Timeout:     "10s",
				TimeoutNext: "goodbye",
			},
			"process": {
				Transitions: []Transition{
					{Event: "hook_result", Target: "respond"},
				},
			},
			"respond": {
				Transitions: []Transition{
					{Event: "tts_complete", Target: "greeting"},
				},
			},
			"menu": {
				Transitions: []Transition{
					{Event: "speech", Target: "process"},
				},
			},
			"goodbye": {
				Terminal: true,
			},
		},
	}
}

func TestStateMachineValidate(t *testing.T) {
	sm := NewStateMachine(sampleDialog())
	if err := sm.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestStateMachineValidateErrors(t *testing.T) {
	tests := []struct {
		name   string
		modify func(d *Dialog)
	}{
		{
			name:   "missing initial state",
			modify: func(d *Dialog) { d.InitialState = "" },
		},
		{
			name:   "initial state not found",
			modify: func(d *Dialog) { d.InitialState = "nonexistent" },
		},
		{
			name: "transition target not found",
			modify: func(d *Dialog) {
				d.States["greeting"] = State{
					Transitions: []Transition{{Event: "speech", Target: "missing"}},
				}
			},
		},
		{
			name: "timeout_next not found",
			modify: func(d *Dialog) {
				s := d.States["greeting"]
				s.TimeoutNext = "missing"
				d.States["greeting"] = s
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := sampleDialog()
			tt.modify(d)
			sm := NewStateMachine(d)
			if err := sm.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestEvaluateTransitions(t *testing.T) {
	sm := NewStateMachine(sampleDialog())
	state, _ := sm.GetState("greeting")
	session := NewSession("s1", "test-dialog", "greeting")

	target, _, err := sm.EvaluateTransitions(state, "speech", session)
	if err != nil {
		t.Fatalf("EvaluateTransitions: %v", err)
	}
	if target != "process" {
		t.Errorf("target = %q, want %q", target, "process")
	}
}

func TestEvaluateTransitionsNoMatch(t *testing.T) {
	sm := NewStateMachine(sampleDialog())
	state, _ := sm.GetState("greeting")
	session := NewSession("s1", "test-dialog", "greeting")

	target, _, err := sm.EvaluateTransitions(state, "unknown_event", session)
	if err != nil {
		t.Fatalf("EvaluateTransitions: %v", err)
	}
	if target != "" {
		t.Errorf("target = %q, want empty", target)
	}
}
