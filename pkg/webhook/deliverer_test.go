package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/urlvalidation"
)

func testEnvelope() events.Envelope {
	data, _ := json.Marshal(events.WebhookTestData{
		WebhookID: "wh-1",
		Message:   "ping",
	})
	return events.Envelope{
		ID:        "evt-1",
		Type:      events.WebhookTest,
		Source:    "test",
		SessionID: "sess-1",
		Timestamp: time.Now().UTC(),
		Data:      data,
	}
}

func TestDelivererSuccess(t *testing.T) {
	var received atomic.Bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing Content-Type header")
		}
		if r.Header.Get(SignatureHeader) == "" {
			t.Error("missing signature header")
		}
		if r.Header.Get("X-Voicetyped-Event") != string(events.WebhookTest) {
			t.Error("wrong event header")
		}
		received.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// We can't easily create a real Repository without a DB, so we'll just
	// test that the deliverer sends the right HTTP request. The repo calls
	// will error but that's acceptable for this unit test.
	d := NewDeliverer(nil, DelivererConfig{
		MaxRetries:        1,
		TimeoutSec:        5,
		BackoffInitialSec: 1,
		BackoffMaxSec:     1,
		CBFailThreshold:   5,
		CBResetTimeoutSec: 60,
	}, nil, urlvalidation.AllowPrivateIPs())
	// Override repo to nil-safe deliverer (delivery recording will be skipped)
	d.repo = nil

	wh := WebhookEndpoint{
		URL:    ts.URL,
		Secret: "test-secret",
	}
	wh.ID = "wh-1"

	env := testEnvelope()

	// Test that the HTTP request is made correctly by checking the server received it.
	// We need to handle the nil repo panic, so wrap in a recover.
	func() {
		defer func() { recover() }()
		d.Deliver(t.Context(), wh, env)
	}()

	if !received.Load() {
		t.Error("server did not receive the webhook delivery")
	}
}

func TestDelivererSignatureVerification(t *testing.T) {
	secret := "webhook-secret-123"
	var sigValid atomic.Bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		body = body[:n]

		sig := r.Header.Get(SignatureHeader)
		if Verify(secret, body, sig) {
			sigValid.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := NewDeliverer(nil, DelivererConfig{
		MaxRetries:        1,
		TimeoutSec:        5,
		BackoffInitialSec: 1,
		BackoffMaxSec:     1,
		CBFailThreshold:   5,
		CBResetTimeoutSec: 60,
	}, nil, urlvalidation.AllowPrivateIPs())
	d.repo = nil

	wh := WebhookEndpoint{
		URL:    ts.URL,
		Secret: secret,
	}
	wh.ID = "wh-sig"

	func() {
		defer func() { recover() }()
		d.Deliver(t.Context(), wh, testEnvelope())
	}()

	if !sigValid.Load() {
		t.Error("webhook signature was not valid")
	}
}
