package sfu

import (
	"context"
	"sync"

	"github.com/rs/xid"
)

// Subscription represents a subscriber's view of a PublisherTrack.
type Subscription struct {
	mu               sync.Mutex
	id               string
	publisherTrack   *PublisherTrack
	subscriber       *Peer
	downTrack        *DownTrack
	currentQuality   VideoQualityLevel
	maxTemporalLayer int // SVC TID limit, -1 = all
	maxSpatialLayer  int // SVC SID limit, -1 = all
	paused           bool
	ctx              context.Context
	cancel           context.CancelFunc
}

// NewSubscription creates a subscription from a PublisherTrack to a subscriber peer.
func NewSubscription(pt *PublisherTrack, subscriber *Peer, dt *DownTrack, quality VideoQualityLevel, maxTemporal, maxSpatial int) *Subscription {
	ctx, cancel := context.WithCancel(pt.ctx)
	if quality == 0 {
		quality = QualityHigh
	}
	return &Subscription{
		id:               xid.New().String(),
		publisherTrack:   pt,
		subscriber:       subscriber,
		downTrack:        dt,
		currentQuality:   quality,
		maxTemporalLayer: maxTemporal,
		maxSpatialLayer:  maxSpatial,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// ID returns the subscription's unique identifier.
func (s *Subscription) ID() string { return s.id }

// SwitchLayer changes the simulcast layer for this subscription.
func (s *Subscription) SwitchLayer(quality VideoQualityLevel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentQuality = quality
}

// SetSVCFilter updates the SVC layer limits.
func (s *Subscription) SetSVCFilter(maxTemporal, maxSpatial int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxTemporalLayer = maxTemporal
	s.maxSpatialLayer = maxSpatial
}

// Pause stops forwarding packets for this subscription.
func (s *Subscription) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
}

// Resume resumes forwarding packets for this subscription.
func (s *Subscription) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = false
}

// IsPaused returns whether the subscription is paused.
func (s *Subscription) IsPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// Close cancels the subscription's forwarding goroutine.
func (s *Subscription) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

// Info returns subscription metadata for proto conversion.
type SubscriptionDetail struct {
	ID               string
	TrackID          string
	PublisherPeerID  string
	Quality          VideoQualityLevel
	MaxTemporalLayer int
	MaxSpatialLayer  int
	Paused           bool
}

func (s *Subscription) Info() SubscriptionDetail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SubscriptionDetail{
		ID:               s.id,
		TrackID:          s.publisherTrack.id,
		PublisherPeerID:  s.publisherTrack.publisher.ID(),
		Quality:          s.currentQuality,
		MaxTemporalLayer: s.maxTemporalLayer,
		MaxSpatialLayer:  s.maxSpatialLayer,
		Paused:           s.paused,
	}
}
