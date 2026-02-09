package google

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/voicetyped/voicetyped/internal/speech/backends/restutil"
	"github.com/voicetyped/voicetyped/internal/speech/engine"
	"github.com/voicetyped/voicetyped/internal/speech/registry"
)

func init() {
	registry.TTS.Register("google", func(config map[string]string) (engine.TTSEngine, error) {
		apiKey := config["google_api_key"]
		if apiKey == "" {
			apiKey = config["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("google API key required (set google_api_key in config)")
		}
		model := config["model"]
		return &GoogleTTS{apiKey: apiKey, model: model}, nil
	})
}

type googleSynthRequest struct {
	Input       googleSynthInput       `json:"input"`
	Voice       googleSynthVoice       `json:"voice"`
	AudioConfig googleSynthAudioConfig `json:"audioConfig"`
}

type googleSynthInput struct {
	Text string `json:"text"`
}

type googleSynthVoice struct {
	LanguageCode string `json:"languageCode"`
	Name         string `json:"name"`
}

type googleSynthAudioConfig struct {
	AudioEncoding   string `json:"audioEncoding"`
	SampleRateHertz int    `json:"sampleRateHertz"`
}

type googleSynthResponse struct {
	AudioContent string `json:"audioContent"` // base64-encoded
}

// GoogleTTS implements TTSEngine using the Google Cloud Text-to-Speech REST API.
type GoogleTTS struct {
	apiKey string
	model  string
}

func (g *GoogleTTS) Synthesize(_ context.Context, text string, voice string) (io.Reader, error) {
	apiURL := "https://texttospeech.googleapis.com/v1/text:synthesize?key=" + g.apiKey

	if voice == "" {
		voice = "en-US-Neural2-A"
	}

	req := googleSynthRequest{
		Input: googleSynthInput{Text: text},
		Voice: googleSynthVoice{
			LanguageCode: "en-US",
			Name:         voice,
		},
		AudioConfig: googleSynthAudioConfig{
			AudioEncoding:   "LINEAR16",
			SampleRateHertz: 16000,
		},
	}

	var resp googleSynthResponse
	if err := restutil.DoJSON("POST", apiURL, nil, req, &resp); err != nil {
		return nil, fmt.Errorf("google TTS: %w", err)
	}

	pcm, err := base64.StdEncoding.DecodeString(resp.AudioContent)
	if err != nil {
		return nil, fmt.Errorf("google TTS decode audio: %w", err)
	}

	return bytes.NewReader(pcm), nil
}

func (g *GoogleTTS) Voices() []engine.Voice {
	return []engine.Voice{
		{ID: "en-US-Neural2-A", Name: "Neural2 A (Female)", Language: "en-US"},
		{ID: "en-US-Neural2-C", Name: "Neural2 C (Female)", Language: "en-US"},
		{ID: "en-US-Studio-M", Name: "Studio M (Male)", Language: "en-US"},
		{ID: "en-US-Studio-O", Name: "Studio O (Female)", Language: "en-US"},
	}
}

func (g *GoogleTTS) Models() []engine.ModelInfo {
	return []engine.ModelInfo{
		{ID: "neural2", DisplayName: "Neural2", IsDefault: true},
		{ID: "studio", DisplayName: "Studio"},
	}
}

func (g *GoogleTTS) Close() error {
	return nil
}
