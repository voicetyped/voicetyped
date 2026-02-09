package sfu

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pitabwire/frame/workerpool"
	"github.com/rs/xid"
)

// SFUConfig holds configuration for the SFU.
type SFUConfig struct {
	WebRTCConfig              webrtc.Configuration
	SimulcastEnabled          bool
	SVCEnabled                bool
	SpeakerDetectorIntervalMs int
	SpeakerDetectorThreshold  int
	DefaultMaxPeers           int
	DefaultMaxPublishers      int
	DefaultAutoSubscribeAudio bool
	E2EEDefaultRequired       bool
}

// SFUStats reports aggregate SFU metrics.
type SFUStats struct {
	RoomCount         int
	PeerCount         int
	TrackCount        int
	AudioTrackCount   int
	VideoTrackCount   int
	SubscriptionCount int
}

// SFU is the top-level Selective Forwarding Unit manager.
type SFU struct {
	mu     sync.RWMutex
	rooms  map[string]*Room
	config SFUConfig
	pool   workerpool.WorkerPool
	api    *webrtc.API
}

// New creates a new SFU with the given configuration and worker pool.
func New(cfg SFUConfig, pool workerpool.WorkerPool) *SFU {
	me := &webrtc.MediaEngine{}

	// Register audio codecs.
	_ = me.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    "audio/opus",
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio)

	// Register video codecs.
	for _, codec := range []webrtc.RTPCodecParameters{
		{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:    "video/VP8",
				ClockRate:   90000,
				SDPFmtpLine: "",
			},
			PayloadType: 96,
		},
		{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:    "video/VP9",
				ClockRate:   90000,
				SDPFmtpLine: "profile-id=0",
			},
			PayloadType: 98,
		},
		{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:    "video/H264",
				ClockRate:   90000,
				SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
			},
			PayloadType: 102,
		},
		{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:  "video/AV1",
				ClockRate: 90000,
			},
			PayloadType: 35,
		},
	} {
		_ = me.RegisterCodec(codec, webrtc.RTPCodecTypeVideo)
	}

	// Register header extensions for speaker detection and simulcast.
	_ = me.RegisterHeaderExtension(
		webrtc.RTPHeaderExtensionCapability{URI: "urn:ietf:params:rtp-hdrext:ssrc-audio-level"},
		webrtc.RTPCodecTypeAudio,
	)
	_ = me.RegisterHeaderExtension(
		webrtc.RTPHeaderExtensionCapability{URI: "urn:ietf:params:rtp-hdrext:sdes:mid"},
		webrtc.RTPCodecTypeVideo,
	)
	_ = me.RegisterHeaderExtension(
		webrtc.RTPHeaderExtensionCapability{URI: "urn:ietf:params:rtp-hdrext:sdes:rtp-stream-id"},
		webrtc.RTPCodecTypeVideo,
	)

	api := webrtc.NewAPI(webrtc.WithMediaEngine(me))

	if cfg.DefaultMaxPeers == 0 {
		cfg.DefaultMaxPeers = 1000
	}
	if cfg.DefaultMaxPublishers == 0 {
		cfg.DefaultMaxPublishers = 100
	}

	return &SFU{
		rooms:  make(map[string]*Room),
		config: cfg,
		pool:   pool,
		api:    api,
	}
}

// Config returns the WebRTC configuration.
func (s *SFU) Config() webrtc.Configuration {
	return s.config.WebRTCConfig
}

// SFUConfig returns the full SFU configuration.
func (s *SFU) SFUCfg() SFUConfig {
	return s.config
}

// API returns the shared webrtc.API instance (with custom MediaEngine).
func (s *SFU) API() *webrtc.API {
	return s.api
}

// CreateRoom creates a new room with the given settings.
func (s *SFU) CreateRoom(id string, maxPeers int, metadata map[string]string) (*Room, error) {
	return s.CreateRoomWithConfig(id, maxPeers, metadata, RoomConfig{})
}

// RoomConfig holds optional room-level configuration.
type RoomConfig struct {
	MaxPublishers      int
	E2EERequired       bool
	AutoSubscribeAudio *bool // nil means use SFU default
}

// CreateRoomWithConfig creates a room with additional configuration.
func (s *SFU) CreateRoomWithConfig(id string, maxPeers int, metadata map[string]string, rc RoomConfig) (*Room, error) {
	if id == "" {
		id = xid.New().String()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[id]; exists {
		return nil, fmt.Errorf("room %q already exists", id)
	}

	if maxPeers <= 0 {
		maxPeers = s.config.DefaultMaxPeers
	}
	maxPublishers := rc.MaxPublishers
	if maxPublishers <= 0 {
		maxPublishers = s.config.DefaultMaxPublishers
	}
	autoSubscribeAudio := s.config.DefaultAutoSubscribeAudio
	if rc.AutoSubscribeAudio != nil {
		autoSubscribeAudio = *rc.AutoSubscribeAudio
	}
	e2eeRequired := rc.E2EERequired || s.config.E2EEDefaultRequired

	threshold := uint8(s.config.SpeakerDetectorThreshold)
	if threshold == 0 {
		threshold = 30
	}
	interval := time.Duration(s.config.SpeakerDetectorIntervalMs) * time.Millisecond
	if interval == 0 {
		interval = 500 * time.Millisecond
	}

	room := NewRoom(id, maxPeers, metadata, s.pool, s.api, RoomOptions{
		MaxPublishers:      maxPublishers,
		AutoSubscribeAudio: autoSubscribeAudio,
		E2EERequired:       e2eeRequired,
		SpeakerThreshold:   threshold,
		SpeakerInterval:    interval,
	})
	s.rooms[id] = room
	return room, nil
}

// GetRoom returns a room by ID.
func (s *SFU) GetRoom(id string) (*Room, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	room, ok := s.rooms[id]
	return room, ok
}

// CloseRoom closes and removes a room.
func (s *SFU) CloseRoom(id string) error {
	s.mu.Lock()
	room, ok := s.rooms[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("room %q not found", id)
	}
	delete(s.rooms, id)
	s.mu.Unlock()

	room.Close()
	return nil
}

// ListRooms returns all active rooms.
func (s *SFU) ListRooms() []*Room {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	return rooms
}

// Stats returns aggregate SFU statistics.
func (s *SFU) Stats() SFUStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := SFUStats{RoomCount: len(s.rooms)}
	for _, r := range s.rooms {
		peers := r.Peers()
		stats.PeerCount += len(peers)

		r.mu.RLock()
		for _, pt := range r.publisherTracks {
			stats.TrackCount++
			if pt.kind == webrtc.RTPCodecTypeAudio {
				stats.AudioTrackCount++
			} else {
				stats.VideoTrackCount++
			}
			pt.mu.RLock()
			stats.SubscriptionCount += len(pt.subscribers)
			pt.mu.RUnlock()
		}
		r.mu.RUnlock()
	}
	return stats
}

// Close gracefully closes all rooms.
func (s *SFU) Close() {
	s.mu.Lock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.rooms = make(map[string]*Room)
	s.mu.Unlock()

	for _, r := range rooms {
		r.Close()
	}
}
