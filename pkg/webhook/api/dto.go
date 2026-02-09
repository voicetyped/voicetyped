package api

import "github.com/voicetyped/voicetyped/pkg/events"

// CreateWebhookRequest is the request body for creating a webhook.
type CreateWebhookRequest struct {
	Name        string             `json:"name"`
	URL         string             `json:"url"`
	EventTypes  []events.EventType `json:"event_types"`
	Description string             `json:"description,omitempty"`
	MaxRPS      int                `json:"max_rps,omitempty"`
}

// UpdateWebhookRequest is the request body for updating a webhook.
type UpdateWebhookRequest struct {
	Name        *string             `json:"name,omitempty"`
	URL         *string             `json:"url,omitempty"`
	EventTypes  *[]events.EventType `json:"event_types,omitempty"`
	IsActive    *bool               `json:"is_active,omitempty"`
	Description *string             `json:"description,omitempty"`
	MaxRPS      *int                `json:"max_rps,omitempty"`
}

// WebhookResponse is the API response for a webhook endpoint.
type WebhookResponse struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	URL          string             `json:"url"`
	Secret       string             `json:"secret,omitempty"` // only on create
	EventTypes   []events.EventType `json:"event_types"`
	IsActive     bool               `json:"is_active"`
	Description  string             `json:"description,omitempty"`
	FailureCount int                `json:"failure_count"`
	CircuitState string             `json:"circuit_state"`
	MaxRPS       int                `json:"max_rps"`
	CreatedAt    string             `json:"created_at"`
	ModifiedAt   string             `json:"modified_at"`
}

// DeliveryResponse is the API response for a delivery attempt.
type DeliveryResponse struct {
	ID            string `json:"id"`
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	ResponseCode  int    `json:"response_code"`
	AttemptNumber int    `json:"attempt_number"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	DurationMs    int64  `json:"duration_ms"`
	CreatedAt     string `json:"created_at"`
}

// DeadLetterResponse is the API response for a dead letter.
type DeadLetterResponse struct {
	ID        string `json:"id"`
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	LastError string `json:"last_error"`
	Attempts  int    `json:"attempts"`
	CreatedAt string `json:"created_at"`
}

// ErrorResponse is a standard error response.
type ErrorResponse struct {
	Error string `json:"error"`
}
