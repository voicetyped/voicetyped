package sfu

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/pion/webrtc/v4"
	"github.com/rs/xid"
)

// PeerConfig configures a peer's media capabilities.
type PeerConfig struct {
	PublishAudio       bool
	PublishVideo       bool
	Simulcast          bool
	Encryption         *EncryptionInfo
	AutoSubscribeAudio bool
}

// DefaultPeerConfig returns a PeerConfig with sensible defaults (audio-only, auto-subscribe).
func DefaultPeerConfig() PeerConfig {
	return PeerConfig{
		PublishAudio:       true,
		PublishVideo:       false,
		AutoSubscribeAudio: true,
	}
}

// PeerInfo holds metadata about a peer for external reporting.
type PeerInfo struct {
	ID               string
	State            string
	Metadata         map[string]string
	PublishedTracks  int
	SubscribedTracks int
	Tracks           []PublisherTrackInfo
	Subscriptions    []SubscriptionDetail
}

// Peer wraps a WebRTC PeerConnection in an SFU room.
type Peer struct {
	mu              sync.Mutex
	id              string
	pc              *webrtc.PeerConnection
	room            *Room
	ctx             context.Context
	cancel          context.CancelFunc
	metadata        map[string]string
	publishedTracks map[string]*webrtc.TrackRemote
	downTracks      map[string]*DownTrack
	state           string
	closeOnce       sync.Once
	peerConfig      PeerConfig
	encryption      *EncryptionInfo
}

// NewPeer creates a peer with a new PeerConnection wired to the room.
func NewPeer(parentCtx context.Context, id string, room *Room, api *webrtc.API, config webrtc.Configuration, metadata map[string]string, pcfg PeerConfig) (*Peer, error) {
	if id == "" {
		id = xid.New().String()
	}

	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("create peer connection: %w", err)
	}

	ctx, cancel := context.WithCancel(parentCtx)

	p := &Peer{
		id:              id,
		pc:              pc,
		room:            room,
		ctx:             ctx,
		cancel:          cancel,
		metadata:        metadata,
		publishedTracks: make(map[string]*webrtc.TrackRemote),
		downTracks:      make(map[string]*DownTrack),
		state:           "connecting",
		peerConfig:      pcfg,
		encryption:      pcfg.Encryption,
	}

	// Add audio transceiver if publishing audio.
	if pcfg.PublishAudio {
		_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendrecv,
		})
		if err != nil {
			pc.Close()
			cancel()
			return nil, fmt.Errorf("add audio transceiver: %w", err)
		}
	}

	// Add video transceiver if publishing video.
	if pcfg.PublishVideo {
		_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendrecv,
		})
		if err != nil {
			pc.Close()
			cancel()
			return nil, fmt.Errorf("add video transceiver: %w", err)
		}
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		p.mu.Lock()
		p.publishedTracks[track.ID()] = track
		p.mu.Unlock()

		room.RegisterPublisherTrack(p, track)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		p.mu.Lock()
		switch state {
		case webrtc.PeerConnectionStateConnected:
			p.state = "connected"
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			p.state = "disconnected"
			p.mu.Unlock()
			room.RemovePeer(p.id)
			return
		case webrtc.PeerConnectionStateDisconnected:
			p.state = "disconnected"
		}
		p.mu.Unlock()
	})

	return p, nil
}

// ID returns the peer's identifier.
func (p *Peer) ID() string { return p.id }

// Context returns the peer's lifecycle context.
func (p *Peer) Context() context.Context { return p.ctx }

// HandleOffer sets the remote SDP offer and creates an answer.
func (p *Peer) HandleOffer(offerSDP string) (string, error) {
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}
	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return "", fmt.Errorf("set remote description: %w", err)
	}

	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("create answer: %w", err)
	}

	if err := p.pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("set local description: %w", err)
	}

	// Wait for ICE gathering to complete.
	gatherComplete := webrtc.GatheringCompletePromise(p.pc)
	<-gatherComplete

	return p.pc.LocalDescription().SDP, nil
}

// Renegotiate handles a mid-session SDP renegotiation (for track add/remove).
func (p *Peer) Renegotiate(offerSDP string) (string, error) {
	return p.HandleOffer(offerSDP)
}

// AddICECandidate adds a remote ICE candidate.
func (p *Peer) AddICECandidate(candidateJSON string) error {
	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal([]byte(candidateJSON), &candidate); err != nil {
		return fmt.Errorf("parse ICE candidate: %w", err)
	}
	return p.pc.AddICECandidate(candidate)
}

// AddDownTrack adds a forwarded track to this peer's PeerConnection.
func (p *Peer) AddDownTrack(dt *DownTrack) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, err := p.pc.AddTrack(dt.LocalTrack()); err != nil {
		return fmt.Errorf("add track to peer: %w", err)
	}
	p.downTracks[dt.trackID] = dt
	return nil
}

// RemoveDownTrack removes a forwarded track.
func (p *Peer) RemoveDownTrack(trackID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	dt, ok := p.downTracks[trackID]
	if !ok {
		return
	}
	if dt.cancel != nil {
		dt.cancel()
	}
	delete(p.downTracks, trackID)
}

// Close closes the peer connection and cancels the context.
// Safe to call multiple times (uses sync.Once).
func (p *Peer) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()

		for _, dt := range p.downTracks {
			if dt.cancel != nil {
				dt.cancel()
			}
		}
		p.downTracks = make(map[string]*DownTrack)
		p.publishedTracks = make(map[string]*webrtc.TrackRemote)

		p.state = "disconnected"
		if p.pc != nil {
			p.pc.Close()
		}
		if p.cancel != nil {
			p.cancel()
		}
	})
}

// Info returns peer metadata for proto conversion.
func (p *Peer) Info() PeerInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	return PeerInfo{
		ID:               p.id,
		State:            p.state,
		Metadata:         p.metadata,
		PublishedTracks:  len(p.publishedTracks),
		SubscribedTracks: len(p.downTracks),
	}
}
