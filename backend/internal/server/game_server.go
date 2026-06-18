package server

import (
	"context"
	"io"
	"time"

	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game"
	pb "github.com/ecscard/game/internal/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GameServer struct {
	pb.UnimplementedGameServiceServer
	gameManager *game.GameManager
	serverAddr  string
}

func NewGameServer(redisAddr, mongoURI, serverAddr string, useCluster bool, clusterAddrs []string) (*GameServer, error) {
	gm, err := game.NewGameManager(redisAddr, mongoURI, useCluster, clusterAddrs)
	if err != nil {
		return nil, err
	}

	return &GameServer{
		gameManager: gm,
		serverAddr:  serverAddr,
	}, nil
}

func (s *GameServer) StartGame(ctx context.Context, req *pb.StartGameRequest) (*pb.StartGameResponse, error) {
	gameType := req.GameType
	if gameType == "" {
		gameType = "normal"
	}

	gameInstance, err := s.gameManager.CreateGame(
		"",
		req.Player1Id,
		req.Player2Id,
		req.Player1Name,
		req.Player2Name,
		gameType,
		req.IsAiOpponent,
		req.AiDifficulty,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create game: %v", err)
	}

	resp := &pb.StartGameResponse{
		GameId:            gameInstance.GameID,
		InitialState:      gameInstance.GetGameState(req.Player1Id),
		InitialSnapshot:   gameInstance.GetCurrentSnapshot(),
		SnapshotIntervalMs: gameInstance.SnapshotInterval(),
		PlayerAssignedId:   req.Player1Id,
	}

	return resp, nil
}

func (s *GameServer) PlayCard(ctx context.Context, req *pb.PlayCardRequest) (*pb.PlayCardResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	seq := req.Sequence
	if seq == 0 {
		seq = gameInstance.GetPlayerNextSeq(req.PlayerId)
	}

	success, rollbackSnap := gameInstance.PlayCard(
		req.PlayerId,
		ecs.EntityID(req.CardEntityId),
		ecs.EntityID(req.TargetEntityId),
		req.BaseSnapshotFrame,
		req.BaseSnapshotHash,
		seq,
	)

	resp := &pb.PlayCardResponse{
		Success:        success,
		ConfirmedFrame: gameInstance.GetLatestFrame(),
	}

	if !success {
		resp.Message = "operation rejected: invalid snapshot or sequence"
		if rollbackSnap != nil {
			resp.NeedsRollback = true
			resp.RollbackSnapshot = rollbackSnap
		}
	} else {
		resp.Message = "card played successfully"
	}

	return resp, nil
}

func (s *GameServer) Attack(ctx context.Context, req *pb.AttackRequest) (*pb.AttackResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	seq := req.Sequence
	if seq == 0 {
		seq = gameInstance.GetPlayerNextSeq(req.PlayerId)
	}

	success, rollbackSnap := gameInstance.Attack(
		req.PlayerId,
		ecs.EntityID(req.AttackerEntityId),
		ecs.EntityID(req.TargetEntityId),
		req.BaseSnapshotFrame,
		req.BaseSnapshotHash,
		seq,
	)

	resp := &pb.AttackResponse{
		Success:        success,
		ConfirmedFrame: gameInstance.GetLatestFrame(),
	}

	if !success {
		resp.Message = "operation rejected: invalid snapshot or sequence"
		if rollbackSnap != nil {
			resp.NeedsRollback = true
			resp.RollbackSnapshot = rollbackSnap
		}
	} else {
		resp.Message = "attack successful"
	}

	return resp, nil
}

func (s *GameServer) EndTurn(ctx context.Context, req *pb.EndTurnRequest) (*pb.EndTurnResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	seq := req.Sequence
	if seq == 0 {
		seq = gameInstance.GetPlayerNextSeq(req.PlayerId)
	}

	success, rollbackSnap := gameInstance.EndTurn(
		req.PlayerId,
		req.BaseSnapshotFrame,
		req.BaseSnapshotHash,
		seq,
	)

	resp := &pb.EndTurnResponse{
		Success:        success,
		ConfirmedFrame: gameInstance.GetLatestFrame(),
	}

	if !success {
		resp.Message = "operation rejected"
		if rollbackSnap != nil {
			resp.NeedsRollback = true
			resp.RollbackSnapshot = rollbackSnap
		}
	} else {
		resp.Message = "turn ended"
	}

	return resp, nil
}

