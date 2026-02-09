package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/voicetyped/voicetyped/internal/speech/backends/restutil"
	"github.com/voicetyped/voicetyped/internal/speech/engine"
	"github.com/voicetyped/voicetyped/internal/speech/registry"
)

func init() {
	registry.TTS.Register("elevenlabs", func(config map[string]string) (engine.TTSEngine, error) {
		apiKey := config["elevenlabs_api_key"]
		if apiKey == "" {
			apiKey = config["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("elevenlabs API key required (set elevenlabs_api_key in config)")
		}
		model := config["model"]
		if model == "" {
			model = "eleven_multilingual_v2"
		}
		return &ElevenLabsTTS{apiKey: apiKey, model: model}, nil
	})
}

type elevenLabsRequest struct {
	Text          string                `json:"text"`
	ModelID       string                `json:"model_id"`
	VoiceSettings elevenLabsVoiceConfig `json:"voice_settings"`
}

type elevenLabsVoiceConfig struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
}

// ElevenLabsTTS implements TTSEngine using the ElevenLabs REST API.
type ElevenLabsTTS struct {
	apiKey string
	model  string
}

func (e *ElevenLabsTTS) Synthesize(_ context.Context, text string, voice string) (io.Reader, error) {
	if voice == "" {
		voice = "21m00Tcm4TlvDq8ikWAM" // Rachel (default)
	}

	apiURL := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s?output_format=pcm_16000", voice)

	headers := map[string]string{
		"xi-api-key":   e.apiKey,
		"Content-Type": "application/json",
	}

	req := elevenLabsRequest{
		Text:    text,
		ModelID: e.model,
		VoiceSettings: elevenLabsVoiceConfig{
			Stability:       0.5,
			SimilarityBoost: 0.75,
		},
	}

	body, err := restutil.DoRaw("POST", apiURL, headers, marshalJSON(req))
	if err != nil {
		return nil, fmt.Errorf("elevenlabs TTS: %w", err)
	}
	defer body.Close()

	// ElevenLabs returns raw 16kHz PCM directly with pcm_16000 format.
	pcm, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs TTS read: %w", err)
	}
	return bytes.NewReader(pcm), nil
}

func (e *ElevenLabsTTS) Voices() []engine.Voice {
	return []engine.Voice{
		{ID: "21m00Tcm4TlvDq8ikWAM", Name: "Rachel", Language: "en"},
		{ID: "AZnzlk1XvdvUeBnXmlld", Name: "Domi", Language: "en"},
		{ID: "EXAVITQu4vr4xnSDxMaL", Name: "Bella", Language: "en"},
		{ID: "ErXwobaYiN019PkySvjV", Name: "Antoni", Language: "en"},
	}
}

func (e *ElevenLabsTTS) Models() []engine.ModelInfo {
	return []engine.ModelInfo{
		{ID: "eleven_multilingual_v2", DisplayName: "Multilingual v2", IsDefault: true},
		{ID: "eleven_monolingual_v1", DisplayName: "Monolingual v1"},
		{ID: "eleven_turbo_v2", DisplayName: "Turbo v2"},
	}
}

func (e *ElevenLabsTTS) Close() error {
	return nil
}

func marshalJSON(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}
