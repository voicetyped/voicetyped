package engine

import (
	"context"
	"io"
)

// ASRResult represents a speech-to-text result.
type ASRResult struct {
	Text       string
	Confidence float32
	Language   string
	IsFinal    bool
	Segments   []Segment
}

// Segment is a timed piece of a transcription.
type Segment struct {
	Text       string
	StartMs    int
	EndMs      int
	Confidence float32
}

// ModelInfo describes an available model for a backend.
type ModelInfo struct {
	ID          string
	DisplayName string
	IsDefault   bool
}

// ASREngine transcribes audio streams.
type ASREngine interface {
	Transcribe(ctx context.Context, audio io.Reader) (<-chan ASRResult, error)
	Models() []ModelInfo
	Close() error
}
