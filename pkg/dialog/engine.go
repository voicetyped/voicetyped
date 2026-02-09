package dialog

import (
	"context"
	"fmt"
	"time"

	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/hooks"
)

// SpeakFunc is a function that synthesizes and plays text to the caller.
type SpeakFunc func(text string) error

// ASRResult represents a speech recognition result passed to the engine.
type ASRResult struct {
	Text       string
	Confidence float32
	IsFinal    bool
}

// Engine runs dialog state machines for active calls.
type Engine struct {
	dialogs   map[string]*StateMachine
	hooks     *hooks.Executor
	publisher *events.Publisher
}

// NewEngine creates a new dialog engine.
func NewEngine(dialogs map[string]*StateMachine, hookExec *hooks.Executor, pub *events.Publisher) *Engine {
	return &Engine{
		dialogs:   dialogs,
		hooks:     hookExec,
		publisher: pub,
	}
}

// RunDialog is the core event loop for a single call.
func (e *Engine) RunDialog(ctx context.Context, session *Session, speechCh <-chan ASRResult, dtmfCh <-chan rune, speakFn SpeakFunc) error {
	sm, ok := e.dialogs[session.DialogName]
	if !ok {
		return fmt.Errorf("dialog %q not found", session.DialogName)
	}

	state, ok := sm.GetState(session.GetCurrentState())
	if !ok {
		return fmt.Errorf("state %q not found in dialog %q", session.GetCurrentState(), session.DialogName)
	}

	// Execute on_enter actions for initial state.
	if err := e.executeActions(ctx, session, state.OnEnter, speakFn); err != nil {
		return err
	}

	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		// Set up timeout channel.
		if dur, err := time.ParseDuration(state.Timeout); err == nil && dur > 0 {
			if timer == nil {
				timer = time.NewTimer(dur)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(dur)
			}
		} else if timer != nil {
			timer.Stop()
			timer = nil
		}

		var timeoutCh <-chan time.Time
		if timer != nil {
			timeoutCh = timer.C
		}

		select {
		case <-ctx.Done():
			return ctx.Err()

		case result, ok := <-speechCh:
			if !ok {
				return nil
			}
			if !result.IsFinal {
				continue
			}
			session.SetLastEvent(result.Text)
			nextState, actions, err := sm.EvaluateTransitions(state, "speech", session)
			if err != nil {
				return err
			}
			if nextState != "" {
				if err := e.executeActions(ctx, session, actions, speakFn); err != nil {
					return err
				}
				state, err = e.transition(ctx, session, sm, nextState, speakFn)
				if err != nil {
					return err
				}
			}

		case digit, ok := <-dtmfCh:
			if !ok {
				return nil
			}
			session.SetLastEvent(digit)
			nextState, actions, err := sm.EvaluateTransitions(state, "dtmf", session)
			if err != nil {
				return err
			}
			if nextState != "" {
				if err := e.executeActions(ctx, session, actions, speakFn); err != nil {
					return err
				}
				state, err = e.transition(ctx, session, sm, nextState, speakFn)
				if err != nil {
					return err
				}
			}

		case <-timeoutCh:
			if state.TimeoutNext != "" {
				var err error
				state, err = e.transition(ctx, session, sm, state.TimeoutNext, speakFn)
				if err != nil {
					return err
				}
			}
		}

		if state.Terminal {
			return nil
		}
	}
}

func (e *Engine) transition(ctx context.Context, session *Session, sm *StateMachine, target string, speakFn SpeakFunc) (State, error) {
	from := session.GetCurrentState()
	session.RecordTransition(from, target, fmt.Sprintf("%v", session.GetLastEvent()))

	if e.publisher != nil {
		_ = e.publisher.Emit(ctx, events.StateTransition, session.ID, &events.StateTransitionData{
			FromState:    from,
			ToState:      target,
			TriggerEvent: fmt.Sprintf("%v", session.GetLastEvent()),
			DialogName:   session.DialogName,
		})
	}

	state, ok := sm.GetState(target)
	if !ok {
		return State{}, fmt.Errorf("state %q not found", target)
	}

	if err := e.executeActions(ctx, session, state.OnEnter, speakFn); err != nil {
		return state, err
	}

	return state, nil
}

func (e *Engine) executeActions(ctx context.Context, session *Session, actions []Action, speakFn SpeakFunc) error {
	for _, action := range actions {
		if err := e.executeAction(ctx, session, action, speakFn); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) executeAction(ctx context.Context, session *Session, action Action, speakFn SpeakFunc) error {
	switch action.Type {
	case "play_tts":
		text, err := RenderParam(action.Params["text"], session)
		if err != nil {
			return fmt.Errorf("render TTS text: %w", err)
		}
		if speakFn != nil {
			return speakFn(text)
		}

	case "call_hook":
		if e.hooks == nil {
			return nil
		}
		cfg := hooks.HookConfig{
			URL:        action.Params["url"],
			AuthType:   action.Params["auth_type"],
			AuthSecret: action.Params["auth_secret"],
			TimeoutSec: 10,
		}
		req := hooks.HookRequest{
			SessionID: session.ID,
			State:     session.GetCurrentState(),
			Event:     fmt.Sprintf("%v", session.GetLastEvent()),
			Variables: session.CopyVariables(),
		}
		resp, err := e.hooks.Execute(ctx, cfg, req)
		if err != nil {
			return nil // hook errors are non-fatal by default
		}
		if resp != nil {
			for k, v := range resp.Variables {
				session.SetVariable(k, v)
			}
			session.SetLastResult(resp.Data)
		}

	case "set_variable":
		for k, v := range action.Params {
			rendered, err := RenderParam(v, session)
			if err != nil {
				return fmt.Errorf("render variable %q: %w", k, err)
			}
			session.SetVariable(k, rendered)
		}

	case "hangup":
		// Signal hangup by cancelling context (handled by caller).
		return nil

	case "play_audio":
		// Placeholder for audio file playback.
		return nil
	}

	if e.publisher != nil {
		_ = e.publisher.Emit(ctx, events.ActionExecuted, session.ID, &events.ActionExecutedData{
			ActionType: action.Type,
			Params:     action.Params,
		})
	}

	return nil
}
