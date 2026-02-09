package handler

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/pion/webrtc/v4"
	"github.com/pitabwire/frame/workerpool"
	"github.com/rs/xid"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/voicetyped/voicetyped/gen/voicetyped/common/v1"
	mediav1 "github.com/voicetyped/voicetyped/gen/voicetyped/media/v1"
	"github.com/voicetyped/voicetyped/gen/voicetyped/media/v1/mediav1connect"
	"github.com/voicetyped/voicetyped/internal/media/sfu"
	"github.com/voicetyped/voicetyped/internal/media/sipbridge"
)

// Ensure we implement the interface.
var _ mediav1connect.MediaServiceHandler = (*MediaHandler)(nil)

// PeerJoinedFunc is called when a peer joins a room, for orchestration.
type PeerJoinedFunc func(ctx context.Context, roomID, peerID string, metadata map[string]string)

// MediaHandler implements mediav1connect.MediaServiceHandler.
type MediaHandler struct {
	sfu          *sfu.SFU
	pool         workerpool.WorkerPool
	onPeerJoined PeerJoinedFunc
}

// NewMediaHandler creates a new media service handler.
func NewMediaHandler(s *sfu.SFU, pool workerpool.WorkerPool) *MediaHandler {
	return &MediaHandler{sfu: s, pool: pool}
}

// SetOnPeerJoined sets a callback invoked (via worker pool) when a peer joins a room.
func (h *MediaHandler) SetOnPeerJoined(fn PeerJoinedFunc) {
	h.onPeerJoined = fn
}

func (h *MediaHandler) CreateRoom(_ context.Context, req *connect.Request[mediav1.CreateRoomRequest]) (*connect.Response[mediav1.CreateRoomResponse], error) {
	rc := sfu.RoomConfig{
		MaxPublishers: int(req.Msg.MaxPublishers),
		E2EERequired:  req.Msg.E2EeRequired,
	}
	if req.Msg.AutoSubscribeAudio {
		v := true
		rc.AutoSubscribeAudio = &v
	}

	room, err := h.sfu.CreateRoomWithConfig(req.Msg.RoomId, int(req.Msg.MaxPeers), req.Msg.Metadata, rc)
	if err != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, err)
	}

	return connect.NewResponse(&mediav1.CreateRoomResponse{
		RoomId:    room.ID(),
		CreatedAt: timestamppb.New(room.CreatedAt()),
	}), nil
}

func (h *MediaHandler) GetRoom(_ context.Context, req *connect.Request[mediav1.GetRoomRequest]) (*connect.Response[mediav1.GetRoomResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	peers := room.Peers()
	peerInfos := make([]*mediav1.PeerInfo, 0, len(peers))
	for _, p := range peers {
		info := p.Info()
		peerInfos = append(peerInfos, &mediav1.PeerInfo{
			PeerId:           info.ID,
			State:            info.State,
			Metadata:         info.Metadata,
			PublishedTracks:  int32(info.PublishedTracks),
			SubscribedTracks: int32(info.SubscribedTracks),
		})
	}

	return connect.NewResponse(&mediav1.GetRoomResponse{
		RoomId:    room.ID(),
		Peers:     peerInfos,
		MaxPeers:  int32(room.MaxPeers()),
		Metadata:  room.Metadata(),
		CreatedAt: timestamppb.New(room.CreatedAt()),
	}), nil
}

func (h *MediaHandler) ListRooms(_ context.Context, _ *connect.Request[mediav1.ListRoomsRequest]) (*connect.Response[mediav1.ListRoomsResponse], error) {
	rooms := h.sfu.ListRooms()
	summaries := make([]*mediav1.RoomSummary, 0, len(rooms))
	for _, r := range rooms {
		summaries = append(summaries, &mediav1.RoomSummary{
			RoomId:    r.ID(),
			PeerCount: int32(r.PeerCount()),
			MaxPeers:  int32(r.MaxPeers()),
			CreatedAt: timestamppb.New(r.CreatedAt()),
		})
	}

	return connect.NewResponse(&mediav1.ListRoomsResponse{Rooms: summaries}), nil
}

func (h *MediaHandler) CloseRoom(_ context.Context, req *connect.Request[mediav1.CloseRoomRequest]) (*connect.Response[mediav1.CloseRoomResponse], error) {
	if err := h.sfu.CloseRoom(req.Msg.RoomId); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&mediav1.CloseRoomResponse{}), nil
}

