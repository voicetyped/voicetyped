package sfu

import (
	"context"

	"github.com/pion/webrtc/v4"
)

// DownTrack represents a forwarded track from a publisher to a subscriber.
type DownTrack struct {
	local    *webrtc.TrackLocalStaticRTP
	sourceID string
	trackID  string
	rid      string
	muted    bool
	cancel   context.CancelFunc
}

// NewDownTrack creates a new DownTrack matching the remote track's codec.
func NewDownTrack(remote *webrtc.TrackRemote, publisherPeerID string) (*DownTrack, error) {
	local, err := webrtc.NewTrackLocalStaticRTP(
		remote.Codec().RTPCodecCapability,
		remote.ID(),
		remote.StreamID(),
	)
	if err != nil {
		return nil, err
	}

	return &DownTrack{
		local:    local,
		sourceID: publisherPeerID,
		trackID:  remote.ID(),
	}, nil
}

// NewSimulcastDownTrack creates a DownTrack with a specific RTP stream ID (RID)
// for simulcast layer identification.
func NewSimulcastDownTrack(remote *webrtc.TrackRemote, publisherPeerID, rid string) (*DownTrack, error) {
	local, err := webrtc.NewTrackLocalStaticRTP(
		remote.Codec().RTPCodecCapability,
		remote.ID(),
		remote.StreamID(),
		webrtc.WithRTPStreamID(rid),
	)
	if err != nil {
		return nil, err
	}

	return &DownTrack{
		local:    local,
		sourceID: publisherPeerID,
		trackID:  remote.ID(),
		rid:      rid,
	}, nil
}

// LocalTrack returns the local track for adding to a PeerConnection.
func (d *DownTrack) LocalTrack() *webrtc.TrackLocalStaticRTP {
	return d.local
}

// Write sends RTP data to the local track, respecting the muted state.
func (d *DownTrack) Write(data []byte) {
	if d.muted {
		return
	}
	_, _ = d.local.Write(data)
}

// SetMuted sets the muted state of the DownTrack.
func (d *DownTrack) SetMuted(muted bool) {
	d.muted = muted
}

// Forward reads RTP packets from remote and writes to local.
// No decode/re-encode — pure forwarding. Runs until ctx cancelled or read error.
func Forward(ctx context.Context, remote *webrtc.TrackRemote, local *webrtc.TrackLocalStaticRTP) {
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

		if _, err := local.Write(buf[:n]); err != nil {
			return
		}
	}
}
