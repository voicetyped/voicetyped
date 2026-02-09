package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/urlvalidation"
	"github.com/voicetyped/voicetyped/pkg/webhook"
)

const maxRequestBodySize = 1 << 20 // 1 MiB

// Handler provides REST endpoints for webhook management.
type Handler struct {
	repo      *webhook.Repository
	publisher *events.Publisher
}

// NewHandler creates a new webhook API handler.
func NewHandler(repo *webhook.Repository, publisher *events.Publisher) *Handler {
	return &Handler{repo: repo, publisher: publisher}
}

// RegisterRoutes registers all webhook API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/webhooks", h.Create)
	mux.HandleFunc("GET /api/v1/webhooks", h.List)
	mux.HandleFunc("GET /api/v1/webhooks/{id}", h.Get)
	mux.HandleFunc("PUT /api/v1/webhooks/{id}", h.Update)
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", h.Delete)
	mux.HandleFunc("POST /api/v1/webhooks/{id}/rotate-secret", h.RotateSecret)
	mux.HandleFunc("GET /api/v1/webhooks/{id}/deliveries", h.ListDeliveries)
	mux.HandleFunc("GET /api/v1/webhooks/{id}/dead-letters", h.ListDeadLetters)
	mux.HandleFunc("POST /api/v1/webhooks/{id}/dead-letters/{dlid}/replay", h.ReplayDeadLetter)
	mux.HandleFunc("POST /api/v1/webhooks/{id}/test", h.Test)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

func toWebhookResponse(wh *webhook.WebhookEndpoint, includeSecret bool) WebhookResponse {
	resp := WebhookResponse{
		ID:           wh.ID,
		Name:         wh.Name,
		URL:          wh.URL,
		EventTypes:   []events.EventType(wh.EventTypes),
		IsActive:     wh.IsActive,
		Description:  wh.Description,
		FailureCount: wh.FailureCount,
		CircuitState: wh.CircuitState,
		MaxRPS:       wh.MaxRPS,
		CreatedAt:    wh.CreatedAt.Format(time.RFC3339),
		ModifiedAt:   wh.ModifiedAt.Format(time.RFC3339),
	}
	if includeSecret {
		resp.Secret = wh.Secret
	}
	return resp
}

// Create handles POST /api/v1/webhooks
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var req CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.URL == "" {
		writeError(w, http.StatusBadRequest, "name and url are required")
		return
	}

	if err := urlvalidation.ValidateWebhookURL(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook URL: "+err.Error())
		return
	}

	secret, err := webhook.GenerateSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}

	maxRPS := req.MaxRPS
	if maxRPS <= 0 {
		maxRPS = 10
	}

	wh := &webhook.WebhookEndpoint{
		Name:        req.Name,
		URL:         req.URL,
		Secret:      secret,
		EventTypes:  webhook.EventTypesJSON(req.EventTypes),
		IsActive:    true,
		Description: req.Description,
		MaxRPS:      maxRPS,
	}

	if err := h.repo.CreateEndpoint(r.Context(), wh); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create webhook")
		return
	}

	writeJSON(w, http.StatusCreated, toWebhookResponse(wh, true))
}

// List handles GET /api/v1/webhooks
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	endpoints, err := h.repo.ListAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list webhooks")
		return
	}

	resp := make([]WebhookResponse, 0, len(endpoints))
	for i := range endpoints {
		resp = append(resp, toWebhookResponse(&endpoints[i], false))
	}
	writeJSON(w, http.StatusOK, resp)
}

// Get handles GET /api/v1/webhooks/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wh, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	writeJSON(w, http.StatusOK, toWebhookResponse(wh, false))
}

// Update handles PUT /api/v1/webhooks/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	id := r.PathValue("id")
	wh, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	var req UpdateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		wh.Name = *req.Name
	}
	if req.URL != nil {
		if err := urlvalidation.ValidateWebhookURL(*req.URL); err != nil {
			writeError(w, http.StatusBadRequest, "invalid webhook URL: "+err.Error())
			return
		}
		wh.URL = *req.URL
	}
	if req.EventTypes != nil {
		wh.EventTypes = webhook.EventTypesJSON(*req.EventTypes)
	}
	if req.IsActive != nil {
		wh.IsActive = *req.IsActive
	}
	if req.Description != nil {
		wh.Description = *req.Description
	}
	if req.MaxRPS != nil {
		wh.MaxRPS = *req.MaxRPS
	}

	if err := h.repo.Update(r.Context(), wh); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update webhook")
		return
	}

	writeJSON(w, http.StatusOK, toWebhookResponse(wh, false))
}

// Delete handles DELETE /api/v1/webhooks/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.repo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete webhook")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RotateSecret handles POST /api/v1/webhooks/{id}/rotate-secret
func (h *Handler) RotateSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wh, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	secret, err := webhook.GenerateSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}

	wh.Secret = secret
	if err := h.repo.Update(r.Context(), wh); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update secret")
		return
	}

	writeJSON(w, http.StatusOK, toWebhookResponse(wh, true))
}

// ListDeliveries handles GET /api/v1/webhooks/{id}/deliveries
func (h *Handler) ListDeliveries(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	attempts, err := h.repo.ListDeliveries(r.Context(), id, 50, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}

	resp := make([]DeliveryResponse, 0, len(attempts))
	for _, a := range attempts {
		resp = append(resp, DeliveryResponse{
			ID:            a.ID,
			EventID:       a.EventID,
			EventType:     a.EventType,
			ResponseCode:  a.ResponseCode,
			AttemptNumber: a.AttemptNumber,
			Status:        a.Status,
			Error:         a.Error,
			DurationMs:    a.DurationMs,
			CreatedAt:     a.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListDeadLetters handles GET /api/v1/webhooks/{id}/dead-letters
func (h *Handler) ListDeadLetters(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	letters, err := h.repo.ListDeadLetters(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dead letters")
		return
	}

	resp := make([]DeadLetterResponse, 0, len(letters))
	for _, dl := range letters {
		resp = append(resp, DeadLetterResponse{
			ID:        dl.ID,
			EventID:   dl.EventID,
			EventType: dl.EventType,
			LastError: dl.LastError,
			Attempts:  dl.Attempts,
			CreatedAt: dl.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ReplayDeadLetter handles POST /api/v1/webhooks/{id}/dead-letters/{dlid}/replay
func (h *Handler) ReplayDeadLetter(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	id := r.PathValue("id")
	dlid := r.PathValue("dlid")

	letters, err := h.repo.ListDeadLetters(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dead letters")
		return
	}

	var found *webhook.DeadLetter
	for i := range letters {
		if letters[i].ID == dlid {
			found = &letters[i]
			break
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "dead letter not found")
		return
	}

	// Re-publish the envelope to the event bus.
	var env events.Envelope
	if err := json.Unmarshal([]byte(found.Payload), &env); err != nil {
		writeError(w, http.StatusInternalServerError, "corrupt dead letter payload")
		return
	}

	if err := h.publisher.Emit(r.Context(), env.Type, env.SessionID, json.RawMessage(env.Data)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to re-publish event")
		return
	}

	if err := h.repo.MarkDeadLetterReplayed(r.Context(), dlid); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark dead letter replayed")
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// Test handles POST /api/v1/webhooks/{id}/test
func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	id := r.PathValue("id")

	// Verify webhook exists.
	_, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	testData := events.WebhookTestData{
		WebhookID: id,
		Message:   "This is a test webhook delivery from voicetyped",
	}

	if err := h.publisher.Emit(r.Context(), events.WebhookTest, "", testData); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to publish test event")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "test event published"})
}

