package engine

import (
	"context"
	"io"
)

// Voice describes an available TTS voice.
type Voice struct {
	ID       string
	Name     string
	Language string
}

// TTSEngine synthesizes speech from text.
type TTSEngine interface {
	Synthesize(ctx context.Context, text string, voice string) (io.Reader, error)
	Voices() []Voice
	Models() []ModelInfo
	Close() error
}
