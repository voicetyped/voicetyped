package sfu

import (
	"context"
	"log/slog"
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pitabwire/frame/workerpool"
)

// trackLayer holds a single simulcast layer (or the sole layer for non-simulcast).
type trackLayer struct {
	rid    string // "" for non-simulcast, "q"/"h"/"f" for simulcast
	remote *webrtc.TrackRemote
}

// PublisherTrackInfo holds metadata about a published track for proto conversion.
type PublisherTrackInfo struct {
	ID        string
	PeerID    string
	Kind      webrtc.RTPCodecType
	MimeType  string
	Simulcast bool
	SVC       bool
	Layers    []string // available RIDs
	Encryption *EncryptionInfo
}

// PublisherTrack groups simulcast layers under one logical track.
type PublisherTrack struct {
	mu          sync.RWMutex
	id          string
	publisher   *Peer
	kind        webrtc.RTPCodecType
	mimeType    string
	simulcast   bool
	svc         bool
	layers      map[string]*trackLayer    // RID -> layer ("" for non-simulcast)
	subscribers map[string]*Subscription  // subscriberPeerID -> Subscription
	audioTaps   map[string]AudioTapFunc   // for audio tracks: ASR pipeline taps
	encryption  *EncryptionInfo
	ctx         context.Context
	cancel      context.CancelFunc
	pool        workerpool.WorkerPool
	speakerDet  *SpeakerDetector
}

// NewPublisherTrack creates a new publisher track.
func NewPublisherTrack(
	parentCtx context.Context,
	publisher *Peer,
	remote *webrtc.TrackRemote,
	pool workerpool.WorkerPool,
	speakerDet *SpeakerDetector,
	encryption *EncryptionInfo,
) *PublisherTrack {
	ctx, cancel := context.WithCancel(parentCtx)

	kind := webrtc.RTPCodecTypeAudio
	if remote.Kind() == webrtc.RTPCodecTypeVideo {
		kind = webrtc.RTPCodecTypeVideo
	}

	rid := remote.RID()
	simulcast := rid != ""

	pt := &PublisherTrack{
		id:          remote.ID(),
		publisher:   publisher,
		kind:        kind,
		mimeType:    remote.Codec().MimeType,
		simulcast:   simulcast,
		layers:      make(map[string]*trackLayer),
		subscribers: make(map[string]*Subscription),
		audioTaps:   make(map[string]AudioTapFunc),
		encryption:  encryption,
		ctx:         ctx,
		cancel:      cancel,
		pool:        pool,
		speakerDet:  speakerDet,
	}

	pt.layers[rid] = &trackLayer{rid: rid, remote: remote}

	// Start the RTP reader for this layer.
	pt.startLayerReader(rid)

	return pt
}

// ID returns the logical track ID.
func (pt *PublisherTrack) ID() string { return pt.id }

// Kind returns the track kind (audio/video).
func (pt *PublisherTrack) Kind() webrtc.RTPCodecType { return pt.kind }

// MimeType returns the track's MIME type.
func (pt *PublisherTrack) MimeType() string { return pt.mimeType }

// IsSimulcast returns whether this track uses simulcast.
func (pt *PublisherTrack) IsSimulcast() bool { return pt.simulcast }

// IsSVC returns whether this track uses SVC.
func (pt *PublisherTrack) IsSVC() bool { return pt.svc }

// SetSVC marks this track as using SVC.
func (pt *PublisherTrack) SetSVC(svc bool) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.svc = svc
}

// AddLayer adds a simulcast layer to this publisher track.
func (pt *PublisherTrack) AddLayer(remote *webrtc.TrackRemote) {
	rid := remote.RID()
	pt.mu.Lock()
	pt.simulcast = true
	pt.layers[rid] = &trackLayer{rid: rid, remote: remote}
	pt.mu.Unlock()

	pt.startLayerReader(rid)
}

