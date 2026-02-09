package sfu

import (
	"context"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

// pliDebouncer limits PLI requests to at most one per interval per track.
type pliDebouncer struct {
	mu       sync.Mutex
	lastPLI  time.Time
	interval time.Duration
}

func newPLIDebouncer(interval time.Duration) *pliDebouncer {
	return &pliDebouncer{interval: interval}
}

func (d *pliDebouncer) shouldSendPLI() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if now.Sub(d.lastPLI) < d.interval {
		return false
	}
	d.lastPLI = now
	return true
}

// sendPLI sends a Picture Loss Indication to the publisher.
func sendPLI(pc *webrtc.PeerConnection, ssrc webrtc.SSRC) {
	_ = pc.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{
			MediaSSRC: uint32(ssrc),
		},
	})
}

// RunSimpleForwarder reads RTP from remote and writes to the subscription's
// down track. This is the basic passthrough for non-simulcast, non-SVC tracks.
func RunSimpleForwarder(ctx context.Context, remote *webrtc.TrackRemote, sub *Subscription) {
	buf := make([]byte, 1500)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, _, err := remote.Read(buf)
		if err != nil {
			return
		}

		sub.mu.Lock()
		paused := sub.paused
		dt := sub.downTrack
		sub.mu.Unlock()

		if paused || dt == nil {
			continue
		}
		dt.Write(buf[:n])
	}
}

// RunSimulcastForwarder selects the active simulcast layer based on the
// subscription's currentQuality and forwards from that layer.
// When the layer changes, it sends a PLI to the publisher.
func RunSimulcastForwarder(ctx context.Context, pt *PublisherTrack, sub *Subscription) {
	debounce := newPLIDebouncer(500 * time.Millisecond)
	buf := make([]byte, 1500)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		sub.mu.Lock()
		paused := sub.paused
		quality := sub.currentQuality
		dt := sub.downTrack
		sub.mu.Unlock()

		if paused || dt == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		rid := qualityToRID(quality)

		pt.mu.RLock()
		layer, ok := pt.layers[rid]
		pt.mu.RUnlock()

		if !ok {
			// Fallback: try to find any available layer.
			pt.mu.RLock()
			for _, l := range pt.layers {
				layer = l
				break
			}
			pt.mu.RUnlock()
		}

		if layer == nil || layer.remote == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		n, _, err := layer.remote.Read(buf)
		if err != nil {
			return
		}

		dt.Write(buf[:n])

		// Check if layer changed since last iteration and send PLI.
		sub.mu.Lock()
		newQuality := sub.currentQuality
		sub.mu.Unlock()

		if newQuality != quality && debounce.shouldSendPLI() {
			pt.mu.RLock()
			pub := pt.publisher
			newRID := qualityToRID(newQuality)
			newLayer, ok := pt.layers[newRID]
			pt.mu.RUnlock()
			if ok && newLayer.remote != nil && pub != nil {
				pub.mu.Lock()
				pc := pub.pc
				pub.mu.Unlock()
				if pc != nil {
					sendPLI(pc, newLayer.remote.SSRC())
				}
			}
		}
	}
}

// RunSVCForwarder reads a single SVC track and drops packets whose
// spatial/temporal layer exceeds the subscription's limits.
// Supports VP9 SVC. For other codecs, forwards all packets (safe fallback).
func RunSVCForwarder(ctx context.Context, remote *webrtc.TrackRemote, sub *Subscription) {
	buf := make([]byte, 1500)
	isVP9 := remote.Codec().MimeType == "video/VP9" || remote.Codec().MimeType == "video/vp9"

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, _, err := remote.Read(buf)
		if err != nil {
			return
		}

		sub.mu.Lock()
		paused := sub.paused
		maxSpatial := sub.maxSpatialLayer
		maxTemporal := sub.maxTemporalLayer
		dt := sub.downTrack
		sub.mu.Unlock()

		if paused || dt == nil {
			continue
		}

		if isVP9 && (maxSpatial >= 0 || maxTemporal >= 0) {
			// Parse RTP packet to get payload.
			pkt := &rtp.Packet{}
			if err := pkt.Unmarshal(buf[:n]); err != nil {
				continue
			}

			var vp9 codecs.VP9Packet
			if _, err := vp9.Unmarshal(pkt.Payload); err == nil {
				if maxSpatial >= 0 && int(vp9.SID) > maxSpatial {
					continue
				}
				if maxTemporal >= 0 && int(vp9.TID) > maxTemporal {
					continue
				}
			}
		}

		dt.Write(buf[:n])
	}
}

// VideoQualityLevel represents simulcast quality levels.
type VideoQualityLevel int

const (
	QualityLow    VideoQualityLevel = 1
	QualityMedium VideoQualityLevel = 2
	QualityHigh   VideoQualityLevel = 3
)

func qualityToRID(q VideoQualityLevel) string {
	switch q {
	case QualityLow:
		return "q"
	case QualityMedium:
		return "h"
	case QualityHigh:
		return "f"
	default:
		return ""
	}
}

func ridToQuality(rid string) VideoQualityLevel {
	switch rid {
	case "q":
		return QualityLow
	case "h":
		return QualityMedium
	case "f":
		return QualityHigh
	default:
		return QualityHigh
	}
}
