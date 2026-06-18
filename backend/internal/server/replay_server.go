package server

import (
	"context"
	"time"

	pb "github.com/ecscard/game/internal/proto"
	"github.com/ecscard/game/internal/replay"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ReplayServer struct {
	pb.UnimplementedReplayServiceServer
	specMgr     *replay.SpectatorManager
	replayStore *replay.ReplayStore
}

func NewReplayServer(replayStore *replay.ReplayStore, specMgr *replay.SpectatorManager) *ReplayServer {
	return &ReplayServer{
		specMgr:     specMgr,
		replayStore: replayStore,
	}
}

func (s *ReplayServer) JoinAsSpectator(req *pb.SpectatorJoinRequest, stream pb.ReplayService_JoinAsSpectatorServer) error {
	gameID := req.GameId
	specID := req.SpectatorId
	specName := req.SpectatorName

	if gameID == "" {
		return status.Errorf(codes.InvalidArgument, "game_id is required")
	}
	if specID == "" {
		return status.Errorf(codes.InvalidArgument, "spectator_id is required")
	}

	recorder, ok := s.replayStore.GetLiveRecorder(gameID)
	if !ok {
		_, err := s.replayStore.GetReplayMeta(stream.Context(), gameID)
		if err != nil {
			return status.Errorf(codes.NotFound, "game not found: %v", err)
		}
	}

	session, err := s.specMgr.JoinSpectator(gameID, specID, specName, recorder)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to join spectator: %v", err)
	}
	defer s.specMgr.LeaveSpectator(gameID, specID)

	go s.specMgr.StreamLoop(stream.Context(), gameID, specID)

	ctx := stream.Context()
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-session.Stream:
			if !ok {
				return nil
			}
			if err := stream.Send(frame); err != nil {
				return err
			}
		case <-heartbeatTicker.C:
			heartbeatFrame := &pb.SpectatorFrame{
				ServerTimeMs:   time.Now().UnixNano() / 1e6,
				StreamTimeMs:   session.CurrentTimeMs,
				SpectatorCount: recorder.GetSpectatorCount(),
				Phase:          pb.ReplayPhase_REPLAY_PHASE_LIVE,
			}
			if !recorder.IsDelayedStreamAvailable() {
				heartbeatFrame.Phase = pb.ReplayPhase_REPLAY_PHASE_WAITING
			}
			if err := stream.Send(heartbeatFrame); err != nil {
				return err
			}
		}
	}
}

func (s *ReplayServer) SeekTo(ctx context.Context, req *pb.SeekRequest) (*pb.SeekResponse, error) {
	gameID := req.GameId
	if gameID == "" {
		return &pb.SeekResponse{Success: false}, status.Errorf(codes.InvalidArgument, "game_id is required")
	}

	recorder, ok := s.replayStore.GetLiveRecorder(gameID)
	if !ok {
		return &pb.SeekResponse{Success: false}, status.Errorf(codes.NotFound, "live game not found")
	}

	session, exists := s.specMgr.GetSession(gameID, "")
	if !exists {
		_ = session
	}

	resp, err := s.specMgr.SeekTo(gameID, "", req.TargetTimeMs, req.TargetFrame, recorder)
	if err != nil {
		return &pb.SeekResponse{Success: false}, nil
	}
	return resp, nil
}

func (s *ReplayServer) SetPlaybackSpeed(ctx context.Context, req *pb.SetPlaybackSpeedRequest) (*pb.SetPlaybackSpeedResponse, error) {
	gameID := req.GameId
	specID := req.SpectatorId

	if gameID == "" {
		return &pb.SetPlaybackSpeedResponse{Success: false}, status.Errorf(codes.InvalidArgument, "game_id is required")
	}
	if specID == "" {
		return &pb.SetPlaybackSpeedResponse{Success: false}, status.Errorf(codes.InvalidArgument, "spectator_id is required")
	}

	err := s.specMgr.SetPlaybackSpeed(gameID, specID, req.Speed)
	if err != nil {
		return &pb.SetPlaybackSpeedResponse{Success: false}, nil
	}

	session, ok := s.specMgr.GetSession(gameID, specID)
	if !ok {
		return &pb.SetPlaybackSpeedResponse{
			Success:      true,
			CurrentSpeed: req.Speed,
		}, nil
	}

	return &pb.SetPlaybackSpeedResponse{
		Success:      true,
		CurrentSpeed: session.PlaybackSpeed,
	}, nil
}