func (s *GameServer) Concede(ctx context.Context, req *pb.ConcedeRequest) (*pb.ConcedeResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	seq := req.Sequence
	if seq == 0 {
		seq = gameInstance.GetPlayerNextSeq(req.PlayerId)
	}

	success := gameInstance.Concede(req.PlayerId, seq)
	if !success {
		return &pb.ConcedeResponse{
			Success: false,
			Message: "failed to concede",
		}, nil
	}

	s.gameManager.MarkGameEnded(req.GameId, gameInstance.GetWinner(), gameInstance.GetTurns(), gameInstance.DurationMs)

	return &pb.ConcedeResponse{
		Success: true,
		Message: "conceded",
	}, nil
}

func (s *GameServer) GetGameState(ctx context.Context, req *pb.GetGameStateRequest) (*pb.GetGameStateResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	return &pb.GetGameStateResponse{
		State:          gameInstance.GetGameState(req.PlayerId),
		LatestSnapshot: gameInstance.GetCurrentSnapshot(),
	}, nil
}

func (s *GameServer) GetSnapshot(ctx context.Context, req *pb.GetSnapshotRequest) (*pb.GetSnapshotResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	if req.FrameNumber == 0 {
		return &pb.GetSnapshotResponse{
			Found:        true,
			Snapshot:     gameInstance.GetCurrentSnapshot(),
			LatestFrame:  gameInstance.GetLatestFrame(),
		}, nil
	}

	snap, found := gameInstance.GetSnapshot(req.FrameNumber)
	return &pb.GetSnapshotResponse{
		Found:       found,
		Snapshot:    snap,
		LatestFrame: gameInstance.GetLatestFrame(),
	}, nil
}

func (s *GameServer) SendAck(ctx context.Context, req *pb.SendAckRequest) (*pb.SendAckResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	gameInstance.ReceiveAck(req.Ack)
	return &pb.SendAckResponse{Success: true}, nil
}

func (s *GameServer) StreamGame(req *pb.StreamGameRequest, stream pb.GameService_StreamGameServer) error {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return status.Errorf(codes.NotFound, "game not found")
	}

	ch := gameInstance.Subscribe(req.PlayerId)
	defer gameInstance.Unsubscribe(req.PlayerId)

	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(frame); err != nil {
				return err
			}
		}
	}
}

func (s *GameServer) SendActionStream(stream pb.GameService_SendActionStreamServer) error {
	ctx := stream.Context()
	var currentGameID string
	var currentPlayerID string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		action, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		currentPlayerID = action.PlayerId
		if currentGameID == "" {
			continue
		}

		gameInstance, ok := s.gameManager.GetGame(currentGameID)
		if !ok {
			continue
		}

		var respFrame *pb.GameFrame
		switch action.Type {
		case pb.ActionType_ACTION_TYPE_PLAY_CARD:
			_, _ = gameInstance.PlayCard(
				action.PlayerId,
				ecs.EntityID(action.CardId),
				ecs.EntityID(action.TargetId),
				action.BaseSnapshotFrame,
				action.BaseSnapshotHash,
				action.Sequence,
			)
		case pb.ActionType_ACTION_TYPE_ATTACK:
			_, _ = gameInstance.Attack(
				action.PlayerId,
				ecs.EntityID(action.CardId),
				ecs.EntityID(action.TargetId),
				action.BaseSnapshotFrame,
				action.BaseSnapshotHash,
				action.Sequence,
			)
		case pb.ActionType_ACTION_TYPE_END_TURN:
			_, _ = gameInstance.EndTurn(
				action.PlayerId,
				action.BaseSnapshotFrame,
				action.BaseSnapshotHash,
				action.Sequence,
			)
		case pb.ActionType_ACTION_TYPE_CONCEDE:
			_ = gameInstance.Concede(action.PlayerId, action.Sequence)
		}

		_ = respFrame

		ack := &pb.FrameAck{
			PlayerId:     action.PlayerId,
			FrameNumber:  gameInstance.GetLatestFrame(),
			ExpectedHash: 0,
			Timestamp:    time.Now().UnixNano() / int64(time.Millisecond),
		}
		gameInstance.ReceiveAck(ack)
	}
}

func (s *GameServer) Close() {
	if s.gameManager != nil {
		s.gameManager.Close()
	}
}
