package deepgram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"

	"github.com/voicetyped/voicetyped/internal/speech/backends/restutil"
	"github.com/voicetyped/voicetyped/internal/speech/engine"
	"github.com/voicetyped/voicetyped/internal/speech/registry"
)

func init() {
	registry.ASR.Register("deepgram", func(config map[string]string) (engine.ASREngine, error) {
		apiKey := config["deepgram_api_key"]
		if apiKey == "" {
			apiKey = config["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("deepgram API key required (set deepgram_api_key in config)")
		}
		model := config["model"]
		if model == "" {
			model = "nova-2"
		}
		lang := config["language"]
		if lang == "" {
			lang = "en"
		}
		return &DeepgramASR{apiKey: apiKey, model: model, language: lang}, nil
	})
}

type deepgramResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string  `json:"transcript"`
				Confidence float32 `json:"confidence"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// DeepgramASR implements ASREngine using the Deepgram REST API.
type DeepgramASR struct {
	apiKey   string
	model    string
	language string
}

func (d *DeepgramASR) Transcribe(ctx context.Context, audio io.Reader) (<-chan engine.ASRResult, error) {
	return restutil.VADBatchTranscribe(ctx, audio, d.transcribeUtterance), nil
}

func (d *DeepgramASR) transcribeUtterance(_ context.Context, pcm []byte) (string, float32, error) {
	params := url.Values{}
	params.Set("model", d.model)
	params.Set("language", d.language)
	apiURL := "https://api.deepgram.com/v1/listen?" + params.Encode()

	headers := map[string]string{
		"Authorization": "Token " + d.apiKey,
		"Content-Type":  "audio/l16;rate=16000;channels=1",
	}

	body, err := restutil.DoRaw("POST", apiURL, headers, bytes.NewReader(pcm))
	if err != nil {
		return "", 0, fmt.Errorf("deepgram API: %w", err)
	}
	defer body.Close()

	var resp deepgramResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return "", 0, fmt.Errorf("deepgram decode: %w", err)
	}

	if len(resp.Results.Channels) > 0 && len(resp.Results.Channels[0].Alternatives) > 0 {
		alt := resp.Results.Channels[0].Alternatives[0]
		return alt.Transcript, alt.Confidence, nil
	}
	return "", 0, nil
}

func (d *DeepgramASR) Models() []engine.ModelInfo {
	return []engine.ModelInfo{
		{ID: "nova-2", DisplayName: "Nova 2", IsDefault: true},
		{ID: "nova-2-general", DisplayName: "Nova 2 General"},
		{ID: "nova-2-meeting", DisplayName: "Nova 2 Meeting"},
		{ID: "nova-2-phonecall", DisplayName: "Nova 2 Phone Call"},
		{ID: "enhanced", DisplayName: "Enhanced"},
		{ID: "base", DisplayName: "Base"},
	}
}

func (d *DeepgramASR) Close() error {
	return nil
}
