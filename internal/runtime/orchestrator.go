package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/pitabwire/frame/workerpool"

	commonv1 "github.com/voicetyped/voicetyped/gen/voicetyped/common/v1"
	dialogv1 "github.com/voicetyped/voicetyped/gen/voicetyped/dialog/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/dialog/v1/dialogv1connect"
	mediav1 "github.com/voicetyped/voicetyped/gen/voicetyped/media/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/media/v1/mediav1connect"
	speechv1 "github.com/voicetyped/voicetyped/gen/voicetyped/speech/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/speech/v1/speechv1connect"
	"github.com/voicetyped/voicetyped/internal/connectutil"
	"github.com/voicetyped/voicetyped/pkg/events"
)

// Orchestrator wires media, speech, and dialog together using Connect RPC clients.
type Orchestrator struct {
	media         mediav1connect.MediaServiceClient
	speech        speechv1connect.SpeechServiceClient
	dialog        dialogv1connect.DialogServiceClient
	pub           *events.Publisher
	defaultDialog string
	pool          workerpool.WorkerPool
}

// NewOrchestrator creates an orchestrator with Connect RPC clients.
func NewOrchestrator(mediaURL, speechURL, dialogURL string, pub *events.Publisher, defaultDialog string, pool workerpool.WorkerPool) *Orchestrator {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			MaxConnsPerHost:     50,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}
	opts := connectutil.DefaultClientOptions()

	return &Orchestrator{
		media:         mediav1connect.NewMediaServiceClient(httpClient, mediaURL, opts...),
		speech:        speechv1connect.NewSpeechServiceClient(httpClient, speechURL, opts...),
		dialog:        dialogv1connect.NewDialogServiceClient(httpClient, dialogURL, opts...),
		pub:           pub,
		defaultDialog: defaultDialog,
		pool:          pool,
	}
}

// NewOrchestratorFromClients creates an orchestrator with pre-built clients (for testing).
func NewOrchestratorFromClients(
	media mediav1connect.MediaServiceClient,
	speech speechv1connect.SpeechServiceClient,
	dialog dialogv1connect.DialogServiceClient,
	pub *events.Publisher,
	defaultDialog string,
	pool workerpool.WorkerPool,
) *Orchestrator {
	return &Orchestrator{
		media:         media,
		speech:        speech,
		dialog:        dialog,
		pub:           pub,
		defaultDialog: defaultDialog,
		pool:          pool,
	}
}

