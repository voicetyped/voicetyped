package restutil

import (
	"context"
	"io"

	"github.com/voicetyped/voicetyped/internal/speech/engine"
)

// TranscribeFunc transcribes a single utterance of raw PCM audio and returns
// the transcription text with confidence. Called once per VAD-detected utterance.
type TranscribeFunc func(ctx context.Context, pcm []byte) (string, float32, error)

// VADBatchTranscribe reads PCM audio from the reader, uses VAD to detect
// utterance boundaries, and calls transcribeFn for each complete utterance.
// Results are sent on the returned channel, which is closed when the reader
// is exhausted or the context is cancelled.
func VADBatchTranscribe(ctx context.Context, audio io.Reader, transcribeFn TranscribeFunc) <-chan engine.ASRResult {
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
						text, conf, txErr := transcribeFn(ctx, utterance)
						if txErr == nil && text != "" {
							results <- engine.ASRResult{
								Text:       text,
								Confidence: conf,
								IsFinal:    true,
							}
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
				if err == io.EOF && len(utterance) > 0 {
					text, conf, txErr := transcribeFn(ctx, utterance)
					if txErr == nil && text != "" {
						results <- engine.ASRResult{
							Text:       text,
							Confidence: conf,
							IsFinal:    true,
						}
					}
				}
				return
			}
		}
	}()

	return results
}
