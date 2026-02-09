package registry

import "github.com/voicetyped/voicetyped/internal/speech/engine"

// TTS is the global TTS engine registry.
var TTS = New[engine.TTSEngine]()
