package whisper

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/voicetyped/voicetyped/internal/speech/engine"
	"github.com/voicetyped/voicetyped/internal/speech/registry"
)

func init() {
	registry.ASR.Register("whisper", func(config map[string]string) (engine.ASREngine, error) {
		modelPath := config["model_path"]
		if modelPath == "" {
			// Derive model path from model name if specified.
			if m := config["model"]; m != "" {
				modelPath = "./models/" + m + ".bin"
			} else {
				modelPath = "./models/ggml-base.bin"
			}
		}
		poolSize := 2
		if s := config["pool_size"]; s != "" {
			if v, err := strconv.Atoi(s); err == nil {
				poolSize = v
			}
		}
		return NewWhisperASR(modelPath, poolSize)
	})
}

// WhisperASR implements ASREngine using whisper.cpp bindings.
// This is a placeholder that will be connected to the actual whisper.cpp
// Go bindings when the C library is available.
type WhisperASR struct {
	modelPath string
	poolSize  int

	mu     sync.Mutex
	closed bool
}

// NewWhisperASR creates a new Whisper ASR engine.
func NewWhisperASR(modelPath string, poolSize int) (*WhisperASR, error) {
	if poolSize <= 0 {
		poolSize = 2
	}

	return &WhisperASR{
		modelPath: modelPath,
		poolSize:  poolSize,
	}, nil
}

// Transcribe reads PCM audio from the reader and returns transcription results.
func (w *WhisperASR) Transcribe(ctx context.Context, audio io.Reader) (<-chan engine.ASRResult, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, fmt.Errorf("whisper ASR is closed")
	}
	w.mu.Unlock()

	results := make(chan engine.ASRResult, 8)

	go func() {
		defer close(results)

		vad := engine.NewVAD(engine.DefaultVADConfig())
		frameSize := 16000 * 30 / 1000 * 2 // 30ms at 16kHz, 16-bit
		buf := make([]byte, frameSize)
		var utterance []byte

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := audio.Read(buf)
			if n > 0 {
				event := vad.ProcessFrame(buf[:n])

				switch event {
				case engine.VADSpeechStart:
					utterance = utterance[:0]
					utterance = append(utterance, buf[:n]...)

				case engine.VADSpeechEnd:
					if len(utterance) > 0 {
						// In a real implementation, this would send the
						// utterance to the whisper.cpp worker pool.
						results <- engine.ASRResult{
							Text:       "[whisper transcription placeholder]",
							Confidence: 0.0,
							IsFinal:    true,
						}
						utterance = utterance[:0]
					}

				default:
					if vad.IsSpeaking() {
						utterance = append(utterance, buf[:n]...)
					}
				}
			}

			if err != nil {
				if err == io.EOF {
					if len(utterance) > 0 {
						results <- engine.ASRResult{
							Text:       "[whisper transcription placeholder]",
							Confidence: 0.0,
							IsFinal:    true,
						}
					}
				}
				return
			}
		}
	}()

	return results, nil
}

// Models returns the available Whisper models.
func (w *WhisperASR) Models() []engine.ModelInfo {
	return []engine.ModelInfo{
		{ID: "ggml-base", DisplayName: "Whisper Base", IsDefault: true},
		{ID: "ggml-small", DisplayName: "Whisper Small"},
		{ID: "ggml-medium", DisplayName: "Whisper Medium"},
		{ID: "ggml-large-v3", DisplayName: "Whisper Large v3"},
	}
}

// Close releases all whisper model resources.
func (w *WhisperASR) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}
