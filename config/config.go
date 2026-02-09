package config

import (
	"strings"

	"github.com/pion/webrtc/v4"
	"github.com/pitabwire/frame/config"
)

// MediaConfig holds configuration for the media service.
type MediaConfig struct {
	config.ConfigurationDefault
	STUNServers                string `envDefault:"stun:stun.l.google.com:19302" env:"STUN_SERVERS"`
	TURNServers                string `envDefault:""                              env:"TURN_SERVERS"`
	TURNUsername               string `envDefault:""                              env:"TURN_USERNAME"`
	TURNPassword               string `envDefault:""                              env:"TURN_PASSWORD"`
	MaxRoomsPerNode            int    `envDefault:"100"                           env:"MAX_ROOMS_PER_NODE"`
	SIPListenAddr              string `envDefault:"0.0.0.0:5060"                 env:"SIP_LISTEN_ADDR"`
	SIPTransport               string `envDefault:"udp"                           env:"SIP_TRANSPORT"`
	DefaultMaxPublishers       int    `envDefault:"100"                           env:"MAX_PUBLISHERS_PER_ROOM"`
	SimulcastEnabled           bool   `envDefault:"true"                          env:"SIMULCAST_ENABLED"`
	SVCEnabled                 bool   `envDefault:"true"                          env:"SVC_ENABLED"`
	SpeakerDetectorIntervalMs  int    `envDefault:"500"                           env:"SPEAKER_DETECTOR_INTERVAL_MS"`
	SpeakerDetectorThreshold   int    `envDefault:"30"                            env:"SPEAKER_DETECTOR_THRESHOLD"`
	E2EEDefaultRequired        bool   `envDefault:"false"                         env:"E2EE_DEFAULT_REQUIRED"`
	DefaultAutoSubscribeAudio  bool   `envDefault:"true"                          env:"AUTO_SUBSCRIBE_AUDIO"`
}

// WebRTCConfig builds a webrtc.Configuration from the STUN/TURN settings.
func (c *MediaConfig) WebRTCConfig() webrtc.Configuration {
	return buildWebRTCConfig(c.STUNServers, c.TURNServers, c.TURNUsername, c.TURNPassword)
}

// SpeechConfig holds configuration for the speech service.
type SpeechConfig struct {
	config.ConfigurationDefault
	DefaultASRBackend string `envDefault:"whisper"                          env:"ASR_BACKEND"`
	DefaultTTSBackend string `envDefault:"piper"                            env:"TTS_BACKEND"`
	WhisperModelPath  string `envDefault:"./models/ggml-base.bin"           env:"WHISPER_MODEL_PATH"`
	WhisperPoolSize   int    `envDefault:"2"                                env:"WHISPER_POOL_SIZE"`
	PiperModelPath    string `envDefault:"./models/en_US-amy-medium.onnx"   env:"PIPER_MODEL_PATH"`
	PiperBinaryPath   string `envDefault:"piper"                            env:"PIPER_BINARY_PATH"`
	DeepgramAPIKey    string `envDefault:""                                 env:"DEEPGRAM_API_KEY"`
	GoogleAPIKey      string `envDefault:""                                 env:"GOOGLE_API_KEY"`
	ElevenLabsAPIKey  string `envDefault:""                                 env:"ELEVENLABS_API_KEY"`
	OpenAIAPIKey      string `envDefault:""                                 env:"OPENAI_API_KEY"`
	OpenAIBaseURL     string `envDefault:"https://api.openai.com/v1"        env:"OPENAI_BASE_URL"`
}

// DialogConfig holds configuration for the dialog service.
type DialogConfig struct {
	config.ConfigurationDefault
	DialogDir string `envDefault:"./dialogs" env:"DIALOG_DIR"`
}

// IntegrationConfig holds configuration for the integration service.
type IntegrationConfig struct {
	config.ConfigurationDefault
	WebhookWorkers    int `envDefault:"16"  env:"WEBHOOK_WORKERS"`
	WebhookMaxRetries int `envDefault:"5"   env:"WEBHOOK_MAX_RETRIES"`
	WebhookTimeoutSec int `envDefault:"10"  env:"WEBHOOK_TIMEOUT_SEC"`
	WebhookBackoffSec int `envDefault:"1"   env:"WEBHOOK_BACKOFF_INITIAL_SEC"`
	WebhookBackoffMax int `envDefault:"300" env:"WEBHOOK_BACKOFF_MAX_SEC"`
	CBFailThreshold   int `envDefault:"5"   env:"CB_FAILURE_THRESHOLD"`
	CBResetTimeoutSec int `envDefault:"60"  env:"CB_RESET_TIMEOUT_SEC"`
}

