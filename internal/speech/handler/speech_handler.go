package handler

import (
	"context"
	"fmt"
	"io"
	"strings"

	"connectrpc.com/connect"
	"github.com/pitabwire/frame/workerpool"

	commonv1 "github.com/voicetyped/voicetyped/gen/voicetyped/common/v1"
	speechv1 "github.com/voicetyped/voicetyped/gen/voicetyped/speech/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/speech/v1/speechv1connect"
	"github.com/voicetyped/voicetyped/internal/speech/codec"
	"github.com/voicetyped/voicetyped/internal/speech/registry"
)

// Ensure we implement the interface.
var _ speechv1connect.SpeechServiceHandler = (*SpeechHandler)(nil)

// SpeechHandler implements speechv1connect.SpeechServiceHandler.
type SpeechHandler struct {
	defaultASRBackend string
	defaultTTSBackend string
	pool              workerpool.WorkerPool
	serviceConfig     map[string]string
}

// NewSpeechHandler creates a new speech service handler.
func NewSpeechHandler(defaultASR, defaultTTS string, pool workerpool.WorkerPool, serviceConfig map[string]string) *SpeechHandler {
	if defaultASR == "" {
		defaultASR = "whisper"
	}
	if defaultTTS == "" {
		defaultTTS = "piper"
	}
	if serviceConfig == nil {
		serviceConfig = map[string]string{}
	}
	return &SpeechHandler{
		defaultASRBackend: defaultASR,
		defaultTTSBackend: defaultTTS,
		pool:              pool,
		serviceConfig:     serviceConfig,
	}
}

// mergeConfig merges service-level config with per-request config.
// Per-request values take precedence over service-level defaults.
func (h *SpeechHandler) mergeConfig(perRequest map[string]string) map[string]string {
	merged := make(map[string]string, len(h.serviceConfig)+len(perRequest))
	for k, v := range h.serviceConfig {
		merged[k] = v
	}
	for k, v := range perRequest {
		if v != "" {
			merged[k] = v
		}
	}
	return merged
}

func (h *SpeechHandler) Transcribe(ctx context.Context, stream *connect.BidiStream[speechv1.TranscribeRequest, speechv1.TranscribeResponse]) error {
	// Read the first message to get configuration.
	firstMsg, err := stream.Receive()
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("expected config as first message: %w", err))
	}

	cfg := firstMsg.GetConfig()
	if cfg == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("first message must contain config"))
	}

	backend := cfg.Backend
	if backend == "" {
		backend = h.defaultASRBackend
	}

	// If the codec is Opus, the ASR engine receives 16kHz PCM regardless.
	inputCodec := strings.ToLower(cfg.Codec)
	needsOpusDecode := inputCodec == "audio/opus" || inputCodec == "opus"

	sampleRate := cfg.SampleRate
	if needsOpusDecode {
		// After Opus→PCM decode we produce 16kHz PCM.
		sampleRate = 16000
	}

	configMap := h.mergeConfig(map[string]string{
		"session_id":  cfg.SessionId,
		"language":    cfg.Language,
		"sample_rate": fmt.Sprintf("%d", sampleRate),
		"model":       cfg.Model,
	})

	asrEngine, err := registry.ASR.Create(backend, configMap)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("create ASR backend %q: %w", backend, err))
	}
	defer asrEngine.Close()

	// Create a pipe to feed audio from the stream to the ASR engine.
	pr, pw := io.Pipe()

	// If Opus, wrap the pipe writer with a decoder.
	var audioWriter io.Writer = pw
	if needsOpusDecode {
		audioWriter = codec.NewOpusToPCM16Writer(pw)
	}

	// Read audio frames from stream and write to pipe via worker pool.
	pipeFunc := func() {
		defer pw.Close()
		for {
			msg, err := stream.Receive()
			if err != nil {
				return
			}
			audio := msg.GetAudio()
			if audio == nil {
				continue
			}
			if _, err := audioWriter.Write(audio.Data); err != nil {
				return
			}
		}
	}

	if h.pool != nil {
		_ = h.pool.Submit(ctx, pipeFunc)
	} else {
		go pipeFunc()
	}

	// Start transcription.
	resultsCh, err := asrEngine.Transcribe(ctx, pr)
	if err != nil {
		pr.Close()
		return connect.NewError(connect.CodeInternal, err)
	}

	// Forward results to the stream.
	for result := range resultsCh {
		segments := make([]*speechv1.TranscribeSegment, 0, len(result.Segments))
		for _, s := range result.Segments {
			segments = append(segments, &speechv1.TranscribeSegment{
				Text:       s.Text,
				StartMs:    int32(s.StartMs),
				EndMs:      int32(s.EndMs),
				Confidence: s.Confidence,
			})
		}

		if err := stream.Send(&speechv1.TranscribeResponse{
			Text:       result.Text,
			Confidence: result.Confidence,
			IsFinal:    result.IsFinal,
			Segments:   segments,
			Language:   result.Language,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (h *SpeechHandler) Synthesize(ctx context.Context, req *connect.Request[speechv1.SynthesizeRequest], stream *connect.ServerStream[speechv1.SynthesizeResponse]) error {
	backend := req.Msg.Backend
	if backend == "" {
		backend = h.defaultTTSBackend
	}

	configMap := h.mergeConfig(map[string]string{
		"voice": req.Msg.Voice,
		"model": req.Msg.Model,
	})

	ttsEngine, err := registry.TTS.Create(backend, configMap)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("create TTS backend %q: %w", backend, err))
	}
	defer ttsEngine.Close()

	audio, err := ttsEngine.Synthesize(ctx, req.Msg.Text, req.Msg.Voice)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}

	// Stream audio in chunks.
	buf := make([]byte, 4096)
	for {
		n, err := audio.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := stream.Send(&speechv1.SynthesizeResponse{
				Audio: &commonv1.AudioFrame{
					Data:       chunk,
					Codec:      "pcm",
					SampleRate: 16000,
					Channels:   1,
				},
				Done: false,
			}); sendErr != nil {
				return sendErr
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return connect.NewError(connect.CodeInternal, err)
		}
	}

	// Send final message indicating completion.
	return stream.Send(&speechv1.SynthesizeResponse{Done: true})
}

