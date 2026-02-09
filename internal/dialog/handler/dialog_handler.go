package handler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/pitabwire/frame/workerpool"

	dialogv1 "github.com/voicetyped/voicetyped/gen/voicetyped/dialog/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/dialog/v1/dialogv1connect"
	"github.com/voicetyped/voicetyped/pkg/dialog"
	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/hooks"
)

const (
	sessionTTL      = 30 * time.Minute
	reaperInterval  = 1 * time.Minute
	endDialogWait   = 5 * time.Second
)

// Ensure we implement the interface.
var _ dialogv1connect.DialogServiceHandler = (*DialogHandler)(nil)

type actionResult struct {
	actions  []dialog.Action
	newState string
	terminal bool
	err      error
}

type activeSession struct {
	session  *dialog.Session
	sm       *dialog.StateMachine
	speechCh chan dialog.ASRResult
	dtmfCh   chan rune
	resultCh chan actionResult
	cancel   context.CancelFunc
	done     chan struct{} // closed when runDialogLoop exits
}

// SessionStore holds active dialog sessions.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*activeSession
}

// DialogHandler implements dialogv1connect.DialogServiceHandler.
type DialogHandler struct {
	loader    *dialog.Loader
	hookExec  *hooks.Executor
	publisher *events.Publisher
	store     SessionStore
	pool      workerpool.WorkerPool
}

// NewDialogHandler creates a new dialog service handler.
func NewDialogHandler(loader *dialog.Loader, hookExec *hooks.Executor, pub *events.Publisher, pool workerpool.WorkerPool) *DialogHandler {
	return &DialogHandler{
		loader:    loader,
		hookExec:  hookExec,
		publisher: pub,
		pool:      pool,
		store: SessionStore{
			sessions: make(map[string]*activeSession),
		},
	}
}

// StartReaper begins the background session TTL reaper.
func (h *DialogHandler) StartReaper(ctx context.Context) {
	reap := func() {
		ticker := time.NewTicker(reaperInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.reapStaleSessions()
			}
		}
	}
	if h.pool != nil {
		_ = h.pool.Submit(ctx, reap)
	} else {
		go reap()
	}
}

func (h *DialogHandler) reapStaleSessions() {
	now := time.Now()
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	for id, as := range h.store.sessions {
		if now.Sub(as.session.StartTime) > sessionTTL {
			slog.Warn("reaping stale dialog session", slog.String("session_id", id))
			as.cancel()
			delete(h.store.sessions, id)
		}
	}
}

func (h *DialogHandler) StartDialog(ctx context.Context, req *connect.Request[dialogv1.StartDialogRequest]) (*connect.Response[dialogv1.StartDialogResponse], error) {
	dialogName := req.Msg.DialogName
	sm, ok := h.loader.Get(dialogName)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("dialog %q not found", dialogName))
	}

	initialState := req.Msg.InitialState
	if initialState == "" {
		initialState = sm.InitialState()
	}

	session := dialog.NewSession(req.Msg.SessionId, dialogName, initialState)
	for k, v := range req.Msg.Variables {
		session.SetVariable(k, v)
	}

	// Get on_enter actions for the initial state.
	state, ok := sm.GetState(initialState)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("state %q not found in dialog %q", initialState, dialogName))
	}

	// Create channels for the background dialog loop.
	speechCh := make(chan dialog.ASRResult, 8)
	dtmfCh := make(chan rune, 16)
	resultCh := make(chan actionResult, 8)

	// Session context is independent of RPC contexts since the dialog session
	// outlives individual RPC calls. Cancellation happens via EndDialog.
	sessionCtx, cancel := context.WithCancel(context.Background())

	as := &activeSession{
		session:  session,
		sm:       sm,
		speechCh: speechCh,
		dtmfCh:   dtmfCh,
		resultCh: resultCh,
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	h.store.mu.Lock()
	h.store.sessions[session.ID] = as
	h.store.mu.Unlock()

	// Start background dialog engine loop.
	loopFunc := func() { h.runDialogLoop(sessionCtx, as) }
	if h.pool != nil {
		_ = h.pool.Submit(sessionCtx, loopFunc)
	} else {
		go loopFunc()
	}

	// Collect on_enter actions for the initial state.
	actions := actionsToDirectives(state.OnEnter)

	return connect.NewResponse(&dialogv1.StartDialogResponse{
		SessionId:    session.ID,
		CurrentState: initialState,
		Actions:      actions,
	}), nil
}