func (s *ReplayServer) GetReplayMeta(ctx context.Context, req *pb.GetReplayMetaRequest) (*pb.ReplayMeta, error) {
	gameID := req.GameId
	if gameID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "game_id is required")
	}

	recorder, ok := s.replayStore.GetLiveRecorder(gameID)
	if ok {
		return recorder.GetMeta(), nil
	}

	doc, err := s.replayStore.GetReplayMeta(ctx, gameID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "replay not found: %v", err)
	}

	phase := pb.ReplayPhase_REPLAY_PHASE_FINISHED
	if doc.IsLive {
		phase = pb.ReplayPhase_REPLAY_PHASE_LIVE
	}

	return &pb.ReplayMeta{
		GameId:           doc.GameID,
		MatchId:          doc.MatchID,
		Player1Id:        doc.Player1ID,
		Player1Name:      doc.Player1Name,
		Player2Id:        doc.Player2ID,
		Player2Name:      doc.Player2Name,
		StartTimeUnixMs:  doc.StartTimeMs,
		EndTimeUnixMs:    doc.EndTimeMs,
		TotalTurns:       doc.TotalTurns,
		WinnerId:         doc.WinnerID,
		DurationMs:       doc.DurationMs,
		SpectatorDelayMs: doc.SpectatorDelayMs,
		IsLive:           doc.IsLive,
		Phase:            phase,
		TotalActions:     doc.TotalActions,
	}, nil
}

func (s *ReplayServer) GetReplaySegment(ctx context.Context, req *pb.GetReplaySegmentRequest) (*pb.ReplaySegment, error) {
	gameID := req.GameId
	if gameID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "game_id is required")
	}

	recorder, ok := s.replayStore.GetLiveRecorder(gameID)
	if ok {
		return recorder.GetSegment(req.FromTimeMs, req.ToTimeMs, req.FromIndex, req.ToIndex), nil
	}

	fromIdx := req.FromIndex
	toIdx := req.ToIndex
	if fromIdx < 0 {
		fromIdx = 0
	}
	if toIdx <= 0 {
		toIdx = 10000
	}

	docs, err := s.replayStore.GetReplayActions(ctx, gameID, fromIdx, toIdx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get replay actions: %v", err)
	}

	actions := make([]*pb.ReplayAction, 0, len(docs))
	for _, doc := range docs {
		action := &pb.Action{}
		_ = action.Unmarshal(doc.ActionData)
		stateAfter := &pb.GameStatus{}
		_ = stateAfter.Unmarshal(doc.StateAfter)
		events := make([]*pb.GameEvent, 0, len(doc.EventsData))
		for _, evData := range doc.EventsData {
			ev := &pb.GameEvent{}
			_ = ev.Unmarshal(evData)
			events = append(events, ev)
		}
		actions = append(actions, &pb.ReplayAction{
			RelativeTimeMs: doc.RelativeMs,
			FrameNumber:    doc.FrameNumber,
			Action:         action,
			StateAfter:     stateAfter,
			Events:         events,
		})
	}

	actualStartMs := int64(0)
	actualEndMs := int64(0)
	if len(actions) > 0 {
		actualStartMs = actions[0].RelativeTimeMs
		actualEndMs = actions[len(actions)-1].RelativeTimeMs
	}

	return &pb.ReplaySegment{
		GameId:      gameID,
		StartTimeMs: actualStartMs,
		EndTimeMs:   actualEndMs,
		StartIndex:  fromIdx,
		EndIndex:    fromIdx + int32(len(actions)),
		Actions:     actions,
		HasMore:     len(actions) == int(toIdx-fromIdx+1),
	}, nil
}

func (s *ReplayServer) ListLiveGames(ctx context.Context, req *pb.ListLiveGamesRequest) (*pb.ListLiveGamesResponse, error) {
	page := req.Page
	pageSize := req.PageSize
	gameType := req.GameType

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	entries, total, err := s.replayStore.ListLiveGames(ctx, gameType, page, pageSize)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list live games: %v", err)
	}

	games := make([]*pb.LiveGameInfo, 0, len(entries))
	for _, entry := range entries {
		games = append(games, &pb.LiveGameInfo{
			GameId:           entry.GameID,
			Player1Name:      entry.Player1Name,
			Player2Name:      entry.Player2Name,
			Player1Health:    entry.Player1Health,
			Player2Health:    entry.Player2Health,
			CurrentTurn:      entry.CurrentTurn,
			SpectatorCount:   entry.SpectatorCount,
			SpectatorDelayMs: entry.SpectatorDelayMs,
			ElapsedMs:        entry.ElapsedMs,
			GameType:         entry.GameType,
		})
	}

	return &pb.ListLiveGamesResponse{
		Games: games,
		Total: total,
	}, nil
}
