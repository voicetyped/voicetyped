package webhook

import (
	"database/sql"
	"encoding/json"

	"github.com/pitabwire/frame/data"

	"github.com/voicetyped/voicetyped/pkg/events"
)

// WebhookEndpoint represents a registered webhook subscription.
type WebhookEndpoint struct {
	data.BaseModel

	Name         string          `gorm:"type:varchar(255);not null"  json:"name"`
	URL          string          `gorm:"type:varchar(2048);not null" json:"url"`
	Secret       string          `gorm:"type:varchar(512);not null"  json:"-"`
	EventTypes   EventTypesJSON  `gorm:"type:jsonb;default:'[]'"     json:"event_types"`
	IsActive     bool            `gorm:"default:true"                json:"is_active"`
	Description  string          `gorm:"type:text"                   json:"description,omitempty"`
	FailureCount int             `gorm:"default:0"                   json:"failure_count"`
	LastFailureAt sql.NullTime   `json:"last_failure_at,omitempty"`
	CircuitState string          `gorm:"type:varchar(20);default:'closed'" json:"circuit_state"`
	MaxRPS       int             `gorm:"default:10"                  json:"max_rps"`
}

func (WebhookEndpoint) TableName() string { return "webhook_endpoints" }

// EventTypesJSON is a custom GORM type for JSONB storage of event types.
type EventTypesJSON []events.EventType

func (e EventTypesJSON) Value() (interface{}, error) {
	return json.Marshal(e)
}

func (e *EventTypesJSON) Scan(src interface{}) error {
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, e)
	case string:
		return json.Unmarshal([]byte(v), e)
	default:
		*e = EventTypesJSON{}
		return nil
	}
}

// Contains checks whether the list includes the given event type.
func (e EventTypesJSON) Contains(et events.EventType) bool {
	for _, t := range e {
		if t == et {
			return true
		}
	}
	return false
}

// DeliveryAttempt records one attempt to deliver an event to a webhook.
type DeliveryAttempt struct {
	data.BaseModel

	WebhookID     string       `gorm:"type:varchar(50);not null;index:idx_da_webhook" json:"webhook_id"`
	EventID       string       `gorm:"type:varchar(50);not null"                       json:"event_id"`
	EventType     string       `gorm:"type:varchar(100);not null"                      json:"event_type"`
	RequestBody   string       `gorm:"type:text"                                       json:"-"`
	ResponseCode  int          `gorm:"default:0"                                       json:"response_code"`
	ResponseBody  string       `gorm:"type:text"                                       json:"-"`
	AttemptNumber int          `gorm:"default:1"                                       json:"attempt_number"`
	Status        string       `gorm:"type:varchar(20);not null;index:idx_da_status"   json:"status"`
	Error         string       `gorm:"type:text"                                       json:"error,omitempty"`
	DurationMs    int64        `gorm:"default:0"                                       json:"duration_ms"`
	NextRetryAt   sql.NullTime `json:"next_retry_at,omitempty"`
}

func (DeliveryAttempt) TableName() string { return "delivery_attempts" }

// DeadLetter holds events that exhausted all delivery retries.
type DeadLetter struct {
	data.BaseModel

	WebhookID  string `gorm:"type:varchar(50);not null;index:idx_dl_webhook" json:"webhook_id"`
	EventID    string `gorm:"type:varchar(50);not null"                       json:"event_id"`
	EventType  string `gorm:"type:varchar(100);not null"                      json:"event_type"`
	Payload    string `gorm:"type:text;not null"                              json:"payload"`
	LastError  string `gorm:"type:text"                                       json:"last_error"`
	Attempts   int    `gorm:"default:0"                                       json:"attempts"`
	Replayable bool   `gorm:"default:true"                                    json:"replayable"`
}

func (DeadLetter) TableName() string { return "dead_letters" }
