package sipbridge

import (
	"context"
	"fmt"

	"github.com/voicetyped/voicetyped/internal/media/sfu"
)

// SIPBridge connects a SIP endpoint to an SFU room as a synthetic peer.
type SIPBridge struct {
	room   *sfu.Room
	peer   *sfu.Peer
	sipURI string
}

// NewSIPBridge creates a SIP bridge peer in the given room.
// TODO: connects to SIP endpoint via diago when available.
func NewSIPBridge(ctx context.Context, room *sfu.Room, sipURI string, sfuInstance *sfu.SFU) (*SIPBridge, error) {
	if room == nil {
		return nil, fmt.Errorf("room is required")
	}

	peer, err := sfu.NewPeer(
		ctx,
		fmt.Sprintf("sip-%s", sipURI),
		room,
		sfuInstance.API(),
		sfuInstance.Config(),
		map[string]string{
			"type":    "sip",
			"sip_uri": sipURI,
		},
		sfu.PeerConfig{PublishAudio: true, AutoSubscribeAudio: true},
	)
	if err != nil {
		return nil, fmt.Errorf("create SIP peer: %w", err)
	}

	if _, err := room.AddPeer(peer); err != nil {
		peer.Close()
		return nil, fmt.Errorf("add SIP peer to room: %w", err)
	}

	return &SIPBridge{
		room:   room,
		peer:   peer,
		sipURI: sipURI,
	}, nil
}

// PeerID returns the bridge peer's ID.
func (b *SIPBridge) PeerID() string {
	return b.peer.ID()
}

// Close disconnects the SIP bridge.
func (b *SIPBridge) Close() {
	b.room.RemovePeer(b.peer.ID())
}
