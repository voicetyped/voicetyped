package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/urlvalidation"
)

// Executor calls external hook endpoints.
type Executor struct {
	httpClient     *http.Client
	publisher      *events.Publisher
	validateOpts   []urlvalidation.Option
}

// NewExecutor creates a new hook executor.
func NewExecutor(publisher *events.Publisher, validateOpts ...urlvalidation.Option) *Executor {
	return &Executor{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     60 * time.Second,
			},
		},
		publisher:    publisher,
		validateOpts: validateOpts,
	}
}

// Execute calls the hook endpoint and returns the response.
func (e *Executor) Execute(ctx context.Context, cfg HookConfig, req HookRequest) (*HookResponse, error) {
	if err := urlvalidation.ValidateWebhookURL(cfg.URL, e.validateOpts...); err != nil {
		return nil, fmt.Errorf("hook URL validation: %w", err)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal hook request: %w", err)
	}

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create hook request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	switch cfg.AuthType {
	case "bearer":
		httpReq.Header.Set("Authorization", "Bearer "+cfg.AuthSecret)
	case "hmac":
		sig := hmacSign(cfg.AuthSecret, body)
		httpReq.Header.Set("X-Hook-Signature", sig)
	}

	for k, v := range cfg.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		if e.publisher != nil {
			_ = e.publisher.Emit(ctx, events.HookError, req.SessionID, &events.HookErrorData{
				HookURL: cfg.URL,
				Error:   err.Error(),
			})
		}
		return nil, fmt.Errorf("hook request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	// Drain remainder for connection reuse.
	io.Copy(io.Discard, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read hook response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMsg := fmt.Sprintf("hook returned HTTP %d: %s", resp.StatusCode, string(respBody))
		if e.publisher != nil {
			_ = e.publisher.Emit(ctx, events.HookError, req.SessionID, &events.HookErrorData{
				HookURL: cfg.URL,
				Error:   errMsg,
			})
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	var hookResp HookResponse
	if err := json.Unmarshal(respBody, &hookResp); err != nil {
		return nil, fmt.Errorf("unmarshal hook response: %w", err)
	}

	if e.publisher != nil {
		_ = e.publisher.Emit(ctx, events.HookResult, req.SessionID, &events.HookResultData{
			HookURL:    cfg.URL,
			StatusCode: resp.StatusCode,
			Response:   hookResp.Data,
		})
	}

	return &hookResp, nil
}

func hmacSign(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return fmt.Sprintf("sha256=%x", mac.Sum(nil))
}
