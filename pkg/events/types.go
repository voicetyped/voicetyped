package events

import (
	"encoding/json"
	"time"
)

// EventType identifies the kind of event flowing through the system.
type EventType string

const (
	CallStarted     EventType = "call.started"
	CallTerminated  EventType = "call.terminated"
	SpeechPartial   EventType = "speech.partial"
	SpeechFinal     EventType = "speech.final"
	DTMFReceived    EventType = "dtmf.received"
	StateTransition EventType = "state.transition"
	ActionExecuted  EventType = "action.executed"
	HookResult      EventType = "hook.result"
	HookError       EventType = "hook.error"
	TTSStarted      EventType = "tts.started"
	TTSCompleted    EventType = "tts.completed"
	SystemError      EventType = "error"
	WebhookTest      EventType = "webhook.test"
	TrackPublished   EventType = "track.published"
	TrackUnpublished EventType = "track.unpublished"
	SpeakerChanged   EventType = "speaker.changed"
)

// Envelope is the standard event wrapper published to the event bus.
type Envelope struct {
	ID        string            `json:"id"`
	Type      EventType         `json:"type"`
	Source    string            `json:"source"`
	SessionID string           `json:"session_id"`
	Timestamp time.Time         `json:"timestamp"`
	Data      json.RawMessage   `json:"data"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// CallStartedData is the payload for call.started events.
type CallStartedData struct {
	CallerID     string `json:"caller_id"`
	CalledNumber string `json:"called_number"`
	Protocol     string `json:"protocol"` // "sip" or "webrtc"
}

// CallTerminatedData is the payload for call.terminated events.
type CallTerminatedData struct {
	Reason     string `json:"reason"`
	DurationMs int64  `json:"duration_ms"`
}

// Segment represents a timed segment of transcribed speech.
type Segment struct {
	Text       string  `json:"text"`
	StartMs    int     `json:"start_ms"`
	EndMs      int     `json:"end_ms"`
	Confidence float32 `json:"confidence"`
}

// SpeechPartialData is the payload for speech.partial events.
type SpeechPartialData struct {
	Transcript string `json:"transcript"`
}

// SpeechFinalData is the payload for speech.final events.
type SpeechFinalData struct {
	Transcript string    `json:"transcript"`
	Confidence float32   `json:"confidence"`
	Language   string    `json:"language"`
	Segments   []Segment `json:"segments,omitempty"`
}

// DTMFData is the payload for dtmf.received events.
type DTMFData struct {
	Digit      rune `json:"digit"`
	DurationMs int  `json:"duration_ms"`
}

// StateTransitionData is the payload for state.transition events.
type StateTransitionData struct {
	FromState    string `json:"from_state"`
	ToState      string `json:"to_state"`
	TriggerEvent string `json:"trigger_event"`
	DialogName   string `json:"dialog_name"`
}

// ActionExecutedData is the payload for action.executed events.
type ActionExecutedData struct {
	ActionType string            `json:"action_type"`
	Params     map[string]string `json:"params,omitempty"`
}

// HookResultData is the payload for hook.result events.
type HookResultData struct {
	HookURL    string                 `json:"hook_url"`
	StatusCode int                    `json:"status_code"`
	Response   map[string]interface{} `json:"response,omitempty"`
}

// HookErrorData is the payload for hook.error events.
type HookErrorData struct {
	HookURL string `json:"hook_url"`
	Error   string `json:"error"`
}

// TTSEventData is the payload for tts.started and tts.completed events.
type TTSEventData struct {
	Text  string `json:"text"`
	Voice string `json:"voice,omitempty"`
}

// WebhookTestData is the payload for webhook.test events.
type WebhookTestData struct {
	WebhookID string `json:"webhook_id"`
	Message   string `json:"message"`
}
