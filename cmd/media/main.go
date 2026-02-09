package main

import (
	"context"
	"log"
	"net/http"

	"github.com/pitabwire/frame"
	"github.com/pitabwire/frame/config"

	vtconfig "github.com/voicetyped/voicetyped/config"
	"github.com/voicetyped/voicetyped/gen/voicetyped/media/v1/mediav1connect"
	"github.com/voicetyped/voicetyped/internal/connectutil"
	mediahandler "github.com/voicetyped/voicetyped/internal/media/handler"
	"github.com/voicetyped/voicetyped/internal/media/sfu"
)

func main() {
	ctx := context.Background()

	cfg, err := config.LoadWithOIDC[vtconfig.MediaConfig](ctx)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	ctx, srv := frame.NewService(
		frame.WithConfig(&cfg),
		frame.WithName("voicetyped-media"),
		frame.WithRegisterServerOauth2Client(),
	)
	defer srv.Stop(ctx)

	pool, err := srv.WorkManager().GetPool()
	if err != nil {
		log.Fatalf("getting worker pool: %v", err)
	}

	authenticator := srv.SecurityManager().GetAuthenticator(ctx)

	sfuInstance := sfu.New(sfu.SFUConfig{
		WebRTCConfig:              cfg.WebRTCConfig(),
		SimulcastEnabled:          cfg.SimulcastEnabled,
		SVCEnabled:                cfg.SVCEnabled,
		SpeakerDetectorIntervalMs: cfg.SpeakerDetectorIntervalMs,
		SpeakerDetectorThreshold:  cfg.SpeakerDetectorThreshold,
		DefaultMaxPublishers:      cfg.DefaultMaxPublishers,
		DefaultAutoSubscribeAudio: cfg.DefaultAutoSubscribeAudio,
		E2EEDefaultRequired:       cfg.E2EEDefaultRequired,
	}, pool)
	handler := mediahandler.NewMediaHandler(sfuInstance, pool)

	mux := http.NewServeMux()
	opts, err := connectutil.AuthenticatedOptions(ctx, authenticator)
	if err != nil {
		log.Fatalf("setting up auth interceptors: %v", err)
	}
	path, hdlr := mediav1connect.NewMediaServiceHandler(handler, opts...)
	mux.Handle(path, hdlr)

	srv.Init(ctx, frame.WithHTTPHandler(connectutil.H2CHandler(mux)))

	if err := srv.Run(ctx, ""); err != nil {
		log.Fatalf("service exited: %v", err)
	}
}
