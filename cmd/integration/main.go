package main

import (
	"context"
	"log"
	"net/http"

	"github.com/pitabwire/frame"
	"github.com/pitabwire/frame/config"

	vtconfig "github.com/voicetyped/voicetyped/config"
	"github.com/voicetyped/voicetyped/gen/voicetyped/integration/v1/integrationv1connect"
	"github.com/voicetyped/voicetyped/internal/connectutil"
	integrationhandler "github.com/voicetyped/voicetyped/internal/integration/handler"
	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/webhook"
	webhookapi "github.com/voicetyped/voicetyped/pkg/webhook/api"
)

func main() {
	ctx := context.Background()

	cfg, err := config.LoadWithOIDC[vtconfig.IntegrationConfig](ctx)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	eventRef := cfg.GetEventsQueueName()
	eventURL := cfg.GetEventsQueueURL()

	ctx, srv := frame.NewService(
		frame.WithConfig(&cfg),
		frame.WithName("voicetyped-integration"),
		frame.WithRegisterServerOauth2Client(),
		frame.WithDatastore(),
		frame.WithRegisterPublisher(eventRef, eventURL),
	)
	defer srv.Stop(ctx)

	pool, err := srv.WorkManager().GetPool()
	if err != nil {
		log.Fatalf("getting worker pool: %v", err)
	}

	authenticator := srv.SecurityManager().GetAuthenticator(ctx)

	pub := events.NewPublisher(srv.QueueManager(), "integration", eventRef)

	whRepo := webhook.NewRepository(
		srv.DatastoreManager().GetPool(ctx, "__default__pool_name__"),
	)
	whDeliverer := webhook.NewDeliverer(whRepo, webhook.DelivererConfig{
		MaxRetries:        cfg.WebhookMaxRetries,
		TimeoutSec:        cfg.WebhookTimeoutSec,
		BackoffInitialSec: cfg.WebhookBackoffSec,
		BackoffMaxSec:     cfg.WebhookBackoffMax,
		CBFailThreshold:   cfg.CBFailThreshold,
		CBResetTimeoutSec: cfg.CBResetTimeoutSec,
	}, pool)
	whSubscriber := &webhook.Subscriber{
		Repo:      whRepo,
		Deliverer: whDeliverer,
		Pool:      pool,
	}

	handler := integrationhandler.NewIntegrationHandler(whRepo, pub)

	mux := http.NewServeMux()
	opts, err := connectutil.AuthenticatedOptions(ctx, authenticator)
	if err != nil {
		log.Fatalf("setting up auth interceptors: %v", err)
	}
	path, hdlr := integrationv1connect.NewIntegrationServiceHandler(handler, opts...)
	mux.Handle(path, hdlr)

	// Backward-compatible REST webhook API with authentication middleware.
	whHandler := webhookapi.NewHandler(whRepo, pub)
	restMux := http.NewServeMux()
	whHandler.RegisterRoutes(restMux)
	mux.Handle("/api/", connectutil.AuthenticatedHTTPMiddleware(restMux, authenticator))

	srv.Init(ctx,
		frame.WithRegisterSubscriber(eventRef+".webhooks", eventURL, whSubscriber),
		frame.WithHTTPHandler(connectutil.H2CHandler(mux)),
	)

	if err := srv.Run(ctx, ""); err != nil {
		log.Fatalf("service exited: %v", err)
	}
}