// Subscribe creates a subscription for a peer to this track.
func (pt *PublisherTrack) Subscribe(subscriber *Peer, quality VideoQualityLevel, maxTemporal, maxSpatial int) (*Subscription, error) {
	pt.mu.RLock()
	// Pick a layer to determine codec for DownTrack.
	var sourceRemote *webrtc.TrackRemote
	if pt.simulcast {
		rid := qualityToRID(quality)
		if layer, ok := pt.layers[rid]; ok {
			sourceRemote = layer.remote
		}
	}
	// Fallback to any available layer.
	if sourceRemote == nil {
		for _, layer := range pt.layers {
			sourceRemote = layer.remote
			break
		}
	}
	pt.mu.RUnlock()

	if sourceRemote == nil {
		return nil, ErrNoLayersAvailable
	}

	var dt *DownTrack
	var err error
	if pt.simulcast {
		dt, err = NewSimulcastDownTrack(sourceRemote, pt.publisher.ID(), qualityToRID(quality))
	} else {
		dt, err = NewDownTrack(sourceRemote, pt.publisher.ID())
	}
	if err != nil {
		return nil, err
	}

	if err := subscriber.AddDownTrack(dt); err != nil {
		return nil, err
	}

	sub := NewSubscription(pt, subscriber, dt, quality, maxTemporal, maxSpatial)

	pt.mu.Lock()
	pt.subscribers[subscriber.ID()] = sub
	pt.mu.Unlock()

	// Start the appropriate forwarder.
	pt.startForwarder(sub)

	return sub, nil
}

// Unsubscribe removes a peer's subscription.
func (pt *PublisherTrack) Unsubscribe(subscriberPeerID string) {
	pt.mu.Lock()
	sub, ok := pt.subscribers[subscriberPeerID]
	if ok {
		delete(pt.subscribers, subscriberPeerID)
	}
	pt.mu.Unlock()

	if ok && sub != nil {
		sub.Close()
		sub.subscriber.RemoveDownTrack(pt.id)
	}
}

// UpdateSubscription updates an existing subscription's layer/quality settings.
func (pt *PublisherTrack) UpdateSubscription(subscriberPeerID string, quality VideoQualityLevel, maxTemporal, maxSpatial int, paused bool) error {
	pt.mu.RLock()
	sub, ok := pt.subscribers[subscriberPeerID]
	pt.mu.RUnlock()

	if !ok {
		return ErrSubscriptionNotFound
	}

	sub.mu.Lock()
	sub.currentQuality = quality
	sub.maxTemporalLayer = maxTemporal
	sub.maxSpatialLayer = maxSpatial
	sub.paused = paused
	sub.mu.Unlock()

	return nil
}

// GetSubscription returns a peer's subscription if it exists.
func (pt *PublisherTrack) GetSubscription(subscriberPeerID string) (*Subscription, bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	sub, ok := pt.subscribers[subscriberPeerID]
	return sub, ok
}

// AddAudioTap registers a callback for audio data from this track.
func (pt *PublisherTrack) AddAudioTap(id string, fn AudioTapFunc) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.audioTaps[id] = fn
}

// RemoveAudioTap removes a previously registered audio tap.
func (pt *PublisherTrack) RemoveAudioTap(id string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	delete(pt.audioTaps, id)
}

// Info returns track metadata for proto conversion.
func (pt *PublisherTrack) Info() PublisherTrackInfo {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	layers := make([]string, 0, len(pt.layers))
	for rid := range pt.layers {
		layers = append(layers, rid)
	}

	return PublisherTrackInfo{
		ID:         pt.id,
		PeerID:     pt.publisher.ID(),
		Kind:       pt.kind,
		MimeType:   pt.mimeType,
		Simulcast:  pt.simulcast,
		SVC:        pt.svc,
		Layers:     layers,
		Encryption: pt.encryption,
	}
}

// Subscribers returns all current subscriptions.
func (pt *PublisherTrack) Subscribers() []*Subscription {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	subs := make([]*Subscription, 0, len(pt.subscribers))
	for _, sub := range pt.subscribers {
		subs = append(subs, sub)
	}
	return subs
}

// Close cancels all subscriptions and the track reader.
func (pt *PublisherTrack) Close() {
	pt.mu.Lock()
	subs := make([]*Subscription, 0, len(pt.subscribers))
	for _, sub := range pt.subscribers {
		subs = append(subs, sub)
	}
	pt.subscribers = make(map[string]*Subscription)
	pt.audioTaps = make(map[string]AudioTapFunc)
	pt.mu.Unlock()

	for _, sub := range subs {
		sub.Close()
	}

	if pt.cancel != nil {
		pt.cancel()
	}
}

// startLayerReader starts an RTP reader goroutine for the given layer.
func (pt *PublisherTrack) startLayerReader(rid string) {
	pt.mu.RLock()
	layer, ok := pt.layers[rid]
	pt.mu.RUnlock()
	if !ok || layer.remote == nil {
		return
	}

	fn := func() {
		pt.rtpReaderLoop(layer)
	}
	if pt.pool != nil {
		_ = pt.pool.Submit(pt.ctx, fn)
	} else {
		go fn()
	}
}

