package handler

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	integrationv1 "github.com/voicetyped/voicetyped/gen/voicetyped/integration/v1"
	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/webhook"
)

// TestSubscribeEventsNilPublisher tests that SubscribeEvents returns an error when
// the publisher is not configured.
func TestSubscribeEventsNilPublisher(t *testing.T) {
	handler := NewIntegrationHandler(nil, nil)

	err := handler.SubscribeEvents(
		context.Background(),
		connect.NewRequest(&integrationv1.SubscribeEventsRequest{
			EventTypes: []string{"call.started"},
		}),
		nil, // stream is not used when pub is nil
	)
	if err == nil {
		t.Fatal("expected error when publisher is nil")
	}
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Errorf("got code %v, want Unavailable", connect.CodeOf(err))
	}
}

// TestWebhookToProto tests the conversion of WebhookEndpoint to proto.
func TestWebhookToProto(t *testing.T) {
	wh := &webhook.WebhookEndpoint{
		Name:        "Test Webhook",
		URL:         "https://example.com/hook",
		EventTypes:  webhook.EventTypesJSON{events.CallStarted, events.CallTerminated},
		IsActive:    true,
		Description: "A test webhook",
	}

	proto := webhookToProto(wh)

	if proto.Name != "Test Webhook" {
		t.Errorf("got name %q, want Test Webhook", proto.Name)
	}
	if proto.Url != "https://example.com/hook" {
		t.Errorf("got URL %q, want https://example.com/hook", proto.Url)
	}
	if len(proto.EventTypes) != 2 {
		t.Errorf("got %d event types, want 2", len(proto.EventTypes))
	}
	if proto.EventTypes[0] != "call.started" {
		t.Errorf("got event type %q, want call.started", proto.EventTypes[0])
	}
	if proto.EventTypes[1] != "call.terminated" {
		t.Errorf("got event type %q, want call.terminated", proto.EventTypes[1])
	}
	if !proto.IsActive {
		t.Error("expected IsActive to be true")
	}
	if proto.Description != "A test webhook" {
		t.Errorf("got description %q, want 'A test webhook'", proto.Description)
	}
}

// TestNewIntegrationHandler tests creation of the handler.
func TestNewIntegrationHandler(t *testing.T) {
	handler := NewIntegrationHandler(nil, nil)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.repo != nil {
		t.Error("expected nil repo")
	}
	if handler.pub != nil {
		t.Error("expected nil pub")
	}
}
