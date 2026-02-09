package hooks

// HookConfig describes how to call an external hook endpoint.
type HookConfig struct {
	URL        string            `yaml:"url"        json:"url"`
	AuthType   string            `yaml:"auth_type"  json:"auth_type"`   // "bearer", "hmac", "none"
	AuthSecret string            `yaml:"auth_secret" json:"auth_secret"` // token or HMAC key
	TimeoutSec int               `yaml:"timeout_sec" json:"timeout_sec"`
	Headers    map[string]string `yaml:"headers"    json:"headers,omitempty"`
}

// HookRequest is the payload sent to a hook endpoint.
type HookRequest struct {
	SessionID string            `json:"session_id"`
	State     string            `json:"state"`
	Event     string            `json:"event"`
	Variables map[string]string `json:"variables"`
	Transcript string           `json:"transcript,omitempty"`
	Digit     string            `json:"digit,omitempty"`
}

// HookResponse is the expected response from a hook endpoint.
type HookResponse struct {
	Actions   []HookAction          `json:"actions,omitempty"`
	Variables map[string]string     `json:"variables,omitempty"`
	Data      map[string]any        `json:"data,omitempty"`
	NextState string                `json:"next_state,omitempty"`
}

// HookAction is a directive returned by a hook.
type HookAction struct {
	Type   string            `json:"type"`
	Params map[string]string `json:"params,omitempty"`
}