// MonolithConfig combines all service configs for the single-binary monolith.
type MonolithConfig struct {
	config.ConfigurationDefault

	// Media
	STUNServers                string `envDefault:"stun:stun.l.google.com:19302" env:"STUN_SERVERS"`
	TURNServers                string `envDefault:""                              env:"TURN_SERVERS"`
	TURNUsername               string `envDefault:""                              env:"TURN_USERNAME"`
	TURNPassword               string `envDefault:""                              env:"TURN_PASSWORD"`
	MaxRoomsPerNode            int    `envDefault:"100"                           env:"MAX_ROOMS_PER_NODE"`
	SIPListenAddr              string `envDefault:"0.0.0.0:5060"                 env:"SIP_LISTEN_ADDR"`
	SIPTransport               string `envDefault:"udp"                           env:"SIP_TRANSPORT"`
	DefaultMaxPublishers       int    `envDefault:"100"                           env:"MAX_PUBLISHERS_PER_ROOM"`
	SimulcastEnabled           bool   `envDefault:"true"                          env:"SIMULCAST_ENABLED"`
	SVCEnabled                 bool   `envDefault:"true"                          env:"SVC_ENABLED"`
	SpeakerDetectorIntervalMs  int    `envDefault:"500"                           env:"SPEAKER_DETECTOR_INTERVAL_MS"`
	SpeakerDetectorThreshold   int    `envDefault:"30"                            env:"SPEAKER_DETECTOR_THRESHOLD"`
	E2EEDefaultRequired        bool   `envDefault:"false"                         env:"E2EE_DEFAULT_REQUIRED"`
	DefaultAutoSubscribeAudio  bool   `envDefault:"true"                          env:"AUTO_SUBSCRIBE_AUDIO"`

	// Speech
	DefaultASRBackend string `envDefault:"whisper"                          env:"ASR_BACKEND"`
	DefaultTTSBackend string `envDefault:"piper"                            env:"TTS_BACKEND"`
	WhisperModelPath  string `envDefault:"./models/ggml-base.bin"           env:"WHISPER_MODEL_PATH"`
	WhisperPoolSize   int    `envDefault:"2"                                env:"WHISPER_POOL_SIZE"`
	PiperModelPath    string `envDefault:"./models/en_US-amy-medium.onnx"   env:"PIPER_MODEL_PATH"`
	PiperBinaryPath   string `envDefault:"piper"                            env:"PIPER_BINARY_PATH"`
	DeepgramAPIKey    string `envDefault:""                                 env:"DEEPGRAM_API_KEY"`
	GoogleAPIKey      string `envDefault:""                                 env:"GOOGLE_API_KEY"`
	ElevenLabsAPIKey  string `envDefault:""                                 env:"ELEVENLABS_API_KEY"`
	OpenAIAPIKey      string `envDefault:""                                 env:"OPENAI_API_KEY"`
	OpenAIBaseURL     string `envDefault:"https://api.openai.com/v1"        env:"OPENAI_BASE_URL"`

	// Dialog
	DialogDir     string `envDefault:"./dialogs" env:"DIALOG_DIR"`
	DefaultDialog string `envDefault:"example"   env:"DEFAULT_DIALOG"`

	// Webhooks
	WebhookWorkers    int `envDefault:"16"  env:"WEBHOOK_WORKERS"`
	WebhookMaxRetries int `envDefault:"5"   env:"WEBHOOK_MAX_RETRIES"`
	WebhookTimeoutSec int `envDefault:"10"  env:"WEBHOOK_TIMEOUT_SEC"`
	WebhookBackoffSec int `envDefault:"1"   env:"WEBHOOK_BACKOFF_INITIAL_SEC"`
	WebhookBackoffMax int `envDefault:"300" env:"WEBHOOK_BACKOFF_MAX_SEC"`
	CBFailThreshold   int `envDefault:"5"   env:"CB_FAILURE_THRESHOLD"`
	CBResetTimeoutSec int `envDefault:"60"  env:"CB_RESET_TIMEOUT_SEC"`

	// Service endpoint URLs (for polylith client connections).
	MediaServiceURL       string `envDefault:"" env:"MEDIA_SERVICE_URL"`
	SpeechServiceURL      string `envDefault:"" env:"SPEECH_SERVICE_URL"`
	DialogServiceURL      string `envDefault:"" env:"DIALOG_SERVICE_URL"`
	IntegrationServiceURL string `envDefault:"" env:"INTEGRATION_SERVICE_URL"`
}

// WebRTCConfig builds a webrtc.Configuration from the STUN/TURN settings.
func (c *MonolithConfig) WebRTCConfig() webrtc.Configuration {
	return buildWebRTCConfig(c.STUNServers, c.TURNServers, c.TURNUsername, c.TURNPassword)
}

// buildWebRTCConfig creates a webrtc.Configuration from STUN/TURN server strings.
func buildWebRTCConfig(stunServers, turnServers, turnUsername, turnPassword string) webrtc.Configuration {
	var iceServers []webrtc.ICEServer
	if stunServers != "" {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs: strings.Split(stunServers, ","),
		})
	}
	if turnServers != "" {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:           strings.Split(turnServers, ","),
			Username:       turnUsername,
			Credential:     turnPassword,
			CredentialType: webrtc.ICECredentialTypePassword,
		})
	}
	return webrtc.Configuration{ICEServers: iceServers}
}