func (h *DialogHandler) SendEvent(_ context.Context, req *connect.Request[dialogv1.SendEventRequest]) (*connect.Response[dialogv1.SendEventResponse], error) {
	h.store.mu.RLock()
	as, ok := h.store.sessions[req.Msg.SessionId]
	h.store.mu.RUnlock()

	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.SessionId))
	}

	previousState := as.session.GetCurrentState()

	switch req.Msg.EventType {
	case "speech":
		// Use select to avoid blocking if the channel is full.
		select {
		case as.speechCh <- dialog.ASRResult{
			Text:    req.Msg.EventData,
			IsFinal: true,
		}:
		case <-time.After(5 * time.Second):
			return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("dialog engine busy, cannot accept speech event"))
		}
	case "dtmf":
		if len(req.Msg.EventData) > 0 {
			select {
			case as.dtmfCh <- rune(req.Msg.EventData[0]):
			case <-time.After(5 * time.Second):
				return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("dialog engine busy, cannot accept dtmf event"))
			}
		}
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unsupported event type %q", req.Msg.EventType))
	}

	// Wait for the dialog engine to process and return results.
	select {
	case result := <-as.resultCh:
		if result.err != nil {
			return nil, connect.NewError(connect.CodeInternal, result.err)
		}

		return connect.NewResponse(&dialogv1.SendEventResponse{
			PreviousState: previousState,
			CurrentState:  result.newState,
			Terminal:      result.terminal,
			Actions:       actionsToDirectives(result.actions),
		}), nil
	case <-time.After(10 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf("dialog engine timeout"))
	}
}

func (h *DialogHandler) GetSession(_ context.Context, req *connect.Request[dialogv1.GetSessionRequest]) (*connect.Response[dialogv1.GetSessionResponse], error) {
	h.store.mu.RLock()
	as, ok := h.store.sessions[req.Msg.SessionId]
	h.store.mu.RUnlock()

	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.SessionId))
	}

	// Use thread-safe session accessors.
	sessionID := as.session.ID
	dialogName := as.session.DialogName
	currentState := as.session.GetCurrentState()
	variables := as.session.CopyVariables()
	history := as.session.CopyHistory()

	historyProto := make([]*dialogv1.StateRecord, 0, len(history))
	for _, r := range history {
		historyProto = append(historyProto, &dialogv1.StateRecord{
			FromState: r.FromState,
			ToState:   r.ToState,
			Trigger:   r.Trigger,
			Timestamp: r.Timestamp.Format(time.RFC3339),
		})
	}

	return connect.NewResponse(&dialogv1.GetSessionResponse{
		SessionId:    sessionID,
		DialogName:   dialogName,
		CurrentState: currentState,
		Variables:    variables,
		History:      historyProto,
	}), nil
}

func (h *DialogHandler) EndDialog(_ context.Context, req *connect.Request[dialogv1.EndDialogRequest]) (*connect.Response[dialogv1.EndDialogResponse], error) {
	h.store.mu.Lock()
	as, ok := h.store.sessions[req.Msg.SessionId]
	if ok {
		delete(h.store.sessions, req.Msg.SessionId)
	}
	h.store.mu.Unlock()

	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q not found", req.Msg.SessionId))
	}

	// Cancel and wait for the dialog loop to exit before closing channels.
	as.cancel()
	select {
	case <-as.done:
	case <-time.After(endDialogWait):
		slog.Warn("dialog loop did not exit in time", slog.String("session_id", req.Msg.SessionId))
	}
	close(as.speechCh)
	close(as.dtmfCh)

	return connect.NewResponse(&dialogv1.EndDialogResponse{}), nil
}

