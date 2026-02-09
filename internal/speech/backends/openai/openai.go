package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"

	"github.com/voicetyped/voicetyped/internal/speech/backends/restutil"
	"github.com/voicetyped/voicetyped/internal/speech/engine"
	"github.com/voicetyped/voicetyped/internal/speech/registry"
)

func init() {
	registry.ASR.Register("openai", func(config map[string]string) (engine.ASREngine, error) {
		apiKey := config["openai_api_key"]
		if apiKey == "" {
			apiKey = config["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("openai API key required (set openai_api_key in config)")
		}
		baseURL := config["openai_base_url"]
		if baseURL == "" {
			baseURL = config["base_url"]
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		model := config["model"]
		if model == "" {
			model = "whisper-1"
		}
		return &OpenAIASR{apiKey: apiKey, baseURL: baseURL, model: model}, nil
	})

	registry.TTS.Register("openai", func(config map[string]string) (engine.TTSEngine, error) {
		apiKey := config["openai_api_key"]
		if apiKey == "" {
			apiKey = config["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("openai API key required (set openai_api_key in config)")
		}
		baseURL := config["openai_base_url"]
		if baseURL == "" {
			baseURL = config["base_url"]
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		model := config["model"]
		if model == "" {
			model = "tts-1"
		}
		return &OpenAITTS{apiKey: apiKey, baseURL: baseURL, model: model}, nil
	})
}

// --- ASR ---

// OpenAIASR implements ASREngine using the OpenAI-compatible transcription API.
type OpenAIASR struct {
	apiKey  string
	baseURL string
	model   string
}

func (o *OpenAIASR) Transcribe(ctx context.Context, audio io.Reader) (<-chan engine.ASRResult, error) {
	return restutil.VADBatchTranscribe(ctx, audio, o.transcribeUtterance), nil
}

func (o *OpenAIASR) transcribeUtterance(_ context.Context, pcm []byte) (string, float32, error) {
	// Wrap raw PCM as WAV for the OpenAI API (requires a file format).
	var wavBuf bytes.Buffer
	if err := writeWAVHeader(&wavBuf, len(pcm)); err != nil {
		return "", 0, fmt.Errorf("openai ASR: write WAV header: %w", err)
	}
	wavBuf.Write(pcm)

	// Build multipart form.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", 0, fmt.Errorf("openai ASR: create form file: %w", err)
	}
	if _, err := part.Write(wavBuf.Bytes()); err != nil {
		return "", 0, fmt.Errorf("openai ASR: write form file: %w", err)
	}
	_ = writer.WriteField("model", o.model)
	_ = writer.WriteField("response_format", "json")
	writer.Close()

	headers := map[string]string{
		"Authorization": "Bearer " + o.apiKey,
		"Content-Type":  writer.FormDataContentType(),
	}

	apiURL := o.baseURL + "/audio/transcriptions"
	respBody, err := restutil.DoRaw("POST", apiURL, headers, &body)
	if err != nil {
		return "", 0, fmt.Errorf("openai ASR: %w", err)
	}
	defer respBody.Close()

	var resp struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(respBody).Decode(&resp); err != nil {
		return "", 0, fmt.Errorf("openai ASR decode: %w", err)
	}

	return resp.Text, 0.9, nil
}

func (o *OpenAIASR) Models() []engine.ModelInfo {
	return []engine.ModelInfo{
		{ID: "whisper-1", DisplayName: "Whisper 1", IsDefault: true},
	}
}

func (o *OpenAIASR) Close() error {
	return nil
}

// --- TTS ---

// OpenAITTS implements TTSEngine using the OpenAI-compatible speech API.
type OpenAITTS struct {
	apiKey  string
	baseURL string
	model   string
}

type openAITTSRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format"`
}

func (o *OpenAITTS) Synthesize(_ context.Context, text string, voice string) (io.Reader, error) {
	if voice == "" {
		voice = "alloy"
	}

	apiURL := o.baseURL + "/audio/speech"

	reqBody := openAITTSRequest{
		Model:          o.model,
		Input:          text,
		Voice:          voice,
		ResponseFormat: "pcm",
	}
	reqJSON, _ := json.Marshal(reqBody)

	headers := map[string]string{
		"Authorization": "Bearer " + o.apiKey,
		"Content-Type":  "application/json",
	}

	body, err := restutil.DoRaw("POST", apiURL, headers, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("openai TTS: %w", err)
	}
	defer body.Close()

	// OpenAI TTS with pcm format returns 24kHz 16-bit mono PCM.
	// Downsample to 16kHz for consistency with other backends.
	pcm24, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("openai TTS read: %w", err)
	}

	pcm16 := resample24to16(pcm24)
	return bytes.NewReader(pcm16), nil
}

func (o *OpenAITTS) Voices() []engine.Voice {
	return []engine.Voice{
		{ID: "alloy", Name: "Alloy", Language: "en"},
		{ID: "echo", Name: "Echo", Language: "en"},
		{ID: "fable", Name: "Fable", Language: "en"},
		{ID: "onyx", Name: "Onyx", Language: "en"},
		{ID: "nova", Name: "Nova", Language: "en"},
		{ID: "shimmer", Name: "Shimmer", Language: "en"},
	}
}

func (o *OpenAITTS) Models() []engine.ModelInfo {
	return []engine.ModelInfo{
		{ID: "tts-1", DisplayName: "TTS 1", IsDefault: true},
		{ID: "tts-1-hd", DisplayName: "TTS 1 HD"},
	}
}

func (o *OpenAITTS) Close() error {
	return nil
}
