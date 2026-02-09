package piper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/voicetyped/voicetyped/internal/speech/engine"
	"github.com/voicetyped/voicetyped/internal/speech/registry"
)

func init() {
	registry.TTS.Register("piper", func(config map[string]string) (engine.TTSEngine, error) {
		binaryPath := config["binary_path"]
		if binaryPath == "" {
			binaryPath = "piper"
		}
		modelPath := config["model_path"]
		if modelPath == "" {
			modelPath = "./models/en_US-amy-medium.onnx"
		}
		return NewPiperTTS(binaryPath, modelPath), nil
	})
}

// PiperTTS implements TTSEngine using the Piper TTS binary.
type PiperTTS struct {
	binaryPath string
	modelPath  string
}

// NewPiperTTS creates a new Piper TTS engine.
func NewPiperTTS(binaryPath, modelPath string) *PiperTTS {
	return &PiperTTS{
		binaryPath: binaryPath,
		modelPath:  modelPath,
	}
}

// Synthesize generates speech audio from text.
// Returns a reader producing 16kHz 16-bit mono PCM audio.
func (p *PiperTTS) Synthesize(ctx context.Context, text string, _ string) (io.Reader, error) {
	cmd := exec.CommandContext(ctx, p.binaryPath,
		"--model", p.modelPath,
		"--output-raw",
	)

	cmd.Stdin = bytes.NewBufferString(text)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("piper TTS: %w: %s", err, stderr.String())
	}

	return &stdout, nil
}

// Voices returns available TTS voices.
func (p *PiperTTS) Voices() []engine.Voice {
	return []engine.Voice{
		{
			ID:       "default",
			Name:     "Default",
			Language: "en-US",
		},
	}
}

// Models returns available Piper models.
func (p *PiperTTS) Models() []engine.ModelInfo {
	return []engine.ModelInfo{
		{ID: "en_US-amy-medium", DisplayName: "Amy (Medium)", IsDefault: true},
	}
}

// Close releases TTS resources.
func (p *PiperTTS) Close() error {
	return nil
}