func (h *DialogHandler) ListDialogs(_ context.Context, _ *connect.Request[dialogv1.ListDialogsRequest]) (*connect.Response[dialogv1.ListDialogsResponse], error) {
	all := h.loader.All()

	dialogs := make([]*dialogv1.DialogInfo, 0, len(all))
	for _, sm := range all {
		d := sm.Dialog()
		states := make([]string, 0, len(d.States))
		for name := range d.States {
			states = append(states, name)
		}

		dialogs = append(dialogs, &dialogv1.DialogInfo{
			Name:         d.Name,
			Version:      d.Version,
			Description:  d.Description,
			InitialState: d.InitialState,
			States:       states,
		})
	}

	return connect.NewResponse(&dialogv1.ListDialogsResponse{Dialogs: dialogs}), nil
}

// runDialogLoop runs a simplified dialog event loop in the background.
func (h *DialogHandler) runDialogLoop(ctx context.Context, as *activeSession) {
	defer close(as.done)

	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		currentState := as.session.GetCurrentState()

		state, ok := as.sm.GetState(currentState)
		if !ok || state.Terminal {
			as.resultCh <- actionResult{terminal: true, newState: currentState}
			return
		}

		// Set up timeout with proper timer management.
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
			return

		case result, ok := <-as.speechCh:
			if !ok {
				return
			}
			as.session.SetLastEvent(result.Text)
			nextState, actions, err := as.sm.EvaluateTransitions(state, "speech", as.session)
			if err != nil {
				as.resultCh <- actionResult{err: err}
				return
			}
			if nextState != "" {
				as.session.RecordTransition(as.session.GetCurrentState(), nextState, result.Text)

				newState, stateOk := as.sm.GetState(nextState)
				if !stateOk {
					as.resultCh <- actionResult{err: fmt.Errorf("state %q not found", nextState)}
					return
				}
				allActions := append(actions, newState.OnEnter...)
				as.resultCh <- actionResult{
					actions:  allActions,
					newState: nextState,
					terminal: newState.Terminal,
				}
			} else {
				curState := as.session.GetCurrentState()
				as.resultCh <- actionResult{newState: curState}
			}

		case digit, ok := <-as.dtmfCh:
			if !ok {
				return
			}
			as.session.SetLastEvent(digit)
			nextState, actions, err := as.sm.EvaluateTransitions(state, "dtmf", as.session)
			if err != nil {
				as.resultCh <- actionResult{err: err}
				return
			}
			if nextState != "" {
				as.session.RecordTransition(as.session.GetCurrentState(), nextState, string(digit))

				newState, stateOk := as.sm.GetState(nextState)
				if !stateOk {
					as.resultCh <- actionResult{err: fmt.Errorf("state %q not found", nextState)}
					return
				}
				allActions := append(actions, newState.OnEnter...)
				as.resultCh <- actionResult{
					actions:  allActions,
					newState: nextState,
					terminal: newState.Terminal,
				}
			} else {
				curState := as.session.GetCurrentState()
				as.resultCh <- actionResult{newState: curState}
			}

		case <-timeoutCh:
			if state.TimeoutNext != "" {
				as.session.RecordTransition(as.session.GetCurrentState(), state.TimeoutNext, "timeout")

				newState, stateOk := as.sm.GetState(state.TimeoutNext)
				if !stateOk {
					continue
				}
				as.resultCh <- actionResult{
					actions:  newState.OnEnter,
					newState: state.TimeoutNext,
					terminal: newState.Terminal,
				}
			}
		}
	}
}

func actionsToDirectives(actions []dialog.Action) []*dialogv1.ActionDirective {
	directives := make([]*dialogv1.ActionDirective, 0, len(actions))
	for _, a := range actions {
		directives = append(directives, &dialogv1.ActionDirective{
			Type:   a.Type,
			Params: a.Params,
		})
	}
	return directives
}
