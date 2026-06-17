package server

import (
	"context"
	"time"

	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game"
	pb "github.com/ecscard/game/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GameServer struct {
	pb.UnimplementedGameServiceServer
	gameManager *game.GameManager
	serverAddr  string
}

func NewGameServer(redisAddr, mongoURI, serverAddr string) (*GameServer, error) {
	gm, err := game.NewGameManager(redisAddr, mongoURI)
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
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create game: %v", err)
	}

	return &pb.StartGameResponse{
		GameId:       gameInstance.GameID,
		InitialState: gameInstance.GetGameState(req.Player1Id),
	}, nil
}

func (s *GameServer) PlayCard(ctx context.Context, req *pb.PlayCardRequest) (*pb.PlayCardResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	success := gameInstance.PlayCard(req.PlayerId, ecs.EntityID(req.CardEntityId), ecs.EntityID(req.TargetEntityId))
	if !success {
		return &pb.PlayCardResponse{
			Success: false,
			Message: "failed to play card",
		}, nil
	}

	return &pb.PlayCardResponse{
		Success: true,
		Message: "card played successfully",
	}, nil
}

func (s *GameServer) Attack(ctx context.Context, req *pb.AttackRequest) (*pb.AttackResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	success := gameInstance.Attack(req.PlayerId, ecs.EntityID(req.AttackerEntityId), ecs.EntityID(req.TargetEntityId))
	if !success {
		return &pb.AttackResponse{
			Success: false,
			Message: "failed to attack",
		}, nil
	}

	return &pb.AttackResponse{
		Success: true,
		Message: "attack successful",
	}, nil
}

func (s *GameServer) EndTurn(ctx context.Context, req *pb.EndTurnRequest) (*pb.EndTurnResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	success := gameInstance.EndTurn(req.PlayerId)
	if !success {
		return &pb.EndTurnResponse{
			Success: false,
			Message: "failed to end turn",
		}, nil
	}

	return &pb.EndTurnResponse{
		Success: true,
		Message: "turn ended successfully",
	}, nil
}

func (s *GameServer) Concede(ctx context.Context, req *pb.ConcedeRequest) (*pb.ConcedeResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	success := gameInstance.Concede(req.PlayerId)
	if !success {
		return &pb.ConcedeResponse{
			Success: false,
			Message: "failed to concede",
		}, nil
	}

	return &pb.ConcedeResponse{
		Success: true,
		Message: "conceded successfully",
	}, nil
}

func (s *GameServer) GetGameState(ctx context.Context, req *pb.GetGameStateRequest) (*pb.GetGameStateResponse, error) {
	gameInstance, ok := s.gameManager.GetGame(req.GameId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "game not found")
	}

	return &pb.GetGameStateResponse{
		State: gameInstance.GetGameState(req.PlayerId),
	}, nil
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
			return nil
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

func (s *GameServer) SendAction(stream pb.GameService_SendActionServer) error {
	go func() {
		for {
			action, err := stream.Recv()
			if err != nil {
				return
			}

			if action.Type == pb.ActionType_ACTION_TYPE_PLAY_CARD {
				req := &pb.PlayCardRequest{
					GameId:         action.PlayerId,
					PlayerId:       action.PlayerId,
					CardEntityId:   action.CardId,
					TargetEntityId: action.TargetId,
				}
				s.PlayCard(context.Background(), req)
			} else if action.Type == pb.ActionType_ACTION_TYPE_ATTACK {
				req := &pb.AttackRequest{
					GameId:           action.PlayerId,
					PlayerId:         action.PlayerId,
					AttackerEntityId: action.CardId,
					TargetEntityId:   action.TargetId,
				}
				s.Attack(context.Background(), req)
			} else if action.Type == pb.ActionType_ACTION_TYPE_END_TURN {
				req := &pb.EndTurnRequest{
					GameId:   action.PlayerId,
					PlayerId: action.PlayerId,
				}
				s.EndTurn(context.Background(), req)
			} else if action.Type == pb.ActionType_ACTION_TYPE_CONCEDE {
				req := &pb.ConcedeRequest{
					GameId:   action.PlayerId,
					PlayerId: action.PlayerId,
				}
				s.Concede(context.Background(), req)
			}
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		select {
		case <-stream.Context().Done():
			return nil
		default:
		}
	}
}

func (s *GameServer) Close() {
	s.gameManager.Close()
}
