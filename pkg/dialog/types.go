package dialog

// Dialog is a YAML-mappable dialog definition.
type Dialog struct {
	Name         string            `yaml:"name"          json:"name"`
	Version      string            `yaml:"version"       json:"version"`
	Description  string            `yaml:"description"   json:"description"`
	Variables    map[string]string `yaml:"variables"     json:"variables"`
	InitialState string           `yaml:"initial_state" json:"initial_state"`
	States       map[string]State  `yaml:"states"        json:"states"`
}

// State represents a single state in the dialog FSM.
type State struct {
	OnEnter     []Action     `yaml:"on_enter"      json:"on_enter,omitempty"`
	Transitions []Transition `yaml:"transitions"   json:"transitions,omitempty"`
	Timeout     string       `yaml:"timeout"       json:"timeout,omitempty"`
	TimeoutNext string       `yaml:"timeout_next"  json:"timeout_next,omitempty"`
	Terminal    bool         `yaml:"terminal"      json:"terminal,omitempty"`
}

// Transition defines a condition under which the FSM moves to a new state.
type Transition struct {
	Event     string   `yaml:"event"     json:"event"`
	Condition string   `yaml:"condition" json:"condition,omitempty"`
	Target    string   `yaml:"target"    json:"target"`
	Actions   []Action `yaml:"actions"   json:"actions,omitempty"`
}

// Action is an operation executed during a state transition or on state entry.
type Action struct {
	Type   string            `yaml:"type"   json:"type"`
	Params map[string]string `yaml:"params" json:"params,omitempty"`
}
