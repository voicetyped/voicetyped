package engine

import (
	"encoding/binary"
	"math"
)

// VADConfig holds voice activity detection parameters.
type VADConfig struct {
	EnergyThreshold float64 // RMS energy threshold for speech
	SpeechMinDurMs  int     // Minimum duration to confirm speech start
	SilenceMinDurMs int     // Minimum duration to confirm speech end
	SampleRate      int     // Audio sample rate in Hz
	FrameSizeMs     int     // Frame size in milliseconds
}

// DefaultVADConfig returns sensible defaults for 16kHz audio.
func DefaultVADConfig() VADConfig {
	return VADConfig{
		EnergyThreshold: 500,
		SpeechMinDurMs:  200,
		SilenceMinDurMs: 700,
		SampleRate:      16000,
		FrameSizeMs:     30,
	}
}

// VAD performs energy-based voice activity detection on PCM audio.
type VAD struct {
	config        VADConfig
	isSpeaking    bool
	speechFrames  int
	silenceFrames int
	frameSamples  int
}

// NewVAD creates a new voice activity detector.
func NewVAD(cfg VADConfig) *VAD {
	return &VAD{
		config:       cfg,
		frameSamples: cfg.SampleRate * cfg.FrameSizeMs / 1000,
	}
}

// VADEvent indicates a speech boundary.
type VADEvent int

const (
	VADNone       VADEvent = iota
	VADSpeechStart
	VADSpeechEnd
)

// ProcessFrame analyzes a frame of 16-bit PCM audio and returns a VAD event.
func (v *VAD) ProcessFrame(pcm []byte) VADEvent {
	energy := rmsEnergy(pcm)

	framesPerMs := float64(v.config.SampleRate) / 1000.0 / float64(v.frameSamples)

	if energy >= v.config.EnergyThreshold {
		v.silenceFrames = 0
		v.speechFrames++
		speechDurMs := float64(v.speechFrames) / framesPerMs

		if !v.isSpeaking && speechDurMs >= float64(v.config.SpeechMinDurMs) {
			v.isSpeaking = true
			return VADSpeechStart
		}
	} else {
		v.speechFrames = 0
		v.silenceFrames++
		silenceDurMs := float64(v.silenceFrames) / framesPerMs

		if v.isSpeaking && silenceDurMs >= float64(v.config.SilenceMinDurMs) {
			v.isSpeaking = false
			return VADSpeechEnd
		}
	}

	return VADNone
}

// IsSpeaking returns whether speech is currently detected.
func (v *VAD) IsSpeaking() bool {
	return v.isSpeaking
}

// Reset clears the VAD state.
func (v *VAD) Reset() {
	v.isSpeaking = false
	v.speechFrames = 0
	v.silenceFrames = 0
}

// rmsEnergy computes the root-mean-square energy of 16-bit signed PCM audio.
func rmsEnergy(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}

	numSamples := len(pcm) / 2
	var sumSquares float64

	for i := 0; i < numSamples; i++ {
		sample := int16(binary.LittleEndian.Uint16(pcm[i*2 : i*2+2]))
		sumSquares += float64(sample) * float64(sample)
	}

	return math.Sqrt(sumSquares / float64(numSamples))
}
