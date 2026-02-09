package sfu

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pitabwire/frame/workerpool"
)

// AudioTapFunc is a callback for audio data from a peer.
// The codec field indicates the encoding of the payload (e.g., "audio/opus").
// The frame contains the raw RTP payload (codec-encoded, not decoded PCM).
type AudioTapFunc func(peerID string, frame []byte, codec string)

// RoomOptions holds optional room-level configuration set at creation time.
type RoomOptions struct {
	MaxPublishers      int
	AutoSubscribeAudio bool
	E2EERequired       bool
	SpeakerThreshold   uint8
	SpeakerInterval    time.Duration
}

// Room holds a set of peers and manages track routing between them.
type Room struct {
	mu                 sync.RWMutex
	id                 string
	peers              map[string]*Peer
	maxPeers           int
	maxPublishers      int
	metadata           map[string]string
	closed             bool
	createdAt          time.Time
	audioTaps          map[string]AudioTapFunc
	publisherTracks    map[string]*PublisherTrack // trackID -> PublisherTrack
	speakerDetector    *SpeakerDetector
	autoSubscribeAudio bool
	e2eeRequired       bool
	pool               workerpool.WorkerPool
	api                *webrtc.API
}

// NewRoom creates a new room.
func NewRoom(id string, maxPeers int, metadata map[string]string, pool workerpool.WorkerPool, api *webrtc.API, opts RoomOptions) *Room {
	if maxPeers <= 0 {
		maxPeers = 1000
	}
	if metadata == nil {
		metadata = make(map[string]string)
	}
	maxPublishers := opts.MaxPublishers
	if maxPublishers <= 0 {
		maxPublishers = 100
	}

	sd := NewSpeakerDetector(opts.SpeakerThreshold, opts.SpeakerInterval, pool)

	return &Room{
		id:                 id,
		peers:              make(map[string]*Peer),
		maxPeers:           maxPeers,
		maxPublishers:      maxPublishers,
		metadata:           metadata,
		createdAt:          time.Now(),
		audioTaps:          make(map[string]AudioTapFunc),
		publisherTracks:    make(map[string]*PublisherTrack),
		speakerDetector:    sd,
		autoSubscribeAudio: opts.AutoSubscribeAudio,
		e2eeRequired:       opts.E2EERequired,
		pool:               pool,
		api:                api,
	}
}

// ID returns the room's identifier.
func (r *Room) ID() string { return r.id }

// MaxPeers returns the maximum peer limit.
func (r *Room) MaxPeers() int { return r.maxPeers }

// Metadata returns the room's metadata.
func (r *Room) Metadata() map[string]string { return r.metadata }

// CreatedAt returns the room creation time.
func (r *Room) CreatedAt() time.Time { return r.createdAt }

// E2EERequired returns whether the room requires E2EE.
func (r *Room) E2EERequired() bool { return r.e2eeRequired }

// AddPeer adds a peer to the room, validating the max peer limit.
// If autoSubscribeAudio is true, the new peer is auto-subscribed to all
// existing audio publisher tracks.
// Returns the list of available publisher tracks for the client.
func (r *Room) AddPeer(p *Peer) ([]*PublisherTrack, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, fmt.Errorf("room %q is closed", r.id)
	}
	if len(r.peers) >= r.maxPeers {
		return nil, fmt.Errorf("room %q is full (%d/%d peers)", r.id, len(r.peers), r.maxPeers)
	}

	// Validate E2EE if required.
	if r.e2eeRequired {
		if err := ValidateE2EERoom(true, p.encryption); err != nil {
			return nil, err
		}
	}

	r.peers[p.ID()] = p

	// Start the speaker detector if this is the first peer.
	if len(r.peers) == 1 && r.speakerDetector != nil {
		r.speakerDetector.Start(context.Background())
	}

	// Collect available tracks.
	available := make([]*PublisherTrack, 0, len(r.publisherTracks))
	for _, pt := range r.publisherTracks {
		available = append(available, pt)
	}

	// Auto-subscribe the new peer to all existing audio tracks if configured.
	if r.autoSubscribeAudio && p.peerConfig.AutoSubscribeAudio {
		for _, pt := range r.publisherTracks {
			if pt.kind == webrtc.RTPCodecTypeAudio && pt.publisher.ID() != p.ID() {
				// Best effort auto-subscribe.
				_, _ = pt.Subscribe(p, QualityHigh, -1, -1)
			}
		}
	}

	return available, nil
}