// HandleNewRoom handles a new peer joining a room, orchestrating the
// media -> speech -> dialog pipeline via Connect RPC.
func (o *Orchestrator) HandleNewRoom(ctx context.Context, roomID, peerID, dialogName string) {
	if dialogName == "" {
		dialogName = o.defaultDialog
	}
	sessionID := fmt.Sprintf("%s-%s", roomID, peerID)

	slog.InfoContext(ctx, "orchestrator: handling new room",
		slog.String("room_id", roomID),
		slog.String("peer_id", peerID),
		slog.String("dialog", dialogName),
	)

	// 1. Subscribe to room audio.
	audioStream, err := o.media.SubscribeAudio(ctx, connect.NewRequest(&mediav1.SubscribeAudioRequest{
		RoomId: roomID,
		PeerId: peerID,
	}))
	if err != nil {
		slog.ErrorContext(ctx, "orchestrator: subscribe audio failed", slog.String("error", err.Error()))
		return
	}

	// 2. Start bidi transcription stream.
	transcribeStream := o.speech.Transcribe(ctx)

	// Send transcription config.
	// Audio from the SFU is Opus-encoded; the speech handler decodes to 16kHz PCM.
	if err := transcribeStream.Send(&speechv1.TranscribeRequest{
		Message: &speechv1.TranscribeRequest_Config{
			Config: &speechv1.TranscribeConfig{
				SessionId:      sessionID,
				Backend:        "",
				InterimResults: true,
				SampleRate:     48000,
				Codec:          "audio/opus",
			},
		},
	}); err != nil {
		slog.ErrorContext(ctx, "orchestrator: send transcribe config failed", slog.String("error", err.Error()))
		transcribeStream.CloseRequest()
		transcribeStream.CloseResponse()
		return
	}

	// 3. Start dialog.
	startResp, err := o.dialog.StartDialog(ctx, connect.NewRequest(&dialogv1.StartDialogRequest{
		SessionId:  sessionID,
		DialogName: dialogName,
	}))
	if err != nil {
		slog.ErrorContext(ctx, "orchestrator: start dialog failed", slog.String("error", err.Error()))
		transcribeStream.CloseRequest()
		transcribeStream.CloseResponse()
		return
	}

	// Ensure dialog cleanup always runs.
	defer func() {
		_, _ = o.dialog.EndDialog(ctx, connect.NewRequest(&dialogv1.EndDialogRequest{
			SessionId: sessionID,
		}))
	}()

	// Execute initial actions.
	o.executeActions(ctx, roomID, sessionID, startResp.Msg.Actions)

	// 4. Pipe audio from media to speech via worker pool.
	pipeCtx, pipeCancel := context.WithCancel(ctx)
	defer pipeCancel()

	pipeFunc := func() {
		defer pipeCancel()
		for audioStream.Receive() {
			msg := audioStream.Msg()
			if msg.Frame != nil {
				if err := transcribeStream.Send(&speechv1.TranscribeRequest{
					Message: &speechv1.TranscribeRequest_Audio{
						Audio: &commonv1.AudioFrame{
							Data:       msg.Frame.Data,
							SampleRate: msg.Frame.SampleRate,
							Channels:   msg.Frame.Channels,
						},
					},
				}); err != nil {
					return
				}
			}
		}
		transcribeStream.CloseRequest()
	}

	if o.pool != nil {
		if err := o.pool.Submit(pipeCtx, pipeFunc); err != nil {
			slog.ErrorContext(ctx, "orchestrator: submit audio pipe failed", slog.String("error", err.Error()))
			return
		}
	} else {
		go pipeFunc()
	}

	// 5. Main loop: receive ASR results and forward to dialog.
	for {
		select {
		case <-pipeCtx.Done():
			// Audio pipe exited (peer left or stream error).
			return
		default:
		}

		resp, err := transcribeStream.Receive()
		if err != nil {
			if err == io.EOF {
				break
			}
			slog.ErrorContext(ctx, "orchestrator: receive transcribe failed", slog.String("error", err.Error()))
			break
		}

		if !resp.IsFinal {
			continue
		}

		// Forward final ASR result to dialog.
		eventResp, err := o.dialog.SendEvent(ctx, connect.NewRequest(&dialogv1.SendEventRequest{
			SessionId: sessionID,
			EventType: "speech",
			EventData: resp.Text,
		}))
		if err != nil {
			slog.ErrorContext(ctx, "orchestrator: send dialog event failed", slog.String("error", err.Error()))
			continue
		}

		// Execute returned actions.
		o.executeActions(ctx, roomID, sessionID, eventResp.Msg.Actions)

		if eventResp.Msg.Terminal {
			// Dialog is done. Leave the room.
			_, _ = o.media.LeaveRoom(ctx, connect.NewRequest(&mediav1.LeaveRoomRequest{
				RoomId: roomID,
				PeerId: peerID,
			}))
			break
		}
	}
}

// executeActions processes action directives from the dialog engine.
func (o *Orchestrator) executeActions(ctx context.Context, roomID, sessionID string, actions []*dialogv1.ActionDirective) {
	for _, action := range actions {
		switch action.Type {
		case "play_tts":
			text := action.Params["text"]
			if text == "" {
				continue
			}
			o.playTTS(ctx, roomID, text)

		case "hangup":
			slog.InfoContext(ctx, "orchestrator: hangup action", slog.String("session_id", sessionID))
			return

		default:
			slog.DebugContext(ctx, "orchestrator: unhandled action",
				slog.String("type", action.Type),
				slog.String("session_id", sessionID),
			)
		}
	}
}

// playTTS synthesizes text and streams the audio into the room via PlayAudio.
func (o *Orchestrator) playTTS(ctx context.Context, roomID, text string) {
	synthStream, err := o.speech.Synthesize(ctx, connect.NewRequest(&speechv1.SynthesizeRequest{
		Text: text,
	}))
	if err != nil {
		slog.ErrorContext(ctx, "orchestrator: synthesize failed", slog.String("error", err.Error()))
		return
	}

	playStream := o.media.PlayAudio(ctx)

	for synthStream.Receive() {
		msg := synthStream.Msg()
		if msg.Audio == nil {
			continue
		}
		if err := playStream.Send(&mediav1.PlayAudioRequest{
			RoomId: roomID,
			Frame: &commonv1.AudioFrame{
				Data:       msg.Audio.Data,
				Codec:      msg.Audio.Codec,
				SampleRate: msg.Audio.SampleRate,
				Channels:   msg.Audio.Channels,
			},
		}); err != nil {
			slog.ErrorContext(ctx, "orchestrator: play audio send failed", slog.String("error", err.Error()))
			break
		}
	}

	if _, err := playStream.CloseAndReceive(); err != nil {
		slog.ErrorContext(ctx, "orchestrator: play audio close failed", slog.String("error", err.Error()))
	}
}
