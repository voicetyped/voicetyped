package main

import (
	"context"
	"log"
	"net/http"

	"github.com/pitabwire/frame"
	"github.com/pitabwire/frame/config"

	vtconfig "github.com/voicetyped/voicetyped/config"
	"github.com/voicetyped/voicetyped/gen/voicetyped/dialog/v1/dialogv1connect"
	"github.com/voicetyped/voicetyped/internal/connectutil"
	dialoghandler "github.com/voicetyped/voicetyped/internal/dialog/handler"
	"github.com/voicetyped/voicetyped/pkg/dialog"
	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/hooks"
)

func main() {
	ctx := context.Background()

	cfg, err := config.LoadWithOIDC[vtconfig.DialogConfig](ctx)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	eventRef := cfg.GetEventsQueueName()
	eventURL := cfg.GetEventsQueueURL()

	ctx, srv := frame.NewService(
		frame.WithConfig(&cfg),
		frame.WithName("voicetyped-dialog"),
		frame.WithRegisterServerOauth2Client(),
		frame.WithRegisterPublisher(eventRef, eventURL),
	)
	defer srv.Stop(ctx)

	pool, err := srv.WorkManager().GetPool()
	if err != nil {
		log.Fatalf("getting worker pool: %v", err)
	}

	authenticator := srv.SecurityManager().GetAuthenticator(ctx)

	pub := events.NewPublisher(srv.QueueManager(), "dialog", eventRef)
	hookExec := hooks.NewExecutor(pub)

	loader := dialog.NewLoader(cfg.DialogDir)
	if _, err := loader.LoadAll(); err != nil {
		log.Printf("warning: loading dialogs: %v", err)
	}

	handler := dialoghandler.NewDialogHandler(loader, hookExec, pub, pool)

	mux := http.NewServeMux()
	opts, err := connectutil.AuthenticatedOptions(ctx, authenticator)
	if err != nil {
		log.Fatalf("setting up auth interceptors: %v", err)
	}
	path, hdlr := dialogv1connect.NewDialogServiceHandler(handler, opts...)
	mux.Handle(path, hdlr)

	// Start session reaper.
	handler.StartReaper(ctx)

	srv.Init(ctx, frame.WithHTTPHandler(connectutil.H2CHandler(mux)))

	if err := srv.Run(ctx, ""); err != nil {
		log.Fatalf("service exited: %v", err)
	}
}
