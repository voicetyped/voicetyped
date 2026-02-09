package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/pitabwire/frame/workerpool"

	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/urlvalidation"
)

const maxBreakers = 10000

// DelivererConfig holds delivery-related settings.
type DelivererConfig struct {
	MaxRetries        int
	TimeoutSec        int
	BackoffInitialSec int
	BackoffMaxSec     int
	CBFailThreshold   int
	CBResetTimeoutSec int
}

// Deliverer delivers webhook events to registered endpoints.
type Deliverer struct {
	repo         *Repository
	httpClient   *http.Client
	config       DelivererConfig
	pool         workerpool.WorkerPool
	validateOpts []urlvalidation.Option

	mu       sync.Mutex
	breakers map[string]*CircuitBreaker
}

// NewDeliverer creates a new webhook deliverer.
func NewDeliverer(repo *Repository, cfg DelivererConfig, pool workerpool.WorkerPool, validateOpts ...urlvalidation.Option) *Deliverer {
	return &Deliverer{
		repo: repo,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		config:       cfg,
		pool:         pool,
		validateOpts: validateOpts,
		breakers:     make(map[string]*CircuitBreaker),
	}
}

func (d *Deliverer) getOrCreateBreaker(webhookID string) *CircuitBreaker {
	d.mu.Lock()
	defer d.mu.Unlock()

	cb, ok := d.breakers[webhookID]
	if ok {
		return cb
	}

	// Evict oldest entry if at capacity.
	if len(d.breakers) >= maxBreakers {
		for k := range d.breakers {
			delete(d.breakers, k)
			break
		}
	}

	cb = NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    d.config.CBFailThreshold,
		ResetTimeout:        time.Duration(d.config.CBResetTimeoutSec) * time.Second,
		HalfOpenMaxAttempts: 1,
	})
	d.breakers[webhookID] = cb
	return cb
}

// Deliver attempts to POST an event envelope to a webhook endpoint.
func (d *Deliverer) Deliver(ctx context.Context, wh WebhookEndpoint, env events.Envelope) {
	d.deliverWithRetry(ctx, wh, env, 1)
}

func (d *Deliverer) deliverWithRetry(ctx context.Context, wh WebhookEndpoint, env events.Envelope, attempt int) {
	if err := urlvalidation.ValidateWebhookURL(wh.URL, d.validateOpts...); err != nil {
		slog.ErrorContext(ctx, "webhook URL failed SSRF validation",
			slog.String("webhook_id", wh.ID),
			slog.String("url", wh.URL),
			slog.String("error", err.Error()))
		return
	}

	cb := d.getOrCreateBreaker(wh.ID)

	if !cb.AllowRequest() {
		d.handleFailure(ctx, wh, env, attempt, "circuit open")
		return
	}

	body, err := json.Marshal(env)
	if err != nil {
		d.handleFailure(ctx, wh, env, attempt, fmt.Sprintf("marshal: %v", err))
		return
	}

	sig := Sign(wh.Secret, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		d.handleFailure(ctx, wh, env, attempt, fmt.Sprintf("create request: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SignatureHeader, sig)
	req.Header.Set("X-Voicetyped-Event", string(env.Type))
	req.Header.Set("X-Voicetyped-Delivery", env.ID)

	start := time.Now()
	resp, err := d.httpClient.Do(req)
	durationMs := time.Since(start).Milliseconds()

	da := &DeliveryAttempt{
		WebhookID:     wh.ID,
		EventID:       env.ID,
		EventType:     string(env.Type),
		RequestBody:   string(body),
		AttemptNumber: attempt,
		DurationMs:    durationMs,
	}

	if err != nil {
		cb.RecordFailure()
		da.Status = "failed"
		da.Error = err.Error()
		if err := d.repo.RecordDelivery(ctx, da); err != nil {
			slog.ErrorContext(ctx, "record delivery failed", slog.String("error", err.Error()))
		}
		d.handleFailure(ctx, wh, env, attempt, da.Error)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	// Drain remainder for connection reuse.
	io.Copy(io.Discard, resp.Body)

	da.ResponseCode = resp.StatusCode
	da.ResponseBody = string(respBody)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		cb.RecordSuccess()
		da.Status = "success"
		if err := d.repo.RecordDelivery(ctx, da); err != nil {
			slog.ErrorContext(ctx, "record delivery failed", slog.String("error", err.Error()))
		}
		return
	}

	cb.RecordFailure()
	da.Status = "failed"
	da.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	if err := d.repo.RecordDelivery(ctx, da); err != nil {
		slog.ErrorContext(ctx, "record delivery failed", slog.String("error", err.Error()))
	}
	d.handleFailure(ctx, wh, env, attempt, da.Error)
}

func (d *Deliverer) handleFailure(ctx context.Context, wh WebhookEndpoint, env events.Envelope, attempt int, errMsg string) {
	if attempt >= d.config.MaxRetries {
		payload, _ := json.Marshal(env)
		if err := d.repo.CreateDeadLetter(ctx, &DeadLetter{
			WebhookID:  wh.ID,
			EventID:    env.ID,
			EventType:  string(env.Type),
			Payload:    string(payload),
			LastError:  errMsg,
			Attempts:   attempt,
			Replayable: true,
		}); err != nil {
			slog.ErrorContext(ctx, "create dead letter failed", slog.String("error", err.Error()))
		}
		return
	}

	// Schedule retry with exponential backoff via worker pool.
	backoff := d.config.BackoffInitialSec * (1 << (attempt - 1))
	if backoff > d.config.BackoffMaxSec {
		backoff = d.config.BackoffMaxSec
	}

	retryFunc := func() {
		timer := time.NewTimer(time.Duration(backoff) * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			d.deliverWithRetry(ctx, wh, env, attempt+1)
		}
	}

	if d.pool != nil {
		if err := d.pool.Submit(ctx, retryFunc); err != nil {
			slog.WarnContext(ctx, "retry pool full, dropping retry",
				slog.String("webhook_id", wh.ID),
				slog.Int("attempt", attempt))
		}
	} else {
		time.AfterFunc(time.Duration(backoff)*time.Second, func() {
			d.deliverWithRetry(ctx, wh, env, attempt+1)
		})
	}
}
