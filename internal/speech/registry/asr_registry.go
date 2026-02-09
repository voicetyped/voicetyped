package registry

import "github.com/voicetyped/voicetyped/internal/speech/engine"

// ASR is the global ASR engine registry.
var ASR = New[engine.ASREngine]()
