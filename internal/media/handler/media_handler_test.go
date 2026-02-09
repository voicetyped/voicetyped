package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"connectrpc.com/connect"

	mediav1 "github.com/voicetyped/voicetyped/gen/voicetyped/media/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/media/v1/mediav1connect"
	"github.com/voicetyped/voicetyped/internal/media/sfu"
)

func setupTestServer(t *testing.T) (mediav1connect.MediaServiceClient, func()) {
	t.Helper()
	sfuInstance := sfu.New(sfu.SFUConfig{}, nil)
	handler := NewMediaHandler(sfuInstance, nil)

	mux := http.NewServeMux()
	path, hdlr := mediav1connect.NewMediaServiceHandler(handler)
	mux.Handle(path, hdlr)

	server := httptest.NewServer(mux)
	client := mediav1connect.NewMediaServiceClient(http.DefaultClient, server.URL)

	return client, server.Close
}

func TestCreateRoom(t *testing.T) {
	client, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := client.CreateRoom(context.Background(), connect.NewRequest(&mediav1.CreateRoomRequest{
		RoomId:   "test-room",
		MaxPeers: 10,
		Metadata: map[string]string{"env": "test"},
	}))
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	if resp.Msg.RoomId != "test-room" {
		t.Errorf("got room ID %q, want %q", resp.Msg.RoomId, "test-room")
	}
	if resp.Msg.CreatedAt == nil {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreateRoomDuplicate(t *testing.T) {
	client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.CreateRoom(context.Background(), connect.NewRequest(&mediav1.CreateRoomRequest{
		RoomId: "dup-room",
	}))
	if err != nil {
		t.Fatalf("first CreateRoom: %v", err)
	}

	_, err = client.CreateRoom(context.Background(), connect.NewRequest(&mediav1.CreateRoomRequest{
		RoomId: "dup-room",
	}))
	if err == nil {
		t.Fatal("expected error for duplicate room")
	}
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("got code %v, want AlreadyExists", connect.CodeOf(err))
	}
}

func TestGetRoom(t *testing.T) {
	client, cleanup := setupTestServer(t)
	defer cleanup()

	_, _ = client.CreateRoom(context.Background(), connect.NewRequest(&mediav1.CreateRoomRequest{
		RoomId:   "get-room",
		MaxPeers: 5,
		Metadata: map[string]string{"type": "test"},
	}))

	resp, err := client.GetRoom(context.Background(), connect.NewRequest(&mediav1.GetRoomRequest{
		RoomId: "get-room",
	}))
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}

	if resp.Msg.RoomId != "get-room" {
		t.Errorf("got room ID %q, want %q", resp.Msg.RoomId, "get-room")
	}
	if resp.Msg.MaxPeers != 5 {
		t.Errorf("got max peers %d, want 5", resp.Msg.MaxPeers)
	}
	if resp.Msg.Metadata["type"] != "test" {
		t.Errorf("got metadata %v, want type=test", resp.Msg.Metadata)
	}
}

func TestGetRoomNotFound(t *testing.T) {
	client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.GetRoom(context.Background(), connect.NewRequest(&mediav1.GetRoomRequest{
		RoomId: "nonexistent",
	}))
	if err == nil {
		t.Fatal("expected error for nonexistent room")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("got code %v, want NotFound", connect.CodeOf(err))
	}
}

func TestListRooms(t *testing.T) {
	client, cleanup := setupTestServer(t)
	defer cleanup()

	_, _ = client.CreateRoom(context.Background(), connect.NewRequest(&mediav1.CreateRoomRequest{RoomId: "a"}))
	_, _ = client.CreateRoom(context.Background(), connect.NewRequest(&mediav1.CreateRoomRequest{RoomId: "b"}))
	_, _ = client.CreateRoom(context.Background(), connect.NewRequest(&mediav1.CreateRoomRequest{RoomId: "c"}))

	resp, err := client.ListRooms(context.Background(), connect.NewRequest(&mediav1.ListRoomsRequest{}))
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}

	if len(resp.Msg.Rooms) != 3 {
		t.Errorf("got %d rooms, want 3", len(resp.Msg.Rooms))
	}
}

func TestCloseRoom(t *testing.T) {
	client, cleanup := setupTestServer(t)
	defer cleanup()

	_, _ = client.CreateRoom(context.Background(), connect.NewRequest(&mediav1.CreateRoomRequest{RoomId: "close-me"}))

	_, err := client.CloseRoom(context.Background(), connect.NewRequest(&mediav1.CloseRoomRequest{
		RoomId: "close-me",
	}))
	if err != nil {
		t.Fatalf("CloseRoom: %v", err)
	}

	_, err = client.GetRoom(context.Background(), connect.NewRequest(&mediav1.GetRoomRequest{
		RoomId: "close-me",
	}))
	if err == nil {
		t.Fatal("expected error after close")
	}
}

func TestCloseRoomNotFound(t *testing.T) {
	client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.CloseRoom(context.Background(), connect.NewRequest(&mediav1.CloseRoomRequest{
		RoomId: "nonexistent",
	}))
	if err == nil {
		t.Fatal("expected error for nonexistent room")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("got code %v, want NotFound", connect.CodeOf(err))
	}
}

func TestLeaveRoomNotFound(t *testing.T) {
	client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.LeaveRoom(context.Background(), connect.NewRequest(&mediav1.LeaveRoomRequest{
		RoomId: "nonexistent",
		PeerId: "p1",
	}))
	if err == nil {
		t.Fatal("expected error for nonexistent room")
	}
}

func TestTrickleICENotFound(t *testing.T) {
	client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.TrickleICE(context.Background(), connect.NewRequest(&mediav1.TrickleICERequest{
		RoomId:        "nonexistent",
		PeerId:        "p1",
		CandidateJson: "{}",
	}))
	if err == nil {
		t.Fatal("expected error for nonexistent room")
	}
}

func TestOnPeerJoinedCallback(t *testing.T) {
	sfuInstance := sfu.New(sfu.SFUConfig{}, nil)
	handler := NewMediaHandler(sfuInstance, nil)

	var mu sync.Mutex
	var calledRoom, calledPeer string
	handler.SetOnPeerJoined(func(ctx context.Context, roomID, peerID string, metadata map[string]string) {
		mu.Lock()
		calledRoom = roomID
		calledPeer = peerID
		mu.Unlock()
	})

	// Verify the callback is set.
	if handler.onPeerJoined == nil {
		t.Fatal("expected onPeerJoined to be set")
	}

	_ = calledRoom
	_ = calledPeer
}
