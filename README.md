# Voicetyped

A real-time voice platform that combines WebRTC media routing, speech recognition/synthesis, programmable dialog flows, and webhook integrations into a single deployable system.

---

## Table of Contents

- [What This Is](#what-this-is)
- [Architecture Overview](#architecture-overview)
- [Project Structure](#project-structure)
- [Getting Started](#getting-started)
- [Configuration Reference](#configuration-reference)
- [Services In Depth](#services-in-depth)
  - [Media Service (SFU)](#media-service-sfu)
  - [Speech Service (ASR/TTS)](#speech-service-asrtts)
  - [Dialog Service (IVR)](#dialog-service-ivr)
  - [Integration Service (Webhooks)](#integration-service-webhooks)
  - [Orchestrator](#orchestrator)
- [API Reference](#api-reference)
- [Writing Dialog Flows](#writing-dialog-flows)
- [Speech Backend Guide](#speech-backend-guide)
- [Deployment](#deployment)
- [Extending the System](#extending-the-system)
- [Database Migrations](#database-migrations)
- [Testing](#testing)
- [Conventions and Guidelines](#conventions-and-guidelines)

---

## What This Is

Voicetyped is a platform for building voice-interactive applications. A caller connects via WebRTC, their audio is transcribed in real-time, a dialog engine decides what to say back, text-to-speech generates the response, and that audio is played back into the call. Webhooks notify external systems about everything that happens.

The system handles:
- **Multi-party audio/video routing** via a Selective Forwarding Unit (SFU)
- **Speech-to-text** using local models (Whisper) or cloud APIs (Deepgram, Google, OpenAI)
- **Text-to-speech** using local models (Piper) or cloud APIs (Google, ElevenLabs, OpenAI)
- **Programmable IVR dialogs** defined in YAML with FSM-based state machines
- **Webhook delivery** with retries, circuit breakers, dead letter queues, and HMAC signing
- **Live event streaming** for real-time monitoring

---

## Architecture Overview

```
                    WebRTC Client
                         |
                    [Media Service]
                    SFU (rooms, peers, tracks)
                         |
                    Opus audio stream
                         |
                   [Orchestrator]
                   /      |       \
          [Speech]    [Dialog]   [Integration]
          ASR/TTS     FSM/IVR    Webhooks
          6 backends  YAML defs  Circuit breakers
```

### Deployment Modes

**Monolith** (recommended for getting started): All four services run in a single binary (`cmd/voicetyped`). Services communicate via in-process Connect RPC calls over localhost.

**Polylith** (for scale): Each service runs as its own binary (`cmd/media`, `cmd/speech`, `cmd/dialog`, `cmd/integration`). Services communicate over the network via Connect RPC. Set `*_SERVICE_URL` environment variables to point services at each other.

### Technology Stack

| Layer | Technology |
|-------|-----------|
| RPC Framework | [ConnectRPC](https://connectrpc.com/) v1.19 (gRPC-compatible, HTTP/1.1+HTTP/2) |
| Service Framework | [pitabwire/frame](https://github.com/pitabwire/frame) v1.72 (config, auth, queues, datastore, telemetry) |
| WebRTC | [Pion](https://github.com/pion/webrtc) v4 (full SFU with simulcast, SVC, E2EE) |
| Database | PostgreSQL via GORM |
| Messaging | NATS (via frame) |
| Auth | OIDC/OAuth2 + JWT |
| Observability | OpenTelemetry (traces, metrics, logs) |
| Proto | Protocol Buffers with [Buf](https://buf.build/) |

### Key Design Decisions

- **Connect RPC everywhere**: All inter-service communication uses Connect RPC (not REST). This gives us bidi streaming (needed for real-time audio), type safety, and gRPC compatibility. The webhook REST API is the only exception, kept for backward compatibility.
- **H2C (HTTP/2 Cleartext)**: Bidi streaming requires HTTP/2. Rather than requiring TLS everywhere in development, we use H2C (HTTP/2 without TLS). All handlers are wrapped with `connectutil.H2CHandler()`.
- **Registry pattern for backends**: Speech backends register themselves via Go `init()` functions. Adding a new backend means creating a package with an `init()` that calls `registry.ASR.Register()` or `registry.TTS.Register()`, then importing it with a blank identifier `_ "path/to/backend"`.
- **Frame handles the boring stuff**: Config loading, OIDC, worker pools, queue management, datastore connections, and telemetry are all handled by the frame library. Services just declare what they need via `frame.With*()` options.

---

## Project Structure

```
voicetyped/
├── cmd/                          # Binary entry points
│   ├── voicetyped/main.go        # Monolith (all services)
│   ├── media/main.go             # Standalone media service
│   ├── speech/main.go            # Standalone speech service
│   ├── dialog/main.go            # Standalone dialog service
│   └── integration/main.go       # Standalone integration service
│
├── proto/voicetyped/             # Protobuf definitions (source of truth)
│   ├── common/v1/common.proto    # Shared types (AudioFrame, SessionInfo, EventEnvelope)
│   ├── media/v1/media.proto      # 14 RPCs: rooms, peers, tracks, SDP, audio
│   ├── speech/v1/speech.proto    # 5 RPCs: transcribe, synthesize, discovery
│   ├── dialog/v1/dialog.proto    # 5 RPCs: dialog lifecycle, events
│   └── integration/v1/           # 8 RPCs: webhooks, events, dead letters
│       └── integration.proto
│
├── gen/                          # Generated Go code (DO NOT EDIT)
│   └── voicetyped/*/v1/          # .pb.go and .connect.go files
│
├── config/
│   └── config.go                 # All config structs (MediaConfig, SpeechConfig, etc.)
│
├── internal/                     # Private implementation
│   ├── connectutil/              # Connect RPC helpers
│   │   ├── h2c.go                # HTTP/2 cleartext handler wrapper
│   │   └── interceptors.go       # Auth interceptors, client options
│   │
│   ├── runtime/
│   │   └── orchestrator.go       # Wires media -> speech -> dialog pipeline
│   │
│   ├── media/
│   │   ├── handler/              # Connect RPC handler for MediaService
│   │   ├── sfu/                  # Selective Forwarding Unit
│   │   │   ├── sfu.go            # SFU manager (create/get/close rooms)
│   │   │   ├── room.go           # Room (peers, tracks, subscriptions)
│   │   │   ├── peer.go           # Peer (PeerConnection wrapper)
│   │   │   ├── track.go          # Track metadata
│   │   │   ├── publisher_track.go # Published track management
│   │   │   ├── subscription.go   # Track subscription/forwarding
│   │   │   ├── forwarder.go      # RTP packet forwarding
│   │   │   ├── speaker_detector.go # Active speaker detection
│   │   │   └── encryption.go     # E2EE key management
│   │   └── sipbridge/
│   │       └── bridge.go         # SIP bridge (stub)
│   │
│   ├── speech/
│   │   ├── handler/              # Connect RPC handler for SpeechService
│   │   ├── engine/               # Interfaces
│   │   │   ├── asr.go            # ASREngine interface + ModelInfo type
│   │   │   ├── tts.go            # TTSEngine interface
│   │   │   └── vad.go            # Voice Activity Detection
│   │   ├── registry/             # Global backend registries
│   │   │   ├── registry.go       # Generic Registry[T] with Factory[T]
│   │   │   ├── asr_registry.go   # var ASR = New[engine.ASREngine]()
│   │   │   └── tts_registry.go   # var TTS = New[engine.TTSEngine]()
│   │   ├── codec/
│   │   │   └── opus.go           # Opus -> PCM16 decoder
│   │   └── backends/             # Speech engine implementations
│   │       ├── whisper/          # Local ASR (whisper.cpp placeholder)
│   │       ├── piper/            # Local TTS (piper binary)
│   │       ├── deepgram/         # Cloud ASR (REST API)
│   │       ├── google/           # Cloud ASR + TTS (REST API)
│   │       ├── elevenlabs/       # Cloud TTS (REST API)
│   │       ├── openai/           # OpenAI-compatible ASR + TTS
│   │       │   ├── openai.go     # Both ASR and TTS registration
│   │       │   ├── wav.go        # WAV header writer for uploads
│   │       │   └── resample.go   # 24kHz -> 16kHz downsampler
│   │       └── restutil/         # Shared HTTP helpers
│   │           ├── restutil.go   # DoJSON, DoRaw
│   │           └── vad_batch.go  # VAD + batch transcription loop
│   │
│   ├── dialog/
│   │   └── handler/              # Connect RPC handler for DialogService
│   │
│   └── integration/
│       └── handler/              # Connect RPC handler for IntegrationService
│
├── pkg/                          # Shared libraries (importable by external code)
│   ├── events/                   # Event system
│   │   ├── types.go              # EventType constants + payload structs
│   │   └── publisher.go          # Queue publisher + local fan-out
│   │
│   ├── hooks/                    # External hook calls
│   │   ├── types.go              # HookConfig, HookRequest, HookResponse
│   │   └── executor.go           # HTTP executor with HMAC/Bearer auth
│   │
│   ├── dialog/                   # Dialog engine core
│   │   ├── types.go              # Dialog, State, Transition, Action
│   │   ├── session.go            # Thread-safe session state
│   │   ├── template.go           # Go template evaluation with caching
│   │   ├── fsm.go                # State machine validation + transition eval
│   │   ├── loader.go             # YAML loading + hot-reload (fsnotify)
│   │   └── engine.go             # Dialog execution engine
│   │
│   ├── urlvalidation/
│   │   └── ssrf.go               # SSRF protection for webhook/hook URLs
│   │
│   └── webhook/                  # Webhook delivery system
│       ├── models.go             # WebhookEndpoint, DeliveryAttempt, DeadLetter
│       ├── repository.go         # PostgreSQL CRUD
│       ├── signer.go             # HMAC-SHA256 signing/verification
│       ├── circuit_breaker.go    # Per-endpoint circuit breaker
│       ├── deliverer.go          # HTTP delivery with retries + backoff
│       ├── subscriber.go         # Queue consumer -> webhook delivery
│       └── api/                  # REST API handlers + DTOs
│
├── dialogs/
│   └── example.yaml              # Example IVR dialog definition
│
├── migrations/                   # PostgreSQL migrations
│   ├── 0001/                     # Webhook tables
│   └── 0002/                     # Room + session tables
│
├── buf.yaml                      # Buf configuration
├── buf.gen.yaml                  # Buf code generation config
├── go.mod
└── go.sum
```

---

## Getting Started

### Prerequisites

- Go 1.25+
- [Buf CLI](https://buf.build/docs/installation/) (for proto generation)
- PostgreSQL (for webhooks and persistence)
- NATS (for event messaging, or use `mem://` for in-memory)

### Build

```bash
# Build all binaries
go build ./...

# Build just the monolith
go build -o voicetyped ./cmd/voicetyped

# Build individual services
go build -o media-svc ./cmd/media
go build -o speech-svc ./cmd/speech
go build -o dialog-svc ./cmd/dialog
go build -o integration-svc ./cmd/integration
```

### Run (Monolith Mode)

```bash
# Minimal - runs with in-memory defaults
./voicetyped

# With configuration
export ASR_BACKEND=whisper
export TTS_BACKEND=piper
export WHISPER_MODEL_PATH=./models/ggml-base.bin
export PIPER_MODEL_PATH=./models/en_US-amy-medium.onnx
export DIALOG_DIR=./dialogs
./voicetyped
```

The monolith starts all services on a single HTTP port (default `:8080`, configurable via `HTTP_PORT`).

### Run (Polylith Mode)

```bash
# Start each service separately
HTTP_PORT=:8081 ./media-svc
HTTP_PORT=:8082 ./speech-svc
HTTP_PORT=:8083 ./dialog-svc
HTTP_PORT=:8084 ./integration-svc

# Or use the monolith with service URLs pointing to separate instances
export MEDIA_SERVICE_URL=http://media-host:8081
export SPEECH_SERVICE_URL=http://speech-host:8082
export DIALOG_SERVICE_URL=http://dialog-host:8083
export INTEGRATION_SERVICE_URL=http://integration-host:8084
./voicetyped
```

### Regenerate Proto Code

```bash
# Lint proto files
buf lint

# Generate Go code
buf generate
```

Generated files land in `gen/`. Never edit these files directly.

### Run Tests

```bash
go test ./...
go test -race ./...   # with race detector
go vet ./...          # static analysis
```

---

## Configuration Reference

All configuration uses environment variables with `env:` struct tags (via `github.com/caarlos0/env/v11`). Defaults are provided via `envDefault:` tags.

### Media Service (`MediaConfig`)

| Variable | Default | Description |
|----------|---------|-------------|
| `STUN_SERVERS` | `stun:stun.l.google.com:19302` | Comma-separated STUN server URLs |
| `TURN_SERVERS` | _(empty)_ | Comma-separated TURN server URLs |
| `TURN_USERNAME` | _(empty)_ | TURN credential username |
| `TURN_PASSWORD` | _(empty)_ | TURN credential password |
| `MAX_ROOMS_PER_NODE` | `100` | Maximum concurrent rooms |
| `MAX_PUBLISHERS_PER_ROOM` | `100` | Maximum publishers in a room |
| `SIMULCAST_ENABLED` | `true` | Enable simulcast for video |
| `SVC_ENABLED` | `true` | Enable SVC (Scalable Video Coding) |
| `SPEAKER_DETECTOR_INTERVAL_MS` | `500` | Active speaker check interval |
| `SPEAKER_DETECTOR_THRESHOLD` | `30` | Audio level threshold for speaking |
| `E2EE_DEFAULT_REQUIRED` | `false` | Require E2EE by default |
| `AUTO_SUBSCRIBE_AUDIO` | `true` | Auto-subscribe peers to audio tracks |
| `SIP_LISTEN_ADDR` | `0.0.0.0:5060` | SIP bridge listen address |
| `SIP_TRANSPORT` | `udp` | SIP transport protocol |

### Speech Service (`SpeechConfig`)

| Variable | Default | Description |
|----------|---------|-------------|
| `ASR_BACKEND` | `whisper` | Default ASR backend |
| `TTS_BACKEND` | `piper` | Default TTS backend |
| `WHISPER_MODEL_PATH` | `./models/ggml-base.bin` | Path to Whisper model file |
| `WHISPER_POOL_SIZE` | `2` | Whisper worker pool size |
| `PIPER_MODEL_PATH` | `./models/en_US-amy-medium.onnx` | Path to Piper model file |
| `PIPER_BINARY_PATH` | `piper` | Path to Piper binary |
| `DEEPGRAM_API_KEY` | _(empty)_ | Deepgram API key |
| `GOOGLE_API_KEY` | _(empty)_ | Google Cloud API key |
| `ELEVENLABS_API_KEY` | _(empty)_ | ElevenLabs API key |
| `OPENAI_API_KEY` | _(empty)_ | OpenAI API key (or compatible) |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | OpenAI-compatible API base URL |

### Dialog Service (`DialogConfig`)

| Variable | Default | Description |
|----------|---------|-------------|
| `DIALOG_DIR` | `./dialogs` | Directory containing YAML dialog definitions |

### Integration Service (`IntegrationConfig`)

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBHOOK_WORKERS` | `16` | Concurrent webhook delivery workers |
| `WEBHOOK_MAX_RETRIES` | `5` | Maximum delivery retry attempts |
| `WEBHOOK_TIMEOUT_SEC` | `10` | HTTP timeout per delivery attempt |
| `WEBHOOK_BACKOFF_INITIAL_SEC` | `1` | Initial retry backoff |
| `WEBHOOK_BACKOFF_MAX_SEC` | `300` | Maximum retry backoff (5 minutes) |
| `CB_FAILURE_THRESHOLD` | `5` | Failures before circuit opens |
| `CB_RESET_TIMEOUT_SEC` | `60` | Time before circuit half-opens |

### Monolith-Only (`MonolithConfig`)

| Variable | Default | Description |
|----------|---------|-------------|
| `DEFAULT_DIALOG` | `example` | Default dialog name for new rooms |
| `MEDIA_SERVICE_URL` | _(empty, uses localhost)_ | Media service URL for polylith |
| `SPEECH_SERVICE_URL` | _(empty, uses localhost)_ | Speech service URL for polylith |
| `DIALOG_SERVICE_URL` | _(empty, uses localhost)_ | Dialog service URL for polylith |
| `INTEGRATION_SERVICE_URL` | _(empty, uses localhost)_ | Integration service URL for polylith |

### Frame-Level Configuration

These are inherited from the `pitabwire/frame` library:

| Variable | Default | Description |
|----------|---------|-------------|
| `HTTP_PORT` | `:8080` | HTTP server listen port |
| `DATABASE_URL` | _(empty)_ | PostgreSQL connection string |
| `QUEUE_URL` | `mem://` | Queue URL (NATS or `mem://`) |
| `OIDC_ISSUER_URL` | _(empty)_ | OIDC issuer for JWT validation |
| `LOG_LEVEL` | `info` | Logging level |

---

## Services In Depth

### Media Service (SFU)

The media service is a **Selective Forwarding Unit** (SFU) that routes WebRTC audio and video between participants. It does not transcode media - it forwards RTP packets from publishers to subscribers.

**Key concepts:**
- **Room**: A named space where peers can publish and subscribe to tracks. Created via `CreateRoom`, joined via `JoinRoom`.
- **Peer**: A participant in a room with a WebRTC PeerConnection. Each peer can publish tracks and subscribe to other peers' tracks.
- **Track**: An audio or video stream published by a peer. Other peers subscribe to receive a copy.
- **Subscription**: A link from a published track to a subscribing peer. The forwarder copies RTP packets.
- **Active Speaker Detection**: Energy-based detection that identifies which peer is currently speaking. Results streamed via `ActiveSpeakers` RPC.

**Data flow:**
```
Publisher Peer -> PeerConnection -> PublisherTrack -> Forwarder -> Subscription -> Subscriber Peer
```

**Special RPCs for orchestrator integration:**
- `SubscribeAudio`: Server-streaming RPC that taps into a room's audio and streams raw frames (used by orchestrator to feed audio to ASR).
- `PlayAudio`: Client-streaming RPC that injects audio frames into a room (used by orchestrator to play TTS output).

**Files:**
- `internal/media/sfu/sfu.go` - SFU manager: room lifecycle, config
- `internal/media/sfu/room.go` - Room: peer management, track routing, audio tap
- `internal/media/sfu/peer.go` - Peer: PeerConnection wrapper, ICE, SDP
- `internal/media/sfu/track.go` - Track metadata (kind, codec, dimensions)
- `internal/media/sfu/publisher_track.go` - Published track with simulcast layer selection
- `internal/media/sfu/subscription.go` - Subscription: links publisher to subscriber
- `internal/media/sfu/forwarder.go` - RTP packet forwarding between tracks
- `internal/media/sfu/speaker_detector.go` - Audio level analysis for speaker detection
- `internal/media/sfu/encryption.go` - E2EE AES-GCM key management
- `internal/media/handler/media_handler.go` - Connect RPC handler (14 RPCs)

### Speech Service (ASR/TTS)

The speech service provides speech-to-text (ASR) and text-to-speech (TTS) via a pluggable backend system. Eight backends are available across four ASR and four TTS engines.

**ASR pipeline:**
```
Audio stream -> [Opus decode if needed] -> io.Pipe -> ASREngine.Transcribe() -> results channel -> stream response
```

**TTS pipeline:**
```
SynthesizeRequest -> TTSEngine.Synthesize() -> io.Reader -> chunk and stream -> SynthesizeResponse
```

**Backend registry**: Backends register via `init()` functions using the global `registry.ASR` and `registry.TTS` registries. The handler creates engine instances per-request using `registry.ASR.Create(backendName, configMap)`.

**Config merging**: The handler merges service-level config (API keys, model paths from env vars) with per-request config (model, language from the RPC). Per-request values override service-level defaults.

**Available backends:**

| Backend | ASR | TTS | Type | Notes |
|---------|-----|-----|------|-------|
| `whisper` | Yes | - | Local | whisper.cpp placeholder, VAD-based |
| `piper` | - | Yes | Local | Calls piper binary, outputs raw PCM |
| `deepgram` | Yes | - | Cloud | REST API, VAD batching |
| `google` | Yes | Yes | Cloud | REST API, base64 audio encoding |
| `elevenlabs` | - | Yes | Cloud | REST API, raw PCM output |
| `openai` | Yes | Yes | Cloud | OpenAI-compatible, configurable base_url |

**Files:**
- `internal/speech/engine/asr.go` - `ASREngine` interface, `ModelInfo`, `ASRResult`
- `internal/speech/engine/tts.go` - `TTSEngine` interface, `Voice`
- `internal/speech/engine/vad.go` - Energy-based Voice Activity Detection
- `internal/speech/registry/registry.go` - Generic `Registry[T]` with `Factory[T]`
- `internal/speech/handler/speech_handler.go` - Connect RPC handler (5 RPCs)
- `internal/speech/codec/opus.go` - Opus to PCM16 decoder (48kHz -> 16kHz)
- `internal/speech/backends/restutil/` - Shared HTTP helpers and VAD batch loop

### Dialog Service (IVR)

The dialog service manages programmable voice interactions using YAML-defined finite state machines. Each dialog defines states, transitions (triggered by speech/DTMF events), and actions (TTS playback, webhook calls, variable manipulation).

**Key concepts:**
- **Dialog**: A YAML file defining states, transitions, and actions. Loaded from `DIALOG_DIR`.
- **Session**: A running instance of a dialog for a specific call. Holds current state, variables, and history.
- **State**: A node in the FSM. Has `on_enter` actions, outbound transitions, and optional timeout.
- **Transition**: A rule: "when event X happens and condition Y is true, go to state Z and execute actions A".
- **Action**: Something to do: `play_tts`, `call_hook`, `set_variable`, `hangup`, `play_audio`.

**Lifecycle:**
1. `StartDialog` creates a session, enters the initial state, runs `on_enter` actions, returns action directives
2. `SendEvent` delivers speech/DTMF events to the FSM, evaluates transitions, returns new actions
3. `EndDialog` cleans up the session and cancels the background loop

**Template expressions**: Conditions and action params support Go templates with access to `.Variables`, `.Event`, `.Result`, and `.Session`. Results are cached for performance.

**Hot-reload**: The loader watches the dialog directory with fsnotify and reloads YAML files on changes.

**Files:**
- `pkg/dialog/types.go` - Dialog, State, Transition, Action structs
- `pkg/dialog/session.go` - Thread-safe session state with history
- `pkg/dialog/template.go` - Go template evaluation with caching
- `pkg/dialog/fsm.go` - State machine validation and transition evaluation
- `pkg/dialog/loader.go` - YAML loader with fsnotify hot-reload
- `pkg/dialog/engine.go` - Full dialog execution engine (for direct use)
- `internal/dialog/handler/dialog_handler.go` - Connect RPC handler with background loop

### Integration Service (Webhooks)

The integration service delivers events to external HTTP endpoints with enterprise-grade reliability features.

**Webhook lifecycle:**
1. Create a webhook via REST API or Connect RPC. A secret is auto-generated for HMAC signing.
2. Events flow through the NATS queue. The subscriber matches events to webhooks by `event_types`.
3. The deliverer POSTs the event payload with an `X-Voicetyped-Signature-256` HMAC header.
4. On failure: exponential backoff retries up to `max_retries`.
5. After all retries exhausted: event moves to dead letter queue for manual replay.
6. Circuit breaker trips after `failure_threshold` consecutive failures, auto-recovers after `reset_timeout`.

**Security:**
- HMAC-SHA256 signing on all deliveries
- SSRF protection: webhook URLs are validated against private/reserved IP ranges
- Secret rotation via `POST /api/v1/webhooks/{id}/rotate-secret`

**REST API endpoints:**
```
POST   /api/v1/webhooks                              # Create webhook
GET    /api/v1/webhooks                              # List webhooks
GET    /api/v1/webhooks/{id}                         # Get webhook
PUT    /api/v1/webhooks/{id}                         # Update webhook
DELETE /api/v1/webhooks/{id}                         # Delete webhook
POST   /api/v1/webhooks/{id}/rotate-secret           # Rotate secret
GET    /api/v1/webhooks/{id}/deliveries              # List deliveries
GET    /api/v1/webhooks/{id}/dead-letters            # List dead letters
POST   /api/v1/webhooks/{id}/dead-letters/{dlid}/replay  # Replay dead letter
POST   /api/v1/webhooks/{id}/test                    # Send test event
```

**Event types:** `call.started`, `call.terminated`, `speech.partial`, `speech.final`, `dtmf.received`, `state.transition`, `action.executed`, `hook.result`, `hook.error`, `tts.started`, `tts.completed`, `error`, `webhook.test`, `track.published`, `track.unpublished`, `speaker.changed`

**Files:**
- `pkg/webhook/models.go` - GORM models (WebhookEndpoint, DeliveryAttempt, DeadLetter)
- `pkg/webhook/repository.go` - PostgreSQL CRUD operations
- `pkg/webhook/signer.go` - HMAC-SHA256 sign/verify
- `pkg/webhook/circuit_breaker.go` - Per-endpoint circuit breaker (closed/open/half-open)
- `pkg/webhook/deliverer.go` - HTTP delivery with retries and exponential backoff
- `pkg/webhook/subscriber.go` - Queue consumer that routes events to webhooks
- `pkg/webhook/api/handler.go` - REST API handlers
- `pkg/webhook/api/dto.go` - Request/response DTOs
- `pkg/urlvalidation/ssrf.go` - SSRF protection (private IP blocking)

### Orchestrator

The orchestrator (`internal/runtime/orchestrator.go`) wires the three core services together for automated voice interactions. It runs when a peer joins a room in monolith mode.

**Pipeline:**
1. Subscribe to room audio via `media.SubscribeAudio`
2. Open a bidi transcription stream via `speech.Transcribe`
3. Start a dialog session via `dialog.StartDialog`
4. Pipe audio from media stream to speech stream (via worker pool)
5. Receive ASR results, forward final transcriptions to dialog via `dialog.SendEvent`
6. Execute returned action directives (e.g., `play_tts` -> synthesize and play audio)
7. On terminal state or disconnect, clean up all streams

The orchestrator uses Connect RPC clients, not direct struct references, so it works identically in monolith and polylith modes.

---

## API Reference

All services expose Connect RPC APIs. Use any Connect/gRPC client. The proto definitions in `proto/voicetyped/*/v1/` are the authoritative API reference.

### MediaService (`/voicetyped.media.v1.MediaService/`)

| RPC | Type | Description |
|-----|------|-------------|
| `CreateRoom` | Unary | Create a new room |
| `GetRoom` | Unary | Get room details |
| `ListRooms` | Unary | List all rooms |
| `CloseRoom` | Unary | Close a room |
| `JoinRoom` | Unary | Join a room (returns SDP answer) |
| `LeaveRoom` | Unary | Leave a room |
| `TrickleICE` | Unary | Send ICE candidate |
| `SubscribeTrack` | Unary | Subscribe to a peer's track |
| `UnsubscribeTrack` | Unary | Unsubscribe from a track |
| `UpdateSubscription` | Unary | Change subscription quality |
| `Renegotiate` | Unary | SDP renegotiation |
| `ActiveSpeakers` | Server stream | Stream active speaker updates |
| `SubscribeAudio` | Server stream | Tap room audio (for ASR) |
| `PlayAudio` | Client stream | Inject audio into room (for TTS) |

### SpeechService (`/voicetyped.speech.v1.SpeechService/`)

| RPC | Type | Description |
|-----|------|-------------|
| `Transcribe` | Bidi stream | Real-time speech-to-text |
| `Synthesize` | Server stream | Text-to-speech |
| `ListVoices` | Unary | Available TTS voices |
| `ListBackends` | Unary | Available ASR/TTS backends |
| `ListModels` | Unary | Available models per backend |

### DialogService (`/voicetyped.dialog.v1.DialogService/`)

| RPC | Type | Description |
|-----|------|-------------|
| `StartDialog` | Unary | Start a dialog session |
| `SendEvent` | Unary | Send speech/DTMF event |
| `GetSession` | Unary | Get session state |
| `EndDialog` | Unary | End a dialog session |
| `ListDialogs` | Unary | List available dialogs |

### IntegrationService (`/voicetyped.integration.v1.IntegrationService/`)

| RPC | Type | Description |
|-----|------|-------------|
| `CreateWebhook` | Unary | Create webhook endpoint |
| `GetWebhook` | Unary | Get webhook details |
| `ListWebhooks` | Unary | List webhooks |
| `UpdateWebhook` | Unary | Update webhook |
| `DeleteWebhook` | Unary | Delete webhook |
| `RotateSecret` | Unary | Rotate webhook secret |
| `TestWebhook` | Unary | Send test event |
| `StreamEvents` | Server stream | Live event stream |

---

## Writing Dialog Flows

Dialog flows are YAML files placed in the `DIALOG_DIR` directory (default `./dialogs/`). They are hot-reloaded when files change.

### Minimal Example

```yaml
name: hello-world
version: "1.0"
description: A minimal dialog

initial_state: greeting

states:
  greeting:
    on_enter:
      - type: play_tts
        params:
          text: "Hello! Say something."
    transitions:
      - event: speech
        target: respond

  respond:
    on_enter:
      - type: play_tts
        params:
          text: "You said something. Goodbye!"
    transitions:
      - event: tts_complete
        target: done

  done:
    terminal: true
```

### Dialog Structure

```yaml
name: my-dialog            # Unique identifier (used in StartDialog RPC)
version: "1.0"             # Version string
description: What it does  # Human-readable description

variables:                 # Default session variables
  caller_name: ""
  intent: ""

initial_state: greeting    # State to enter on StartDialog

states:
  state_name:
    on_enter:              # Actions to run when entering this state
      - type: play_tts
        params:
          text: "Hello"

    transitions:           # Rules for leaving this state
      - event: speech      # Trigger: "speech" or "dtmf"
        condition: '...'   # Optional Go template condition
        target: next_state # Target state name
        actions:           # Actions to run during transition
          - type: set_variable
            params:
              key: value

    timeout: "15s"         # Time before timeout triggers
    timeout_next: fallback # State to go to on timeout
    terminal: true/false   # If true, dialog ends when entering this state
```

### Available Actions

| Action | Params | Description |
|--------|--------|-------------|
| `play_tts` | `text` | Synthesize and play text to the caller |
| `call_hook` | `url`, `auth_type`, `auth_secret` | Call an external HTTP endpoint |
| `set_variable` | `key: value` pairs | Set session variables |
| `hangup` | _(none)_ | End the call |
| `play_audio` | _(placeholder)_ | Play pre-recorded audio |

### Template Expressions

Conditions and action params support Go templates:

```yaml
# Access session variables
condition: '{{ eq .Variables.intent "sales" }}'

# Access the last event (speech text or DTMF digit)
condition: '{{ eq (printf "%c" .Event) "1" }}'

# Access hook result data
condition: '{{ eq (index .Result "intent") "support" }}'

# Use variables in TTS text
params:
  text: "Hello {{ .Variables.caller_name }}, how can I help?"
```

**Template context:**
- `.Variables` - `map[string]string` of session variables
- `.Event` - The last event value (string for speech, rune for DTMF)
- `.Result` - `map[string]any` from the last hook response
- `.Session` - Full session object

### Hook Integration

The `call_hook` action POSTs a JSON payload to an external URL:

```json
{
  "session_id": "room1-peer1",
  "state": "understand",
  "event": "hello I need help with billing",
  "variables": {"caller_name": "John"},
  "transcript": "hello I need help with billing"
}
```

Expected response:
```json
{
  "variables": {"intent": "support"},
  "data": {"confidence": 0.95},
  "next_state": "support",
  "actions": [
    {"type": "play_tts", "params": {"text": "Routing to support..."}}
  ]
}
```

Auth types: `bearer` (Authorization header), `hmac` (X-Hook-Signature header), `none`.

---

## Speech Backend Guide

### Using Different Backends

Specify the backend per-request in the RPC, or set defaults via environment variables:

```bash
# Default backends
export ASR_BACKEND=whisper
export TTS_BACKEND=piper

# Or use cloud backends
export ASR_BACKEND=deepgram
export DEEPGRAM_API_KEY=your-key

export TTS_BACKEND=elevenlabs
export ELEVENLABS_API_KEY=your-key
```

Per-request override (in the `TranscribeConfig`):
```
backend: "openai"
model: "whisper-1"
```

### Backend Details

**Whisper (Local ASR)**
- Models: `ggml-base` (default), `ggml-small`, `ggml-medium`, `ggml-large-v3`
- Set `WHISPER_MODEL_PATH` or use `model` field to auto-derive path: `./models/{model}.bin`
- VAD-based utterance detection, batched transcription

**Piper (Local TTS)**
- Models: `en_US-amy-medium` (default)
- Requires the `piper` binary in PATH or set `PIPER_BINARY_PATH`
- Outputs 16kHz 16-bit mono PCM

**Deepgram (Cloud ASR)**
- Models: `nova-2` (default), `nova-2-general`, `nova-2-meeting`, `nova-2-phonecall`, `enhanced`, `base`
- Requires `DEEPGRAM_API_KEY`
- Sends raw PCM as `audio/l16;rate=16000;channels=1`

**Google Cloud (ASR + TTS)**
- ASR models: `latest_long` (default), `latest_short`, `chirp_2`, `chirp`
- TTS voices: `en-US-Neural2-A` (default), `en-US-Neural2-C`, `en-US-Studio-M`, `en-US-Studio-O`
- Requires `GOOGLE_API_KEY`
- Uses base64-encoded LINEAR16 audio

**ElevenLabs (Cloud TTS)**
- Models: `eleven_multilingual_v2` (default), `eleven_monolingual_v1`, `eleven_turbo_v2`
- Voices: Rachel, Domi, Bella, Antoni (with ElevenLabs voice IDs)
- Requires `ELEVENLABS_API_KEY`
- Returns raw 16kHz PCM via `pcm_16000` output format

**OpenAI-Compatible (ASR + TTS)**
- ASR models: `whisper-1` (default)
- TTS models: `tts-1` (default), `tts-1-hd`
- TTS voices: `alloy`, `echo`, `fable`, `onyx`, `nova`, `shimmer`
- Requires `OPENAI_API_KEY`
- Set `OPENAI_BASE_URL` to use any OpenAI-compatible API (LocalAI, self-hosted, etc.)
- ASR wraps PCM as WAV for upload; TTS downsamples 24kHz output to 16kHz

### Adding a New Backend

1. Create a package under `internal/speech/backends/mybackend/`
2. Implement `engine.ASREngine` and/or `engine.TTSEngine`
3. Register in `init()`:

```go
package mybackend

import (
    "github.com/voicetyped/voicetyped/internal/speech/engine"
    "github.com/voicetyped/voicetyped/internal/speech/registry"
)

func init() {
    registry.ASR.Register("mybackend", func(config map[string]string) (engine.ASREngine, error) {
        return &MyASR{apiKey: config["mybackend_api_key"]}, nil
    })
}

type MyASR struct { apiKey string }

func (m *MyASR) Transcribe(ctx context.Context, audio io.Reader) (<-chan engine.ASRResult, error) {
    // Use restutil.VADBatchTranscribe for batch-style APIs
    // Or implement streaming directly
    return restutil.VADBatchTranscribe(ctx, audio, m.transcribeUtterance), nil
}

func (m *MyASR) Models() []engine.ModelInfo {
    return []engine.ModelInfo{
        {ID: "default", DisplayName: "Default Model", IsDefault: true},
    }
}

func (m *MyASR) Close() error { return nil }
```

4. Add a blank import in `cmd/speech/main.go` and `cmd/voicetyped/main.go`:
```go
_ "github.com/voicetyped/voicetyped/internal/speech/backends/mybackend"
```

5. Add any API key config fields to `SpeechConfig` and `MonolithConfig` in `config/config.go`
6. Wire the config key into the `serviceConfig` map in both `main.go` files
7. Update tests in `internal/speech/handler/speech_handler_test.go`

---

## Deployment

### Scaling Considerations

**Media Service**: CPU-bound (RTP forwarding). Scale horizontally by partitioning rooms across nodes. Each room exists on exactly one node. Use a load balancer with sticky sessions (route by room ID).

**Speech Service**: Latency-sensitive. Local backends (Whisper, Piper) are CPU-bound. Cloud backends are I/O-bound. Scale by adding more instances behind a load balancer. Stateless - any instance can handle any request.

**Dialog Service**: Lightweight, memory-bound (sessions stored in memory). Sessions are pinned to instances. For HA, implement session persistence using the `dialog_sessions` database table.

**Integration Service**: I/O-bound (webhook delivery). Scale by adding more instances. The queue subscriber handles work distribution. Circuit breakers are per-instance - share state via Redis for multi-instance deployments.

### Database Setup

Run migrations in order:

```bash
psql $DATABASE_URL < migrations/0001/001_webhook_endpoints.sql
psql $DATABASE_URL < migrations/0001/002_delivery_attempts.sql
psql $DATABASE_URL < migrations/0001/003_dead_letters.sql
psql $DATABASE_URL < migrations/0002/001_rooms.sql
psql $DATABASE_URL < migrations/0002/002_sessions.sql
```

### Production Checklist

- [ ] Set `OIDC_ISSUER_URL` for authentication
- [ ] Configure a real NATS server (not `mem://`)
- [ ] Set `DATABASE_URL` for PostgreSQL
- [ ] Run database migrations
- [ ] Configure STUN/TURN servers for WebRTC NAT traversal
- [ ] Set API keys for cloud speech backends
- [ ] Place dialog YAML files in `DIALOG_DIR`
- [ ] Set up TLS termination (reverse proxy or load balancer)
- [ ] Configure OpenTelemetry exporter for observability

---

## Extending the System

### Adding a New Event Type

1. Add the constant to `pkg/events/types.go`:
```go
const MyNewEvent EventType = "my.new_event"
```

2. Add a payload struct:
```go
type MyNewEventData struct {
    SomeField string `json:"some_field"`
}
```

3. Emit from wherever it's relevant:
```go
publisher.Emit(ctx, events.MyNewEvent, sessionID, &events.MyNewEventData{
    SomeField: "value",
})
```

Webhooks automatically receive the new event type if subscribed to it.

### Adding a New Dialog Action

1. Add handling in `pkg/dialog/engine.go` `executeAction()`:
```go
case "my_action":
    param, _ := RenderParam(action.Params["param_name"], session)
    // Do something with param
```

2. Add handling in `internal/runtime/orchestrator.go` `executeActions()`:
```go
case "my_action":
    // Handle the action directive from the dialog handler
```

3. Use in dialog YAML:
```yaml
on_enter:
  - type: my_action
    params:
      param_name: "{{ .Variables.some_var }}"
```

### Adding a New Connect RPC Service

1. Define the service in `proto/voicetyped/myservice/v1/myservice.proto`
2. Run `buf generate`
3. Create handler in `internal/myservice/handler/`
4. Create standalone binary in `cmd/myservice/main.go`
5. Add to monolith in `cmd/voicetyped/main.go`:
```go
path, h = myservicev1connect.NewMyServiceHandler(myHandler, opts...)
mux.Handle(path, h)
```

### Pattern: Connect RPC Handler

All handlers follow the same pattern:

```go
package handler

import (
    "connectrpc.com/connect"
    myv1 "github.com/voicetyped/voicetyped/gen/voicetyped/myservice/v1"
    "github.com/voicetyped/voicetyped/gen/voicetyped/myservice/v1/myservicev1connect"
)

var _ myservicev1connect.MyServiceHandler = (*MyHandler)(nil)

type MyHandler struct { /* dependencies */ }

func NewMyHandler(/* deps */) *MyHandler {
    return &MyHandler{ /* ... */ }
}

// Unary RPC
func (h *MyHandler) MyMethod(ctx context.Context, req *connect.Request[myv1.MyRequest]) (*connect.Response[myv1.MyResponse], error) {
    return connect.NewResponse(&myv1.MyResponse{ /* ... */ }), nil
}

// Server streaming RPC
func (h *MyHandler) MyStream(ctx context.Context, req *connect.Request[myv1.MyRequest], stream *connect.ServerStream[myv1.MyResponse]) error {
    return stream.Send(&myv1.MyResponse{ /* ... */ })
}

// Bidi streaming RPC
func (h *MyHandler) MyBidi(ctx context.Context, stream *connect.BidiStream[myv1.MyRequest, myv1.MyResponse]) error {
    msg, err := stream.Receive()  // receive
    stream.Send(&myv1.MyResponse{ /* ... */ })  // send
    return nil
}
```

### Pattern: Frame Service Setup

```go
ctx, srv := frame.NewService(
    frame.WithConfig(&cfg),                          // Load config struct
    frame.WithName("service-name"),                  // Service name for telemetry
    frame.WithRegisterServerOauth2Client(),          // Enable OIDC auth
    frame.WithDatastore(),                           // Enable PostgreSQL
    frame.WithRegisterPublisher(ref, url),           // Enable queue publishing
    frame.WithWorkerPoolOptions(...),                // Configure worker pool
)
defer srv.Stop(ctx)

pool, _ := srv.WorkManager().GetPool()              // Get worker pool
auth := srv.SecurityManager().GetAuthenticator(ctx)  // Get JWT authenticator

mux := http.NewServeMux()
opts, _ := connectutil.AuthenticatedOptions(ctx, auth)  // Auth interceptors
path, h := myv1connect.NewMyServiceHandler(handler, opts...)
mux.Handle(path, h)

srv.Init(ctx, frame.WithHTTPHandler(connectutil.H2CHandler(mux)))
srv.Run(ctx, "")
```

---

## Database Migrations

Migrations are plain SQL files in `migrations/`. They are organized by version:

```
migrations/
├── 0001/                  # Webhook infrastructure
│   ├── 001_webhook_endpoints.sql
│   ├── 002_delivery_attempts.sql
│   └── 003_dead_letters.sql
└── 0002/                  # Media & Dialog
    ├── 001_rooms.sql
    └── 002_sessions.sql
```

All tables follow the frame `BaseModel` pattern with standard columns: `id`, `created_at`, `modified_at`, `version`, `tenant_id`, `partition_id`, `access_id`, `deleted_at`.

Run them in numeric order. They are idempotent (`CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`).

---

## Testing

```bash
# All tests
go test ./...

# With race detector
go test -race ./...

# Specific package
go test ./internal/speech/handler/

# Verbose
go test -v ./internal/speech/handler/
```

**Test patterns used in this codebase:**
- Handler tests use `httptest.NewServer` with Connect RPC clients
- Backends register via init() - import them in test files with `_ "path/to/backend"`
- The `NewSpeechHandler` accepts `nil` for worker pool and service config in tests
- SSRF validation can be disabled in tests with `urlvalidation.AllowPrivateIPs()`
- Hook executor accepts `urlvalidation.Option` for test flexibility

---

## Conventions and Guidelines

### Code Organization

- `internal/` - Private to this module. Each service has `handler/` for the RPC layer.
- `pkg/` - Importable by external code. Core business logic lives here.
- `gen/` - Generated code. Never edit.
- `proto/` - Source of truth for APIs.
- `cmd/` - Binary entry points. Minimal logic, just wiring.
- `config/` - Configuration structs only.

### Naming

- Packages are lowercase, single-word where possible
- Handler structs are named `{Service}Handler`
- Test files are `{file}_test.go` in the same package
- Proto packages follow `voicetyped.{service}.v1`
- Generated Go packages are `{service}v1` with connect suffix `{service}v1connect`

### Error Handling

- Connect RPC handlers return `connect.NewError(connect.Code*, err)` for typed errors
- Internal errors use `fmt.Errorf` with `%w` wrapping
- Hook errors are non-fatal by default (logged, not propagated)
- Circuit breaker failures return immediately (no blocking)

### Streaming Patterns

- **Bidi streaming** (Transcribe): First message is config, subsequent messages are data
- **Server streaming** (Synthesize, ActiveSpeakers): Request starts the stream, server sends until done
- **Client streaming** (PlayAudio): Client sends audio frames, server responds when done
- Always use `connectutil.H2CHandler()` for HTTP/2 streaming support

### Config Pattern

- Use `env:` tags with `envDefault:` for environment variable loading
- Frame handles loading via `config.LoadWithOIDC[T](ctx)`
- Service-specific configs are separate structs; `MonolithConfig` merges them all
- API keys use backend-specific config map keys (e.g., `deepgram_api_key`, not `api_key`)

### Worker Pool Usage

- All goroutine work should go through the frame worker pool when available
- Pattern: `if h.pool != nil { h.pool.Submit(ctx, fn) } else { go fn() }`
- Tests pass `nil` for the pool to use plain goroutines

### Backend Registration Pattern

- Each backend is a separate package under `internal/speech/backends/`
- Registration happens in `init()` via `registry.ASR.Register()` or `registry.TTS.Register()`
- Backends are activated by blank imports: `_ "path/to/backend"`
- Factory functions receive `map[string]string` config and return the engine interface
- Backend-specific config keys prevent collisions in the shared config map