func (h *SpeechHandler) ListVoices(_ context.Context, req *connect.Request[speechv1.ListVoicesRequest]) (*connect.Response[speechv1.ListVoicesResponse], error) {
	var voices []*speechv1.VoiceInfo

	backends := registry.TTS.List()
	for _, name := range backends {
		if req.Msg.Backend != "" && req.Msg.Backend != name {
			continue
		}

		ttsEngine, err := registry.TTS.Create(name, h.serviceConfig)
		if err != nil {
			continue
		}

		for _, v := range ttsEngine.Voices() {
			voices = append(voices, &speechv1.VoiceInfo{
				Id:       v.ID,
				Name:     v.Name,
				Language: v.Language,
				Backend:  name,
			})
		}
		ttsEngine.Close()
	}

	return connect.NewResponse(&speechv1.ListVoicesResponse{Voices: voices}), nil
}

func (h *SpeechHandler) ListBackends(_ context.Context, _ *connect.Request[speechv1.ListBackendsRequest]) (*connect.Response[speechv1.ListBackendsResponse], error) {
	asrBackends := make([]*speechv1.BackendInfo, 0)
	for _, name := range registry.ASR.List() {
		info := &speechv1.BackendInfo{
			Name: name,
			Type: "asr",
		}
		if eng, err := registry.ASR.Create(name, h.serviceConfig); err == nil {
			for _, m := range eng.Models() {
				info.Models = append(info.Models, m.ID)
				if m.IsDefault {
					info.DefaultModel = m.ID
				}
			}
			eng.Close()
		}
		asrBackends = append(asrBackends, info)
	}

	ttsBackends := make([]*speechv1.BackendInfo, 0)
	for _, name := range registry.TTS.List() {
		info := &speechv1.BackendInfo{
			Name: name,
			Type: "tts",
		}
		if eng, err := registry.TTS.Create(name, h.serviceConfig); err == nil {
			for _, m := range eng.Models() {
				info.Models = append(info.Models, m.ID)
				if m.IsDefault {
					info.DefaultModel = m.ID
				}
			}
			eng.Close()
		}
		ttsBackends = append(ttsBackends, info)
	}

	return connect.NewResponse(&speechv1.ListBackendsResponse{
		AsrBackends: asrBackends,
		TtsBackends: ttsBackends,
	}), nil
}

func (h *SpeechHandler) ListModels(_ context.Context, req *connect.Request[speechv1.ListModelsRequest]) (*connect.Response[speechv1.ListModelsResponse], error) {
	var models []*speechv1.ModelInfo

	filterBackend := req.Msg.Backend
	filterType := req.Msg.Type

	if filterType == "" || filterType == "asr" {
		for _, name := range registry.ASR.List() {
			if filterBackend != "" && filterBackend != name {
				continue
			}
			eng, err := registry.ASR.Create(name, h.serviceConfig)
			if err != nil {
				continue
			}
			for _, m := range eng.Models() {
				models = append(models, &speechv1.ModelInfo{
					Id:          m.ID,
					Backend:     name,
					Type:        "asr",
					DisplayName: m.DisplayName,
					IsDefault:   m.IsDefault,
				})
			}
			eng.Close()
		}
	}

	if filterType == "" || filterType == "tts" {
		for _, name := range registry.TTS.List() {
			if filterBackend != "" && filterBackend != name {
				continue
			}
			eng, err := registry.TTS.Create(name, h.serviceConfig)
			if err != nil {
				continue
			}
			for _, m := range eng.Models() {
				models = append(models, &speechv1.ModelInfo{
					Id:          m.ID,
					Backend:     name,
					Type:        "tts",
					DisplayName: m.DisplayName,
					IsDefault:   m.IsDefault,
				})
			}
			eng.Close()
		}
	}

	return connect.NewResponse(&speechv1.ListModelsResponse{Models: models}), nil
}