// RemovePeer removes a peer from the room and cleans up its tracks.
func (r *Room) RemovePeer(peerID string) {
	r.mu.Lock()
	peer, ok := r.peers[peerID]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.peers, peerID)

	// Unsubscribe this peer from all publisher tracks.
	for _, pt := range r.publisherTracks {
		pt.Unsubscribe(peerID)
	}

	// Close and remove any publisher tracks owned by this peer.
	for trackID, pt := range r.publisherTracks {
		if pt.publisher.ID() == peerID {
			pt.Close()
			delete(r.publisherTracks, trackID)
		}
	}
	r.mu.Unlock()

	// Remove from speaker detector.
	if r.speakerDetector != nil {
		r.speakerDetector.RemovePeer(peerID)
	}

	peer.Close()
}

// GetPeer returns a peer by ID.
func (r *Room) GetPeer(peerID string) (*Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.peers[peerID]
	return p, ok
}

// Peers returns all peers in the room.
func (r *Room) Peers() []*Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, p := range r.peers {
		peers = append(peers, p)
	}
	return peers
}

// PeerCount returns the number of peers.
func (r *Room) PeerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.peers)
}

// IsClosed returns whether the room is closed.
func (r *Room) IsClosed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.closed
}

// Close closes all peers and marks the room closed.
func (r *Room) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	peers := make([]*Peer, 0, len(r.peers))
	for _, p := range r.peers {
		peers = append(peers, p)
	}
	r.peers = make(map[string]*Peer)
	r.audioTaps = make(map[string]AudioTapFunc)

	// Close all publisher tracks.
	for _, pt := range r.publisherTracks {
		pt.Close()
	}
	r.publisherTracks = make(map[string]*PublisherTrack)

	if r.speakerDetector != nil {
		r.speakerDetector.Close()
	}
	r.mu.Unlock()

	for _, p := range peers {
		p.Close()
	}
}

// AddAudioTap registers a callback for audio data from peers.
// The id is used for later removal via RemoveAudioTap.
// The tap is registered on all existing and future audio publisher tracks.
func (r *Room) AddAudioTap(id string, fn AudioTapFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.audioTaps[id] = fn
	// Register on all existing audio publisher tracks.
	for _, pt := range r.publisherTracks {
		if pt.kind == webrtc.RTPCodecTypeAudio {
			pt.AddAudioTap(id, fn)
		}
	}
}

// RemoveAudioTap removes a previously registered audio tap by ID.
func (r *Room) RemoveAudioTap(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.audioTaps, id)
	for _, pt := range r.publisherTracks {
		if pt.kind == webrtc.RTPCodecTypeAudio {
			pt.RemoveAudioTap(id)
		}
	}
}

// InjectAudio sends raw audio data to all audio taps in the room.
// This is used for TTS playback — the audio data bypasses the WebRTC
// track system and goes directly to audio taps (which feed the
// orchestrator's ASR pipeline or other consumers).
func (r *Room) InjectAudio(peerID string, data []byte, codec string) {
	r.mu.RLock()
	taps := make([]AudioTapFunc, 0, len(r.audioTaps))
	for _, tap := range r.audioTaps {
		taps = append(taps, tap)
	}
	r.mu.RUnlock()

	for _, tap := range taps {
		tap(peerID, data, codec)
	}
}

