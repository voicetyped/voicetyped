package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/pitabwire/frame"
	"github.com/pitabwire/frame/config"

	vtconfig "github.com/voicetyped/voicetyped/config"
	"github.com/voicetyped/voicetyped/gen/voicetyped/speech/v1/speechv1connect"
	"github.com/voicetyped/voicetyped/internal/connectutil"
	speechhandler "github.com/voicetyped/voicetyped/internal/speech/handler"

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

	cfg, err := config.LoadWithOIDC[vtconfig.SpeechConfig](ctx)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	ctx, srv := frame.NewService(
		frame.WithConfig(&cfg),
		frame.WithName("voicetyped-speech"),
		frame.WithRegisterServerOauth2Client(),
	)
	defer srv.Stop(ctx)

	pool, err := srv.WorkManager().GetPool()
	if err != nil {
		log.Fatalf("getting worker pool: %v", err)
	}

	authenticator := srv.SecurityManager().GetAuthenticator(ctx)

	serviceConfig := map[string]string{
		"model_path":        cfg.WhisperModelPath,
		"pool_size":         fmt.Sprintf("%d", cfg.WhisperPoolSize),
		"binary_path":       cfg.PiperBinaryPath,
		"deepgram_api_key":  cfg.DeepgramAPIKey,
		"google_api_key":    cfg.GoogleAPIKey,
		"elevenlabs_api_key": cfg.ElevenLabsAPIKey,
		"openai_api_key":    cfg.OpenAIAPIKey,
		"openai_base_url":   cfg.OpenAIBaseURL,
	}
	handler := speechhandler.NewSpeechHandler(cfg.DefaultASRBackend, cfg.DefaultTTSBackend, pool, serviceConfig)

	mux := http.NewServeMux()
	opts, err := connectutil.AuthenticatedOptions(ctx, authenticator)
	if err != nil {
		log.Fatalf("setting up auth interceptors: %v", err)
	}
	path, hdlr := speechv1connect.NewSpeechServiceHandler(handler, opts...)
	mux.Handle(path, hdlr)

	srv.Init(ctx, frame.WithHTTPHandler(connectutil.H2CHandler(mux)))

	if err := srv.Run(ctx, ""); err != nil {
		log.Fatalf("service exited: %v", err)
	}
}