func (h *MediaHandler) JoinRoom(ctx context.Context, req *connect.Request[mediav1.JoinRoomRequest]) (*connect.Response[mediav1.JoinRoomResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	// Build PeerConfig from request fields.
	// publish_audio defaults to true (proto3 zero-value false means "use default").
	publishAudio := true
	if req.Msg.PublishAudio {
		publishAudio = true
	}
	// auto_subscribe_audio defaults to true.
	autoSubscribeAudio := true
	if req.Msg.AutoSubscribeAudio {
		autoSubscribeAudio = true
	}

	var enc *sfu.EncryptionInfo
	if req.Msg.Encryption != nil {
		enc = &sfu.EncryptionInfo{
			Algorithm: req.Msg.Encryption.Algorithm.String(),
			KeyID:     req.Msg.Encryption.KeyId,
			SenderKey: req.Msg.Encryption.SenderKey,
		}
	}

	pcfg := sfu.PeerConfig{
		PublishAudio:       publishAudio,
		PublishVideo:       req.Msg.PublishVideo,
		Simulcast:          req.Msg.Simulcast,
		Encryption:         enc,
		AutoSubscribeAudio: autoSubscribeAudio,
	}

	peer, err := sfu.NewPeer(ctx, req.Msg.PeerId, room, h.sfu.API(), h.sfu.Config(), req.Msg.Metadata, pcfg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	availableTracks, err := room.AddPeer(peer)
	if err != nil {
		peer.Close()
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	}

	answerSDP, err := peer.HandleOffer(req.Msg.SdpOffer)
	if err != nil {
		room.RemovePeer(peer.ID())
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Notify orchestrator about the new peer via worker pool.
	if h.onPeerJoined != nil {
		fn := h.onPeerJoined
		roomID := req.Msg.RoomId
		peerID := peer.ID()
		metadata := req.Msg.Metadata
		if h.pool != nil {
			if err := h.pool.Submit(ctx, func() { fn(ctx, roomID, peerID, metadata) }); err != nil {
				slog.WarnContext(ctx, "peer joined pool full", slog.String("peer_id", peerID))
			}
		} else {
			go fn(ctx, roomID, peerID, metadata)
		}
	}

	// Convert available tracks to proto.
	protoTracks := publisherTracksToProto(availableTracks)

	return connect.NewResponse(&mediav1.JoinRoomResponse{
		SdpAnswer: answerSDP,
		SessionInfo: &commonv1.SessionInfo{
			SessionId: peer.ID(),
			RoomId:    room.ID(),
			PeerId:    peer.ID(),
			Protocol:  "webrtc",
			Metadata:  req.Msg.Metadata,
		},
		AvailableTracks: protoTracks,
	}), nil
}

func (h *MediaHandler) LeaveRoom(_ context.Context, req *connect.Request[mediav1.LeaveRoomRequest]) (*connect.Response[mediav1.LeaveRoomResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	room.RemovePeer(req.Msg.PeerId)
	return connect.NewResponse(&mediav1.LeaveRoomResponse{}), nil
}

func (h *MediaHandler) TrickleICE(_ context.Context, req *connect.Request[mediav1.TrickleICERequest]) (*connect.Response[mediav1.TrickleICEResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	peer, ok := room.GetPeer(req.Msg.PeerId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("peer %q not found", req.Msg.PeerId))
	}

	if err := peer.AddICECandidate(req.Msg.CandidateJson); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	return connect.NewResponse(&mediav1.TrickleICEResponse{}), nil
}

func (h *MediaHandler) SubscribeTrack(_ context.Context, req *connect.Request[mediav1.SubscribeTrackRequest]) (*connect.Response[mediav1.SubscribeTrackResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	quality := protoQualityToSFU(req.Msg.Quality)
	sub, err := room.Subscribe(req.Msg.PeerId, req.Msg.TrackId, quality, int(req.Msg.MaxTemporalLayer), int(req.Msg.MaxSpatialLayer))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&mediav1.SubscribeTrackResponse{
		Subscription: subscriptionToProto(sub),
	}), nil
}

func (h *MediaHandler) UnsubscribeTrack(_ context.Context, req *connect.Request[mediav1.UnsubscribeTrackRequest]) (*connect.Response[mediav1.UnsubscribeTrackResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	if err := room.Unsubscribe(req.Msg.PeerId, req.Msg.TrackId); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&mediav1.UnsubscribeTrackResponse{}), nil
}

func (h *MediaHandler) UpdateSubscription(_ context.Context, req *connect.Request[mediav1.UpdateSubscriptionRequest]) (*connect.Response[mediav1.UpdateSubscriptionResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	quality := protoQualityToSFU(req.Msg.Quality)
	if err := room.UpdateSubscription(req.Msg.PeerId, req.Msg.TrackId, quality, int(req.Msg.MaxTemporalLayer), int(req.Msg.MaxSpatialLayer), req.Msg.Paused); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&mediav1.UpdateSubscriptionResponse{}), nil
}

func (h *MediaHandler) ListTracks(_ context.Context, req *connect.Request[mediav1.ListTracksRequest]) (*connect.Response[mediav1.ListTracksResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	tracks := room.ListPublisherTracks()
	return connect.NewResponse(&mediav1.ListTracksResponse{
		Tracks: publisherTracksToProto(tracks),
	}), nil
}

func (h *MediaHandler) SubscribeActiveSpeakers(ctx context.Context, req *connect.Request[mediav1.SubscribeActiveSpeakersRequest], stream *connect.ServerStream[mediav1.ActiveSpeakersMessage]) error {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	speakerCh := make(chan []sfu.ActiveSpeakerInfo, 16)
	listenerID := xid.New().String()

	room.AddSpeakerListener(listenerID, func(speakers []sfu.ActiveSpeakerInfo) {
		select {
		case speakerCh <- speakers:
		default:
		}
	})
	defer room.RemoveSpeakerListener(listenerID)

	for {
		select {
		case <-ctx.Done():
			return nil
		case speakers := <-speakerCh:
			protoSpeakers := make([]*mediav1.ActiveSpeaker, 0, len(speakers))
			for _, s := range speakers {
				protoSpeakers = append(protoSpeakers, &mediav1.ActiveSpeaker{
					PeerId:        s.PeerID,
					AudioLevel:    float32(s.AudioLevel),
					VoiceActivity: s.VoiceActivity,
				})
			}
			if err := stream.Send(&mediav1.ActiveSpeakersMessage{
				Speakers: protoSpeakers,
			}); err != nil {
				return err
			}
		}
	}
}

func (h *MediaHandler) Renegotiate(_ context.Context, req *connect.Request[mediav1.RenegotiateRequest]) (*connect.Response[mediav1.RenegotiateResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	peer, ok := room.GetPeer(req.Msg.PeerId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("peer %q not found", req.Msg.PeerId))
	}

	answerSDP, err := peer.Renegotiate(req.Msg.SdpOffer)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&mediav1.RenegotiateResponse{
		SdpAnswer: answerSDP,
	}), nil
}

func (h *MediaHandler) PlayAudio(_ context.Context, stream *connect.ClientStream[mediav1.PlayAudioRequest]) (*connect.Response[mediav1.PlayAudioResponse], error) {
	var framesPlayed int64
	var roomID string

	for stream.Receive() {
		msg := stream.Msg()
		if roomID == "" {
			roomID = msg.RoomId
		}

		room, ok := h.sfu.GetRoom(msg.RoomId)
		if !ok {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", msg.RoomId))
		}

		if msg.Frame != nil {
			room.InjectAudio("system-tts", msg.Frame.Data, msg.Frame.Codec)
			framesPlayed++
		}
	}

	if err := stream.Err(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&mediav1.PlayAudioResponse{
		FramesPlayed: framesPlayed,
	}), nil
}

func (h *MediaHandler) SubscribeAudio(ctx context.Context, req *connect.Request[mediav1.SubscribeAudioRequest], stream *connect.ServerStream[mediav1.AudioStreamMessage]) error {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	frameCh := make(chan audioFrame, 64)
	peerFilter := req.Msg.PeerId

	tapID := xid.New().String()
	room.AddAudioTap(tapID, func(peerID string, frame []byte, codec string) {
		if peerFilter != "" && peerID != peerFilter {
			return
		}
		select {
		case frameCh <- audioFrame{peerID: peerID, data: frame, codec: codec}:
		default:
		}
	})
	defer room.RemoveAudioTap(tapID)

	for {
		select {
		case <-ctx.Done():
			return nil
		case f := <-frameCh:
			if err := stream.Send(&mediav1.AudioStreamMessage{
				Frame: &commonv1.AudioFrame{
					Data:       f.data,
					Codec:      f.codec,
					SampleRate: 48000,
					Channels:   1,
				},
				PeerId: f.peerID,
			}); err != nil {
				return err
			}
		}
	}
}

type audioFrame struct {
	peerID string
	data   []byte
	codec  string
}

func (h *MediaHandler) CreateSIPBridge(ctx context.Context, req *connect.Request[mediav1.CreateSIPBridgeRequest]) (*connect.Response[mediav1.CreateSIPBridgeResponse], error) {
	room, ok := h.sfu.GetRoom(req.Msg.RoomId)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("room %q not found", req.Msg.RoomId))
	}

	bridge, err := sipbridge.NewSIPBridge(ctx, room, req.Msg.SipUri, h.sfu)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&mediav1.CreateSIPBridgeResponse{
		BridgePeerId: bridge.PeerID(),
	}), nil
}

// --- Helpers ---

func publisherTracksToProto(tracks []*sfu.PublisherTrack) []*mediav1.TrackInfo {
	result := make([]*mediav1.TrackInfo, 0, len(tracks))
	for _, pt := range tracks {
		info := pt.Info()
		kind := mediav1.TrackKind_TRACK_KIND_AUDIO
		if info.Kind == webrtc.RTPCodecTypeVideo {
			kind = mediav1.TrackKind_TRACK_KIND_VIDEO
		}
		result = append(result, &mediav1.TrackInfo{
			TrackId:   info.ID,
			PeerId:    info.PeerID,
			Kind:      kind,
			MimeType:  info.MimeType,
			Simulcast: info.Simulcast,
			Svc:       info.SVC,
		})
	}
	return result
}

func subscriptionToProto(sub *sfu.Subscription) *mediav1.SubscriptionInfo {
	info := sub.Info()
	return &mediav1.SubscriptionInfo{
		SubscriptionId:  info.ID,
		TrackId:         info.TrackID,
		PublisherPeerId: info.PublisherPeerID,
		Quality:         sfuQualityToProto(info.Quality),
		MaxTemporalLayer: int32(info.MaxTemporalLayer),
		MaxSpatialLayer:  int32(info.MaxSpatialLayer),
		Paused:          info.Paused,
	}
}

func protoQualityToSFU(q mediav1.VideoQuality) sfu.VideoQualityLevel {
	switch q {
	case mediav1.VideoQuality_VIDEO_QUALITY_LOW:
		return sfu.QualityLow
	case mediav1.VideoQuality_VIDEO_QUALITY_MEDIUM:
		return sfu.QualityMedium
	case mediav1.VideoQuality_VIDEO_QUALITY_HIGH:
		return sfu.QualityHigh
	default:
		return sfu.QualityHigh
	}
}

func sfuQualityToProto(q sfu.VideoQualityLevel) mediav1.VideoQuality {
	switch q {
	case sfu.QualityLow:
		return mediav1.VideoQuality_VIDEO_QUALITY_LOW
	case sfu.QualityMedium:
		return mediav1.VideoQuality_VIDEO_QUALITY_MEDIUM
	case sfu.QualityHigh:
		return mediav1.VideoQuality_VIDEO_QUALITY_HIGH
	default:
		return mediav1.VideoQuality_VIDEO_QUALITY_UNSPECIFIED
	}
}
