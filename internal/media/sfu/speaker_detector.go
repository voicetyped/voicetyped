package sfu

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/pitabwire/frame/workerpool"
)

// ActiveSpeakerInfo reports a single speaker's audio state.
type ActiveSpeakerInfo struct {
	PeerID        string
	AudioLevel    uint8
	VoiceActivity bool
}

// SpeakerListener is called when the active speaker set changes.
type SpeakerListener func(speakers []ActiveSpeakerInfo)

type speakerState struct {
	level     uint8
	lastSeen  time.Time
	voiceActivity bool
}

// SpeakerDetector tracks audio levels from peers and periodically
// reports the active speakers to registered listeners.
type SpeakerDetector struct {
	mu        sync.RWMutex
	levels    map[string]*speakerState
	listeners map[string]SpeakerListener
	threshold uint8
	interval  time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
	pool      workerpool.WorkerPool
}

// NewSpeakerDetector creates a new speaker detector.
func NewSpeakerDetector(threshold uint8, interval time.Duration, pool workerpool.WorkerPool) *SpeakerDetector {
	if threshold == 0 {
		threshold = 30
	}
	if interval == 0 {
		interval = 500 * time.Millisecond
	}
	return &SpeakerDetector{
		levels:    make(map[string]*speakerState),
		listeners: make(map[string]SpeakerListener),
		threshold: threshold,
		interval:  interval,
		pool:      pool,
	}
}

// Start begins the periodic speaker detection ticker.
func (sd *SpeakerDetector) Start(ctx context.Context) {
	sd.ctx, sd.cancel = context.WithCancel(ctx)
	fn := func() {
		ticker := time.NewTicker(sd.interval)
		defer ticker.Stop()
		for {
			select {
			case <-sd.ctx.Done():
				return
			case <-ticker.C:
				sd.report()
			}
		}
	}
	if sd.pool != nil {
		_ = sd.pool.Submit(sd.ctx, fn)
	} else {
		go fn()
	}
}

// UpdateLevel records a new audio level for a peer.
// The level follows RFC 6464: 0 = loudest, 127 = silence.
func (sd *SpeakerDetector) UpdateLevel(peerID string, level uint8, voiceActivity bool) {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	s, ok := sd.levels[peerID]
	if !ok {
		s = &speakerState{}
		sd.levels[peerID] = s
	}
	s.level = level
	s.lastSeen = time.Now()
	s.voiceActivity = voiceActivity
}

// AddListener registers a callback for active speaker updates.
func (sd *SpeakerDetector) AddListener(id string, fn SpeakerListener) {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	sd.listeners[id] = fn
}

// RemoveListener removes a previously registered listener.
func (sd *SpeakerDetector) RemoveListener(id string) {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	delete(sd.listeners, id)
}

// ActiveSpeakers returns the current set of active speakers.
func (sd *SpeakerDetector) ActiveSpeakers() []ActiveSpeakerInfo {
	sd.mu.RLock()
	defer sd.mu.RUnlock()
	return sd.activeSpeakersLocked()
}

func (sd *SpeakerDetector) activeSpeakersLocked() []ActiveSpeakerInfo {
	now := time.Now()
	var speakers []ActiveSpeakerInfo
	for peerID, s := range sd.levels {
		// Exclude peers silent for >2s.
		if now.Sub(s.lastSeen) > 2*time.Second {
			continue
		}
		// Level below threshold means speaking (lower = louder in RFC 6464).
		if s.level <= sd.threshold || s.voiceActivity {
			speakers = append(speakers, ActiveSpeakerInfo{
				PeerID:        peerID,
				AudioLevel:    s.level,
				VoiceActivity: s.voiceActivity,
			})
		}
	}
	// Sort by loudness (lower level = louder).
	sort.Slice(speakers, func(i, j int) bool {
		return speakers[i].AudioLevel < speakers[j].AudioLevel
	})
	return speakers
}

func (sd *SpeakerDetector) report() {
	sd.mu.RLock()
	speakers := sd.activeSpeakersLocked()
	listeners := make([]SpeakerListener, 0, len(sd.listeners))
	for _, fn := range sd.listeners {
		listeners = append(listeners, fn)
	}
	sd.mu.RUnlock()

	for _, fn := range listeners {
		fn(speakers)
	}
}

// RemovePeer removes a peer's state from the detector.
func (sd *SpeakerDetector) RemovePeer(peerID string) {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	delete(sd.levels, peerID)
}

// Close stops the speaker detector.
func (sd *SpeakerDetector) Close() {
	if sd.cancel != nil {
		sd.cancel()
	}
}
