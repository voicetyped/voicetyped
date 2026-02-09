package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"
	"github.com/rs/xid"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/voicetyped/voicetyped/gen/voicetyped/common/v1"
	integrationv1 "github.com/voicetyped/voicetyped/gen/voicetyped/integration/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/integration/v1/integrationv1connect"
	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/urlvalidation"
	"github.com/voicetyped/voicetyped/pkg/webhook"
)

// Ensure we implement the interface.
var _ integrationv1connect.IntegrationServiceHandler = (*IntegrationHandler)(nil)

// IntegrationHandler implements integrationv1connect.IntegrationServiceHandler.
type IntegrationHandler struct {
	repo *webhook.Repository
	pub  *events.Publisher
}

// NewIntegrationHandler creates a new integration service handler.
func NewIntegrationHandler(repo *webhook.Repository, pub *events.Publisher) *IntegrationHandler {
	return &IntegrationHandler{repo: repo, pub: pub}
}

func (h *IntegrationHandler) CreateWebhook(ctx context.Context, req *connect.Request[integrationv1.CreateWebhookRequest]) (*connect.Response[integrationv1.CreateWebhookResponse], error) {
	if err := urlvalidation.ValidateWebhookURL(req.Msg.Url); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid webhook URL: %w", err))
	}

	secret, err := webhook.GenerateSecret()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("generate secret: %w", err))
	}

	eventTypes := make(webhook.EventTypesJSON, 0, len(req.Msg.EventTypes))
	for _, et := range req.Msg.EventTypes {
		eventTypes = append(eventTypes, events.EventType(et))
	}

	wh := &webhook.WebhookEndpoint{
		Name:        req.Msg.Name,
		URL:         req.Msg.Url,
		Secret:      secret,
		EventTypes:  eventTypes,
		IsActive:    true,
		Description: req.Msg.Description,
	}

	if err := h.repo.CreateEndpoint(ctx, wh); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&integrationv1.CreateWebhookResponse{
		Webhook: webhookToProto(wh),
		Secret:  secret,
	}), nil
}

func (h *IntegrationHandler) GetWebhook(ctx context.Context, req *connect.Request[integrationv1.GetWebhookRequest]) (*connect.Response[integrationv1.GetWebhookResponse], error) {
	wh, err := h.repo.GetByID(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("webhook %q not found", req.Msg.Id))
	}

	return connect.NewResponse(&integrationv1.GetWebhookResponse{
		Webhook: webhookToProto(wh),
	}), nil
}

func (h *IntegrationHandler) ListWebhooks(ctx context.Context, _ *connect.Request[integrationv1.ListWebhooksRequest]) (*connect.Response[integrationv1.ListWebhooksResponse], error) {
	endpoints, err := h.repo.ListAll(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	webhooks := make([]*integrationv1.WebhookInfo, 0, len(endpoints))
	for i := range endpoints {
		webhooks = append(webhooks, webhookToProto(&endpoints[i]))
	}

	return connect.NewResponse(&integrationv1.ListWebhooksResponse{Webhooks: webhooks}), nil
}

func (h *IntegrationHandler) UpdateWebhook(ctx context.Context, req *connect.Request[integrationv1.UpdateWebhookRequest]) (*connect.Response[integrationv1.UpdateWebhookResponse], error) {
	wh, err := h.repo.GetByID(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("webhook %q not found", req.Msg.Id))
	}

	if req.Msg.Name != "" {
		wh.Name = req.Msg.Name
	}
	if req.Msg.Url != "" {
		if err := urlvalidation.ValidateWebhookURL(req.Msg.Url); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid webhook URL: %w", err))
		}
		wh.URL = req.Msg.Url
	}
	if len(req.Msg.EventTypes) > 0 {
		eventTypes := make(webhook.EventTypesJSON, 0, len(req.Msg.EventTypes))
		for _, et := range req.Msg.EventTypes {
			eventTypes = append(eventTypes, events.EventType(et))
		}
		wh.EventTypes = eventTypes
	}
	wh.IsActive = req.Msg.IsActive
	if req.Msg.Description != "" {
		wh.Description = req.Msg.Description
	}

	if err := h.repo.Update(ctx, wh); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&integrationv1.UpdateWebhookResponse{
		Webhook: webhookToProto(wh),
	}), nil
}

func (h *IntegrationHandler) DeleteWebhook(ctx context.Context, req *connect.Request[integrationv1.DeleteWebhookRequest]) (*connect.Response[integrationv1.DeleteWebhookResponse], error) {
	if err := h.repo.Delete(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&integrationv1.DeleteWebhookResponse{}), nil
}

func (h *IntegrationHandler) RotateSecret(ctx context.Context, req *connect.Request[integrationv1.RotateSecretRequest]) (*connect.Response[integrationv1.RotateSecretResponse], error) {
	wh, err := h.repo.GetByID(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("webhook %q not found", req.Msg.Id))
	}

	newSecret, err := webhook.GenerateSecret()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	wh.Secret = newSecret
	if err := h.repo.Update(ctx, wh); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&integrationv1.RotateSecretResponse{
		NewSecret: newSecret,
	}), nil
}

