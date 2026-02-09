package webhook

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/pitabwire/frame/workerpool"
	"github.com/pitabwire/util"

	"github.com/voicetyped/voicetyped/pkg/events"
)

// Subscriber implements queue.SubscribeWorker to route events to matching webhooks.
type Subscriber struct {
	Repo      *Repository
	Deliverer *Deliverer
	Pool      workerpool.WorkerPool
}

// Handle is called by frame's pub/sub for each event message.
func (ws *Subscriber) Handle(ctx context.Context, _ map[string]string, message []byte) error {
	var env events.Envelope
	if err := json.Unmarshal(message, &env); err != nil {
		util.Log(ctx).WithError(err).Error("webhook subscriber: unmarshal envelope")
		return err
	}

	webhooks, err := ws.Repo.ListByEventType(ctx, env.Type)
	if err != nil {
		util.Log(ctx).WithError(err).Error("webhook subscriber: list webhooks")
		return err
	}

	for _, wh := range webhooks {
		wh := wh
		env := env
		if ws.Pool != nil {
			if err := ws.Pool.Submit(ctx, func() {
				ws.Deliverer.Deliver(ctx, wh, env)
			}); err != nil {
				slog.WarnContext(ctx, "webhook pool full", slog.String("webhook_id", wh.ID))
			}
		} else {
			go ws.Deliverer.Deliver(ctx, wh, env)
		}
	}

	return nil
}
