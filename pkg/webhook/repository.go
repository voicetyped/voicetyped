package webhook

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/pitabwire/frame/datastore/pool"

	"github.com/voicetyped/voicetyped/pkg/events"
)

// Repository provides CRUD operations for webhook-related models.
type Repository struct {
	pool pool.Pool
}

// NewRepository creates a new webhook repository.
func NewRepository(pool pool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) db(ctx context.Context, readOnly bool) *gorm.DB {
	return r.pool.DB(ctx, readOnly)
}

// CreateEndpoint persists a new webhook endpoint.
func (r *Repository) CreateEndpoint(ctx context.Context, wh *WebhookEndpoint) error {
	return r.db(ctx, false).Create(wh).Error
}

// GetByID returns a webhook endpoint by ID.
func (r *Repository) GetByID(ctx context.Context, id string) (*WebhookEndpoint, error) {
	var wh WebhookEndpoint
	err := r.db(ctx, true).Where("id = ?", id).First(&wh).Error
	if err != nil {
		return nil, err
	}
	return &wh, nil
}

// ListActive returns all active webhook endpoints.
func (r *Repository) ListActive(ctx context.Context) ([]WebhookEndpoint, error) {
	var endpoints []WebhookEndpoint
	err := r.db(ctx, true).Where("is_active = ?", true).Find(&endpoints).Error
	return endpoints, err
}

// ListByEventType returns active webhooks subscribed to the given event type.
func (r *Repository) ListByEventType(ctx context.Context, et events.EventType) ([]WebhookEndpoint, error) {
	var endpoints []WebhookEndpoint
	// Use JSONB containment operator for efficient lookup.
	err := r.db(ctx, true).
		Where("is_active = ? AND event_types @> ?", true, fmt.Sprintf(`[%q]`, et)).
		Find(&endpoints).Error
	return endpoints, err
}

// ListAll returns all webhook endpoints (for admin listing).
func (r *Repository) ListAll(ctx context.Context) ([]WebhookEndpoint, error) {
	var endpoints []WebhookEndpoint
	err := r.db(ctx, true).Find(&endpoints).Error
	return endpoints, err
}

// Update persists changes to a webhook endpoint.
func (r *Repository) Update(ctx context.Context, wh *WebhookEndpoint) error {
	return r.db(ctx, false).Save(wh).Error
}

// Delete soft-deletes a webhook endpoint.
func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.db(ctx, false).Where("id = ?", id).Delete(&WebhookEndpoint{}).Error
}

// RecordDelivery persists a delivery attempt.
func (r *Repository) RecordDelivery(ctx context.Context, da *DeliveryAttempt) error {
	return r.db(ctx, false).Create(da).Error
}

// ListDeliveries returns delivery attempts for a webhook, newest first.
func (r *Repository) ListDeliveries(ctx context.Context, webhookID string, limit, offset int) ([]DeliveryAttempt, error) {
	var attempts []DeliveryAttempt
	q := r.db(ctx, true).
		Where("webhook_id = ?", webhookID).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	err := q.Find(&attempts).Error
	return attempts, err
}

// GetDeadLetterByID returns a single dead letter by its ID.
func (r *Repository) GetDeadLetterByID(ctx context.Context, id string) (*DeadLetter, error) {
	var dl DeadLetter
	err := r.db(ctx, true).Where("id = ?", id).First(&dl).Error
	if err != nil {
		return nil, err
	}
	return &dl, nil
}

// CreateDeadLetter persists a dead-lettered event.
func (r *Repository) CreateDeadLetter(ctx context.Context, dl *DeadLetter) error {
	return r.db(ctx, false).Create(dl).Error
}

// ListDeadLetters returns dead letters for a webhook.
func (r *Repository) ListDeadLetters(ctx context.Context, webhookID string) ([]DeadLetter, error) {
	var letters []DeadLetter
	err := r.db(ctx, true).
		Where("webhook_id = ? AND replayable = ?", webhookID, true).
		Order("created_at DESC").
		Find(&letters).Error
	return letters, err
}

// MarkDeadLetterReplayed marks a dead letter as no longer replayable.
func (r *Repository) MarkDeadLetterReplayed(ctx context.Context, id string) error {
	return r.db(ctx, false).
		Model(&DeadLetter{}).
		Where("id = ?", id).
		Update("replayable", false).Error
}
