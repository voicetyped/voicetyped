package dialog

import (
	"sync"
	"time"
)

// DefaultMaxHistory is the maximum number of state records before eviction.
const DefaultMaxHistory = 1000

// StateRecord records a state transition for audit purposes.
type StateRecord struct {
	FromState string    `json:"from_state"`
	ToState   string    `json:"to_state"`
	Trigger   string    `json:"trigger"`
	Timestamp time.Time `json:"timestamp"`
}

// Session holds per-call mutable state. All access is thread-safe.
type Session struct {
	mu         sync.RWMutex
	maxHistory int

	ID           string
	DialogName   string
	CurrentState string
	Variables    map[string]string
	History      []StateRecord
	StartTime    time.Time
	LastEvent    any
	LastResult   map[string]any
}

// NewSession creates a new call session.
func NewSession(id, dialogName, initialState string) *Session {
	return &Session{
		ID:           id,
		DialogName:   dialogName,
		CurrentState: initialState,
		Variables:    make(map[string]string),
		StartTime:    time.Now(),
		LastResult:   make(map[string]any),
		maxHistory:   DefaultMaxHistory,
	}
}

// RecordTransition adds a state transition to the audit history.
// Evicts oldest 10% of entries when the history cap is reached.
func (s *Session) RecordTransition(from, to, trigger string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.History) >= s.maxHistory {
		evict := s.maxHistory / 10
		if evict < 1 {
			evict = 1
		}
		s.History = s.History[evict:]
	}
	s.History = append(s.History, StateRecord{
		FromState: from,
		ToState:   to,
		Trigger:   trigger,
		Timestamp: time.Now(),
	})
	s.CurrentState = to
}

// SetVariable sets a session variable.
func (s *Session) SetVariable(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Variables[key] = value
}

// GetVariable returns a session variable.
func (s *Session) GetVariable(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Variables[key]
}

// GetCurrentState returns the current state name.
func (s *Session) GetCurrentState() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CurrentState
}

// SetCurrentState sets the current state name.
func (s *Session) SetCurrentState(st string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentState = st
}

// GetLastEvent returns the last event value.
func (s *Session) GetLastEvent() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastEvent
}

// SetLastEvent sets the last event value.
func (s *Session) SetLastEvent(e any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastEvent = e
}

// GetLastResult returns a copy of the last result map.
func (s *Session) GetLastResult() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]any, len(s.LastResult))
	for k, v := range s.LastResult {
		cp[k] = v
	}
	return cp
}

// SetLastResult sets the last result map.
func (s *Session) SetLastResult(r map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastResult = r
}

// CopyVariables returns a snapshot of all session variables.
func (s *Session) CopyVariables() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]string, len(s.Variables))
	for k, v := range s.Variables {
		cp[k] = v
	}
	return cp
}

// CopyHistory returns a snapshot of the state history.
func (s *Session) CopyHistory() []StateRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]StateRecord, len(s.History))
	copy(cp, s.History)
	return cp
}
