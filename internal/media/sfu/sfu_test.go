package sfu

import (
	"testing"

	"github.com/pion/webrtc/v4"
)

func testSFU() *SFU {
	return New(SFUConfig{}, nil)
}

func TestCreateRoom(t *testing.T) {
	s := testSFU()

	room, err := s.CreateRoom("test-room", 10, nil)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if room.ID() != "test-room" {
		t.Errorf("got room ID %q, want %q", room.ID(), "test-room")
	}
	if room.PeerCount() != 0 {
		t.Errorf("got peer count %d, want 0", room.PeerCount())
	}
}

func TestCreateRoomAutoID(t *testing.T) {
	s := testSFU()

	room, err := s.CreateRoom("", 5, nil)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if room.ID() == "" {
		t.Error("expected auto-generated room ID")
	}
}

func TestCreateRoomDuplicate(t *testing.T) {
	s := testSFU()

	_, err := s.CreateRoom("dup", 10, nil)
	if err != nil {
		t.Fatalf("first CreateRoom: %v", err)
	}

	_, err = s.CreateRoom("dup", 10, nil)
	if err == nil {
		t.Fatal("expected error for duplicate room ID")
	}
}

func TestListRooms(t *testing.T) {
	s := testSFU()

	_, _ = s.CreateRoom("a", 10, nil)
	_, _ = s.CreateRoom("b", 10, nil)

	rooms := s.ListRooms()
	if len(rooms) != 2 {
		t.Errorf("got %d rooms, want 2", len(rooms))
	}
}

func TestCloseRoom(t *testing.T) {
	s := testSFU()

	_, _ = s.CreateRoom("close-me", 10, nil)
	if err := s.CloseRoom("close-me"); err != nil {
		t.Fatalf("CloseRoom: %v", err)
	}

	_, ok := s.GetRoom("close-me")
	if ok {
		t.Error("room should not exist after close")
	}
}

func TestCloseRoomNotFound(t *testing.T) {
	s := testSFU()

	err := s.CloseRoom("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent room")
	}
}

func TestRoomMaxPeers(t *testing.T) {
	s := testSFU()
	room, _ := s.CreateRoom("max-test", 1, nil)

	p1 := &Peer{id: "p1", state: "connected", publishedTracks: make(map[string]*webrtc.TrackRemote), downTracks: make(map[string]*DownTrack), peerConfig: DefaultPeerConfig()}
	if _, err := room.AddPeer(p1); err != nil {
		t.Fatalf("AddPeer p1: %v", err)
	}

	p2 := &Peer{id: "p2", state: "connected", publishedTracks: make(map[string]*webrtc.TrackRemote), downTracks: make(map[string]*DownTrack), peerConfig: DefaultPeerConfig()}
	if _, err := room.AddPeer(p2); err == nil {
		t.Fatal("expected error when room is full")
	}
}

func TestRoomClosedRejectsNewPeers(t *testing.T) {
	s := testSFU()
	room, _ := s.CreateRoom("closed-test", 10, nil)
	room.Close()

	p := &Peer{id: "late", state: "connecting", publishedTracks: make(map[string]*webrtc.TrackRemote), downTracks: make(map[string]*DownTrack), peerConfig: DefaultPeerConfig()}
	if _, err := room.AddPeer(p); err == nil {
		t.Fatal("expected error adding peer to closed room")
	}
}

func TestRoomRemovePeer(t *testing.T) {
	s := testSFU()
	room, _ := s.CreateRoom("remove-test", 10, nil)

	p := &Peer{id: "removable", state: "connected", publishedTracks: make(map[string]*webrtc.TrackRemote), downTracks: make(map[string]*DownTrack), peerConfig: DefaultPeerConfig()}
	_, _ = room.AddPeer(p)

	room.RemovePeer("removable")
	if room.PeerCount() != 0 {
		t.Errorf("got peer count %d after remove, want 0", room.PeerCount())
	}
}

func TestStats(t *testing.T) {
	s := testSFU()

	room, _ := s.CreateRoom("stats-room", 10, nil)
	p := &Peer{id: "p1", state: "connected", publishedTracks: make(map[string]*webrtc.TrackRemote), downTracks: make(map[string]*DownTrack), peerConfig: DefaultPeerConfig()}
	_, _ = room.AddPeer(p)

	stats := s.Stats()
	if stats.RoomCount != 1 {
		t.Errorf("got room count %d, want 1", stats.RoomCount)
	}
	if stats.PeerCount != 1 {
		t.Errorf("got peer count %d, want 1", stats.PeerCount)
	}
}

func TestE2EEValidation(t *testing.T) {
	// Room requires E2EE, peer without encryption should be rejected.
	if err := ValidateE2EERoom(true, nil); err == nil {
		t.Fatal("expected error for nil encryption on E2EE room")
	}

	// Room requires E2EE, peer with valid encryption should pass.
	enc := &EncryptionInfo{Algorithm: "AES_128_GCM", KeyID: 1}
	if err := ValidateE2EERoom(true, enc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Room does not require E2EE, peer without encryption should pass.
	if err := ValidateE2EERoom(false, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpeakerDetector(t *testing.T) {
	sd := NewSpeakerDetector(30, 0, nil)

	sd.UpdateLevel("p1", 10, true)
	sd.UpdateLevel("p2", 50, false)

	speakers := sd.ActiveSpeakers()
	if len(speakers) != 1 {
		t.Errorf("got %d active speakers, want 1", len(speakers))
	}
	if len(speakers) > 0 && speakers[0].PeerID != "p1" {
		t.Errorf("got speaker %q, want p1", speakers[0].PeerID)
	}
}

func TestRoomE2EERequired(t *testing.T) {
	s := New(SFUConfig{E2EEDefaultRequired: true}, nil)
	room, err := s.CreateRoom("e2ee-room", 10, nil)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	// Peer without encryption should be rejected.
	p := &Peer{id: "p1", state: "connecting", publishedTracks: make(map[string]*webrtc.TrackRemote), downTracks: make(map[string]*DownTrack), peerConfig: DefaultPeerConfig()}
	_, err = room.AddPeer(p)
	if err == nil {
		t.Fatal("expected error adding non-E2EE peer to E2EE room")
	}

	// Peer with encryption should succeed.
	p2 := &Peer{
		id: "p2", state: "connecting",
		publishedTracks: make(map[string]*webrtc.TrackRemote),
		downTracks:      make(map[string]*DownTrack),
		peerConfig:      DefaultPeerConfig(),
		encryption:      &EncryptionInfo{Algorithm: "AES_128_GCM", KeyID: 1},
	}
	_, err = room.AddPeer(p2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
