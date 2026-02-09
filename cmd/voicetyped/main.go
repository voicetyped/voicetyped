package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/pitabwire/frame"
	"github.com/pitabwire/frame/config"
	"github.com/pitabwire/frame/workerpool"

	vtconfig "github.com/voicetyped/voicetyped/config"
	"github.com/voicetyped/voicetyped/gen/voicetyped/dialog/v1/dialogv1connect"
	"github.com/voicetyped/voicetyped/gen/voicetyped/integration/v1/integrationv1connect"
	"github.com/voicetyped/voicetyped/gen/voicetyped/media/v1/mediav1connect"
	"github.com/voicetyped/voicetyped/gen/voicetyped/speech/v1/speechv1connect"
	"github.com/voicetyped/voicetyped/internal/connectutil"
	dialoghandler "github.com/voicetyped/voicetyped/internal/dialog/handler"
	integrationhandler "github.com/voicetyped/voicetyped/internal/integration/handler"
	mediahandler "github.com/voicetyped/voicetyped/internal/media/handler"
	"github.com/voicetyped/voicetyped/internal/media/sfu"
	"github.com/voicetyped/voicetyped/internal/runtime"
	speechhandler "github.com/voicetyped/voicetyped/internal/speech/handler"
	"github.com/voicetyped/voicetyped/pkg/dialog"
	"github.com/voicetyped/voicetyped/pkg/events"
	"github.com/voicetyped/voicetyped/pkg/hooks"
	"github.com/voicetyped/voicetyped/pkg/webhook"
	webhookapi "github.com/voicetyped/voicetyped/pkg/webhook/api"

	// Register speech backends via init().
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/deepgram"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/elevenlabs"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/google"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/openai"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/piper"
	_ "github.com/voicetyped/voicetyped/internal/speech/backends/whisper"
)

func main() {
	ctx := context.Background()

	cfg, err := config.LoadWithOIDC[vtconfig.MonolithConfig](ctx)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	eventRef := cfg.GetEventsQueueName()
	eventURL := cfg.GetEventsQueueURL()

	ctx, srv := frame.NewService(
		frame.WithConfig(&cfg),
		frame.WithName("voicetyped"),
		frame.WithRegisterServerOauth2Client(),
		frame.WithDatastore(),
		frame.WithRegisterPublisher(eventRef, eventURL),
		frame.WithWorkerPoolOptions(
			workerpool.WithPoolCount(cfg.WorkerPoolCount),
			workerpool.WithSinglePoolCapacity(cfg.WorkerPoolCapacity),
		),
	)
	defer srv.Stop(ctx)

	pool, err := srv.WorkManager().GetPool()
	if err != nil {
		log.Fatalf("getting worker pool: %v", err)
	}

	// Obtain the frame authenticator for JWT validation.
	authenticator := srv.SecurityManager().GetAuthenticator(ctx)

	pub := events.NewPublisher(srv.QueueManager(), "system", eventRef)

	// --- Media Service ---
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
	mediaHdlr := mediahandler.NewMediaHandler(sfuInstance, pool)

	// --- Speech Service ---
	speechServiceConfig := map[string]string{
		"model_path":         cfg.WhisperModelPath,
		"pool_size":          fmt.Sprintf("%d", cfg.WhisperPoolSize),
		"binary_path":        cfg.PiperBinaryPath,
		"deepgram_api_key":   cfg.DeepgramAPIKey,
		"google_api_key":     cfg.GoogleAPIKey,
		"elevenlabs_api_key": cfg.ElevenLabsAPIKey,
		"openai_api_key":     cfg.OpenAIAPIKey,
		"openai_base_url":    cfg.OpenAIBaseURL,
	}
	speechHdlr := speechhandler.NewSpeechHandler(cfg.DefaultASRBackend, cfg.DefaultTTSBackend, pool, speechServiceConfig)

	// --- Dialog Service ---
	hookExec := hooks.NewExecutor(pub)
	loader := dialog.NewLoader(cfg.DialogDir)
	if _, err := loader.LoadAll(); err != nil {
		log.Printf("warning: loading dialogs: %v", err)
	}
	dialogHdlr := dialoghandler.NewDialogHandler(loader, hookExec, pub, pool)

	// --- Integration Service ---
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
	intHdlr := integrationhandler.NewIntegrationHandler(whRepo, pub)

	// --- Orchestrator ---
	// In monolith mode, Connect RPC clients point to localhost.
	// Frame defaults to :8080 for its HTTP server (env HTTP_PORT).
	baseURL := "http://localhost" + cfg.HTTPPort()
	mediaURL := cfg.MediaServiceURL
	if mediaURL == "" {
		mediaURL = baseURL
	}
	speechURL := cfg.SpeechServiceURL
	if speechURL == "" {
		speechURL = baseURL
	}
	dialogURL := cfg.DialogServiceURL
	if dialogURL == "" {
		dialogURL = baseURL
	}

	orch := runtime.NewOrchestrator(mediaURL, speechURL, dialogURL, pub, cfg.DefaultDialog, pool)

	// Wire media handler to notify orchestrator when peers join.
	// IMPORTANT: Use the service-level ctx (not the request ctx) so the
	// orchestrator pipeline survives beyond the JoinRoom RPC call.
	mediaHdlr.SetOnPeerJoined(func(_ context.Context, roomID, peerID string, metadata map[string]string) {
		dialogName := metadata["dialog"]
		_ = pool.Submit(ctx, func() {
			orch.HandleNewRoom(ctx, roomID, peerID, dialogName)
		})
	})

	// --- HTTP Mux: all services on one server ---
	mux := http.NewServeMux()

	// Authenticated Connect RPC handler options using frame's security interceptors.
	opts, err := connectutil.AuthenticatedOptions(ctx, authenticator)
	if err != nil {
		log.Fatalf("setting up auth interceptors: %v", err)
	}

	path, h := mediav1connect.NewMediaServiceHandler(mediaHdlr, opts...)
	mux.Handle(path, h)
	path, h = speechv1connect.NewSpeechServiceHandler(speechHdlr, opts...)
	mux.Handle(path, h)
	path, h = dialogv1connect.NewDialogServiceHandler(dialogHdlr, opts...)
	mux.Handle(path, h)
	path, h = integrationv1connect.NewIntegrationServiceHandler(intHdlr, opts...)
	mux.Handle(path, h)

	// Backward-compatible REST webhook API with authentication middleware.
	whHandler := webhookapi.NewHandler(whRepo, pub)
	restMux := http.NewServeMux()
	whHandler.RegisterRoutes(restMux)
	mux.Handle("/api/", connectutil.AuthenticatedHTTPMiddleware(restMux, authenticator))

	// Start session reaper for dialog handler.
	dialogHdlr.StartReaper(ctx)

	srv.Init(ctx,
		frame.WithRegisterSubscriber(eventRef+".webhooks", eventURL, whSubscriber),
		frame.WithHTTPHandler(connectutil.H2CHandler(mux)),
	)

	if err := srv.Run(ctx, ""); err != nil {
		log.Fatalf("service exited: %v", err)
	}
}
