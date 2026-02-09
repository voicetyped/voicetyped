package google

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/voicetyped/voicetyped/internal/speech/backends/restutil"
	"github.com/voicetyped/voicetyped/internal/speech/engine"
	"github.com/voicetyped/voicetyped/internal/speech/registry"
)

func init() {
	registry.ASR.Register("google", func(config map[string]string) (engine.ASREngine, error) {
		apiKey := config["google_api_key"]
		if apiKey == "" {
			apiKey = config["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("google API key required (set google_api_key in config)")
		}
		model := config["model"]
		if model == "" {
			model = "latest_long"
		}
		lang := config["language"]
		if lang == "" {
			lang = "en-US"
		}
		return &GoogleASR{apiKey: apiKey, model: model, language: lang}, nil
	})
}

type googleRecognizeRequest struct {
	Config googleRecognizeConfig `json:"config"`
	Audio  googleRecognizeAudio  `json:"audio"`
}

type googleRecognizeConfig struct {
	Encoding        string `json:"encoding"`
	SampleRateHertz int    `json:"sampleRateHertz"`
	LanguageCode    string `json:"languageCode"`
	Model           string `json:"model"`
}

type googleRecognizeAudio struct {
	Content string `json:"content"`
}

type googleRecognizeResponse struct {
	Results []struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float32 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"results"`
}

// GoogleASR implements ASREngine using the Google Cloud Speech-to-Text REST API.
type GoogleASR struct {
	apiKey   string
	model    string
	language string
}

func (g *GoogleASR) Transcribe(ctx context.Context, audio io.Reader) (<-chan engine.ASRResult, error) {
	return restutil.VADBatchTranscribe(ctx, audio, g.transcribeUtterance), nil
}

func (g *GoogleASR) transcribeUtterance(_ context.Context, pcm []byte) (string, float32, error) {
	apiURL := "https://speech.googleapis.com/v1/speech:recognize?key=" + g.apiKey

	req := googleRecognizeRequest{
		Config: googleRecognizeConfig{
			Encoding:        "LINEAR16",
			SampleRateHertz: 16000,
			LanguageCode:    g.language,
			Model:           g.model,
		},
		Audio: googleRecognizeAudio{
			Content: base64.StdEncoding.EncodeToString(pcm),
		},
	}

	var resp googleRecognizeResponse
	if err := restutil.DoJSON("POST", apiURL, nil, req, &resp); err != nil {
		return "", 0, fmt.Errorf("google ASR: %w", err)
	}

	if len(resp.Results) > 0 && len(resp.Results[0].Alternatives) > 0 {
		alt := resp.Results[0].Alternatives[0]
		return alt.Transcript, alt.Confidence, nil
	}
	return "", 0, nil
}

func (g *GoogleASR) Models() []engine.ModelInfo {
	return []engine.ModelInfo{
		{ID: "latest_long", DisplayName: "Latest Long", IsDefault: true},
		{ID: "latest_short", DisplayName: "Latest Short"},
		{ID: "chirp_2", DisplayName: "Chirp 2"},
		{ID: "chirp", DisplayName: "Chirp"},
	}
}

func (g *GoogleASR) Close() error {
	return nil
}