// rtpReaderLoop reads RTP packets from a layer and dispatches to audio taps
// and the speaker detector. Subscriptions are handled by their own forwarders.
func (pt *PublisherTrack) rtpReaderLoop(layer *trackLayer) {
	remote := layer.remote
	codec := remote.Codec().MimeType
	buf := make([]byte, 1500)

	for {
		select {
		case <-pt.ctx.Done():
			return
		default:
		}

		n, _, err := remote.Read(buf)
		if err != nil {
			return
		}

		// For audio tracks: parse audio level extension and dispatch to taps.
		if pt.kind == webrtc.RTPCodecTypeAudio && n > 12 {
			// Parse RTP packet for audio level extension.
			if pt.speakerDet != nil {
				pkt := &rtp.Packet{}
				if err := pkt.Unmarshal(buf[:n]); err == nil {
					pt.parseAudioLevel(pkt)
				}
			}

			// Dispatch to audio taps.
			payload := make([]byte, n-12)
			copy(payload, buf[12:n])
			peerID := pt.publisher.ID()

			pt.mu.RLock()
			taps := make([]AudioTapFunc, 0, len(pt.audioTaps))
			for _, tap := range pt.audioTaps {
				taps = append(taps, tap)
			}
			pt.mu.RUnlock()

			for _, tap := range taps {
				tap := tap
				if pt.pool != nil {
					if err := pt.pool.Submit(pt.ctx, func() {
						tap(peerID, payload, codec)
					}); err != nil {
						slog.Warn("audio tap pool full", slog.String("track", pt.id))
					}
				} else {
					tap(peerID, payload, codec)
				}
			}
		}

		// Write to subscribers directly (simple forwarding for non-simulcast/non-SVC).
		// Simulcast and SVC subscriptions use their own forwarder goroutines
		// which read from the layer directly.
		if !pt.simulcast && !pt.svc {
			pt.mu.RLock()
			for _, sub := range pt.subscribers {
				sub.mu.Lock()
				paused := sub.paused
				dt := sub.downTrack
				sub.mu.Unlock()
				if !paused && dt != nil {
					dt.Write(buf[:n])
				}
			}
			pt.mu.RUnlock()
		}
	}
}

// audioLevelExtensionID is the RTP header extension ID for audio level.
// This should match the ID registered in the MediaEngine.
const audioLevelExtensionID = 1

// parseAudioLevel extracts the RFC 6464 audio level from the RTP header extension.
func (pt *PublisherTrack) parseAudioLevel(pkt *rtp.Packet) {
	var ext rtp.AudioLevelExtension
	raw := pkt.Header.GetExtension(audioLevelExtensionID)
	if raw == nil {
		return
	}
	if err := ext.Unmarshal(raw); err != nil {
		return
	}
	pt.speakerDet.UpdateLevel(pt.publisher.ID(), ext.Level, ext.Voice)
}

// startForwarder starts the appropriate forwarder goroutine for a subscription.
func (pt *PublisherTrack) startForwarder(sub *Subscription) {
	// For simple (non-simulcast, non-SVC) tracks, the rtpReaderLoop
	// directly writes to subscribers, so no separate forwarder needed.
	if !pt.simulcast && !pt.svc {
		return
	}

	fn := func() {
		if pt.simulcast {
			RunSimulcastForwarder(sub.ctx, pt, sub)
		} else if pt.svc {
			// For SVC, use the first available layer.
			pt.mu.RLock()
			var remote *webrtc.TrackRemote
			for _, layer := range pt.layers {
				remote = layer.remote
				break
			}
			pt.mu.RUnlock()
			if remote != nil {
				RunSVCForwarder(sub.ctx, remote, sub)
			}
		}
	}

	if pt.pool != nil {
		_ = pt.pool.Submit(sub.ctx, fn)
	} else {
		go fn()
	}
}

// errors
var (
	ErrNoLayersAvailable    = &SFUError{"no layers available for track"}
	ErrSubscriptionNotFound = &SFUError{"subscription not found"}
)

// SFUError is a simple error type for SFU operations.
type SFUError struct {
	msg string
}

func (e *SFUError) Error() string { return e.msg }