func (h *IntegrationHandler) TestWebhook(ctx context.Context, req *connect.Request[integrationv1.TestWebhookRequest]) (*connect.Response[integrationv1.TestWebhookResponse], error) {
	if h.pub != nil {
		err := h.pub.Emit(ctx, events.WebhookTest, "", &events.WebhookTestData{
			WebhookID: req.Msg.Id,
			Message:   "Test event from Connect RPC",
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	return connect.NewResponse(&integrationv1.TestWebhookResponse{Success: true}), nil
}

func (h *IntegrationHandler) ListDeliveries(ctx context.Context, req *connect.Request[integrationv1.ListDeliveriesRequest]) (*connect.Response[integrationv1.ListDeliveriesResponse], error) {
	attempts, err := h.repo.ListDeliveries(ctx, req.Msg.WebhookId, int(req.Msg.Limit), int(req.Msg.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	deliveries := make([]*integrationv1.DeliveryInfo, 0, len(attempts))
	for _, a := range attempts {
		deliveries = append(deliveries, &integrationv1.DeliveryInfo{
			Id:            a.ID,
			WebhookId:     a.WebhookID,
			EventId:       a.EventID,
			EventType:     a.EventType,
			ResponseCode:  int32(a.ResponseCode),
			AttemptNumber: int32(a.AttemptNumber),
			Status:        a.Status,
			Error:         a.Error,
			DurationMs:    a.DurationMs,
			CreatedAt:     timestamppb.New(a.CreatedAt),
		})
	}

	return connect.NewResponse(&integrationv1.ListDeliveriesResponse{Deliveries: deliveries}), nil
}

func (h *IntegrationHandler) ListDeadLetters(ctx context.Context, req *connect.Request[integrationv1.ListDeadLettersRequest]) (*connect.Response[integrationv1.ListDeadLettersResponse], error) {
	letters, err := h.repo.ListDeadLetters(ctx, req.Msg.WebhookId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	deadLetters := make([]*integrationv1.DeadLetterInfo, 0, len(letters))
	for _, dl := range letters {
		deadLetters = append(deadLetters, &integrationv1.DeadLetterInfo{
			Id:         dl.ID,
			WebhookId:  dl.WebhookID,
			EventId:    dl.EventID,
			EventType:  dl.EventType,
			Payload:    dl.Payload,
			LastError:  dl.LastError,
			Attempts:   int32(dl.Attempts),
			Replayable: dl.Replayable,
			CreatedAt:  timestamppb.New(dl.CreatedAt),
		})
	}

	return connect.NewResponse(&integrationv1.ListDeadLettersResponse{DeadLetters: deadLetters}), nil
}

func (h *IntegrationHandler) ReplayDeadLetter(ctx context.Context, req *connect.Request[integrationv1.ReplayDeadLetterRequest]) (*connect.Response[integrationv1.ReplayDeadLetterResponse], error) {
	dl, err := h.repo.GetDeadLetterByID(ctx, req.Msg.DeadLetterId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("dead letter %q not found", req.Msg.DeadLetterId))
	}

	// Re-publish the original event envelope.
	var env events.Envelope
	if err := json.Unmarshal([]byte(dl.Payload), &env); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("corrupt dead letter payload: %w", err))
	}

	if err := h.pub.Emit(ctx, env.Type, env.SessionID, json.RawMessage(env.Data)); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("re-publish failed: %w", err))
	}

	if err := h.repo.MarkDeadLetterReplayed(ctx, req.Msg.DeadLetterId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&integrationv1.ReplayDeadLetterResponse{Success: true}), nil
}

func (h *IntegrationHandler) SubscribeEvents(ctx context.Context, req *connect.Request[integrationv1.SubscribeEventsRequest], stream *connect.ServerStream[commonv1.EventEnvelope]) error {
	if h.pub == nil {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("event publisher not configured"))
	}

	// Create a unique subscription ID.
	subID := xid.New().String()
	eventCh := h.pub.Subscribe(subID, 128)
	defer h.pub.Unsubscribe(subID)

	// Build a filter set from the requested event types.
	allowedTypes := make(map[string]bool, len(req.Msg.EventTypes))
	for _, t := range req.Msg.EventTypes {
		allowedTypes[t] = true
	}

	// Stream matching events until the client disconnects.
	for {
		select {
		case <-ctx.Done():
			return nil
		case env, ok := <-eventCh:
			if !ok {
				return nil
			}
			// Filter by event type if any types were specified.
			if len(allowedTypes) > 0 && !allowedTypes[string(env.Type)] {
				continue
			}
			if err := stream.Send(&commonv1.EventEnvelope{
				Id:        env.ID,
				Type:      string(env.Type),
				Source:    env.Source,
				SessionId: env.SessionID,
				Timestamp: timestamppb.New(env.Timestamp),
				Data:      env.Data,
				Metadata:  env.Metadata,
			}); err != nil {
				return err
			}
		}
	}
}

func webhookToProto(wh *webhook.WebhookEndpoint) *integrationv1.WebhookInfo {
	eventTypes := make([]string, 0, len(wh.EventTypes))
	for _, et := range wh.EventTypes {
		eventTypes = append(eventTypes, string(et))
	}

	return &integrationv1.WebhookInfo{
		Id:           wh.ID,
		Name:         wh.Name,
		Url:          wh.URL,
		EventTypes:   eventTypes,
		IsActive:     wh.IsActive,
		Description:  wh.Description,
		FailureCount: int32(wh.FailureCount),
		CircuitState: wh.CircuitState,
		CreatedAt:    timestamppb.New(wh.CreatedAt),
	}
}