// RegisterPublisherTrack registers a track published by a peer.
// Groups simulcast layers under the same logical track by track.ID().
// Auto-subscribes peers if autoSubscribeAudio is enabled for audio tracks.
func (r *Room) RegisterPublisherTrack(publisher *Peer, remote *webrtc.TrackRemote) *PublisherTrack {
	trackID := remote.ID()

	r.mu.Lock()
	existing, ok := r.publisherTracks[trackID]
	if ok && remote.RID() != "" {
		// This is an additional simulcast layer for an existing track.
		r.mu.Unlock()
		existing.AddLayer(remote)
		return existing
	}

	pt := NewPublisherTrack(publisher.Context(), publisher, remote, r.pool, r.speakerDetector, publisher.encryption)
	r.publisherTracks[trackID] = pt

	// Register existing room-level audio taps on this track.
	if pt.kind == webrtc.RTPCodecTypeAudio {
		for id, tap := range r.audioTaps {
			pt.AddAudioTap(id, tap)
		}
	}

	// Auto-subscribe all peers to this audio track if configured.
	if r.autoSubscribeAudio && pt.kind == webrtc.RTPCodecTypeAudio {
		for _, peer := range r.peers {
			if peer.ID() == publisher.ID() {
				continue
			}
			if !peer.peerConfig.AutoSubscribeAudio {
				continue
			}
			_, _ = pt.Subscribe(peer, QualityHigh, -1, -1)
		}
	}
	r.mu.Unlock()

	return pt
}

// Subscribe creates a subscription for a peer to a specific track.
func (r *Room) Subscribe(subscriberPeerID, trackID string, quality VideoQualityLevel, maxTemporal, maxSpatial int) (*Subscription, error) {
	r.mu.RLock()
	pt, ok := r.publisherTracks[trackID]
	peer, peerOK := r.peers[subscriberPeerID]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("track %q not found", trackID)
	}
	if !peerOK {
		return nil, fmt.Errorf("peer %q not found", subscriberPeerID)
	}

	return pt.Subscribe(peer, quality, maxTemporal, maxSpatial)
}

// Unsubscribe removes a peer's subscription to a track.
func (r *Room) Unsubscribe(subscriberPeerID, trackID string) error {
	r.mu.RLock()
	pt, ok := r.publisherTracks[trackID]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("track %q not found", trackID)
	}

	pt.Unsubscribe(subscriberPeerID)
	return nil
}

// UpdateSubscription updates an existing subscription's settings.
func (r *Room) UpdateSubscription(subscriberPeerID, trackID string, quality VideoQualityLevel, maxTemporal, maxSpatial int, paused bool) error {
	r.mu.RLock()
	pt, ok := r.publisherTracks[trackID]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("track %q not found", trackID)
	}

	return pt.UpdateSubscription(subscriberPeerID, quality, maxTemporal, maxSpatial, paused)
}

// ListPublisherTracks returns all published tracks in the room.
func (r *Room) ListPublisherTracks() []*PublisherTrack {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tracks := make([]*PublisherTrack, 0, len(r.publisherTracks))
	for _, pt := range r.publisherTracks {
		tracks = append(tracks, pt)
	}
	return tracks
}

// AddSpeakerListener registers a callback for active speaker updates.
func (r *Room) AddSpeakerListener(id string, fn SpeakerListener) {
	if r.speakerDetector != nil {
		r.speakerDetector.AddListener(id, fn)
	}
}

// RemoveSpeakerListener removes a speaker listener.
func (r *Room) RemoveSpeakerListener(id string) {
	if r.speakerDetector != nil {
		r.speakerDetector.RemoveListener(id)
	}
}

// PublisherCount returns the number of peers that are currently publishing.
func (r *Room) PublisherCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	publishers := make(map[string]struct{})
	for _, pt := range r.publisherTracks {
		publishers[pt.publisher.ID()] = struct{}{}
	}
	return len(publishers)
}
