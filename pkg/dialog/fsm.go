package dialog

import "fmt"

// StateMachine validates and provides access to dialog states.
type StateMachine struct {
	dialog *Dialog
}

// NewStateMachine creates a state machine from a dialog definition.
func NewStateMachine(d *Dialog) *StateMachine {
	return &StateMachine{dialog: d}
}

// Validate checks the dialog definition for consistency.
func (sm *StateMachine) Validate() error {
	if sm.dialog.InitialState == "" {
		return fmt.Errorf("dialog %q: initial_state is required", sm.dialog.Name)
	}

	if _, ok := sm.dialog.States[sm.dialog.InitialState]; !ok {
		return fmt.Errorf("dialog %q: initial_state %q not found in states",
			sm.dialog.Name, sm.dialog.InitialState)
	}

	for name, state := range sm.dialog.States {
		for i, t := range state.Transitions {
			if t.Target == "" {
				return fmt.Errorf("dialog %q state %q transition %d: target is required",
					sm.dialog.Name, name, i)
			}
			if _, ok := sm.dialog.States[t.Target]; !ok {
				return fmt.Errorf("dialog %q state %q transition %d: target %q not found",
					sm.dialog.Name, name, i, t.Target)
			}
		}
		if state.TimeoutNext != "" {
			if _, ok := sm.dialog.States[state.TimeoutNext]; !ok {
				return fmt.Errorf("dialog %q state %q: timeout_next %q not found",
					sm.dialog.Name, name, state.TimeoutNext)
			}
		}
	}

	return nil
}

// GetState returns the state definition for the given name.
func (sm *StateMachine) GetState(name string) (State, bool) {
	s, ok := sm.dialog.States[name]
	return s, ok
}

// InitialState returns the initial state name.
func (sm *StateMachine) InitialState() string {
	return sm.dialog.InitialState
}

// Dialog returns the underlying dialog definition.
func (sm *StateMachine) Dialog() *Dialog {
	return sm.dialog
}

// EvaluateTransitions checks all transitions for the given event type
// and returns the first matching target state.
func (sm *StateMachine) EvaluateTransitions(state State, event string, session *Session) (string, []Action, error) {
	for _, t := range state.Transitions {
		if t.Event != event {
			continue
		}

		match, err := EvalCondition(t.Condition, session)
		if err != nil {
			return "", nil, fmt.Errorf("eval condition %q: %w", t.Condition, err)
		}
		if match {
			return t.Target, t.Actions, nil
		}
	}
	return "", nil, nil
}
