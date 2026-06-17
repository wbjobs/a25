package server

import (
	"context"

	"github.com/ecscard/game/internal/matchmaking"
	pb "github.com/ecscard/game/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MatchServer struct {
	pb.UnimplementedMatchServiceServer
	matchmaker *matchmaking.Matchmaker
}

func NewMatchServer(redisAddr, mongoURI, gameServerAddr string, gameClient pb.GameServiceClient) (*MatchServer, error) {
	mm, err := matchmaking.NewMatchmaker(redisAddr, mongoURI, gameServerAddr, gameClient)
	if err != nil {
		return nil, err
	}

	return &MatchServer{
		matchmaker: mm,
	}, nil
}

func (s *MatchServer) FindMatch(req *pb.MatchRequest, stream pb.MatchService_FindMatchServer) error {
	ch, err := s.matchmaker.FindMatch(req.Player, req.GameType)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to join queue: %v", err)
	}

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			s.matchmaker.CancelMatch(req.Player.PlayerId, "")
			return nil
		case resp, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(resp); err != nil {
				s.matchmaker.CancelMatch(req.Player.PlayerId, resp.MatchId)
				return err
			}
			if resp.Status == pb.MatchStatus_MATCH_STATUS_IN_GAME ||
				resp.Status == pb.MatchStatus_MATCH_STATUS_CANCELLED ||
				resp.Status == pb.MatchStatus_MATCH_STATUS_FINISHED {
				return nil
			}
		}
	}
}

func (s *MatchServer) CancelMatch(ctx context.Context, req *pb.CancelMatchRequest) (*pb.CancelMatchResponse, error) {
	success := s.matchmaker.CancelMatch(req.PlayerId, req.MatchId)
	if !success {
		return &pb.CancelMatchResponse{
			Success: false,
			Message: "not in queue or match not found",
		}, nil
	}

	return &pb.CancelMatchResponse{
		Success: true,
		Message: "cancelled successfully",
	}, nil
}

func (s *MatchServer) GetMatchStatus(ctx context.Context, req *pb.GetMatchStatusRequest) (*pb.GetMatchStatusResponse, error) {
	resp, err := s.matchmaker.GetMatchStatus(req.PlayerId, req.MatchId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get match status: %v", err)
	}
	if resp == nil {
		return nil, status.Errorf(codes.NotFound, "match not found")
	}
	return resp, nil
}

func (s *MatchServer) SubmitMatchResult(ctx context.Context, req *pb.SubmitMatchResultRequest) (*pb.SubmitMatchResultResponse, error) {
	err := s.matchmaker.SubmitMatchResult(req.Result)
	if err != nil {
		return &pb.SubmitMatchResultResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &pb.SubmitMatchResultResponse{
		Success: true,
		Message: "result submitted successfully",
	}, nil
}

func (s *MatchServer) GetPlayerStats(ctx context.Context, req *pb.GetPlayerStatsRequest) (*pb.GetPlayerStatsResponse, error) {
	resp, err := s.matchmaker.GetPlayerStats(req.PlayerId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get player stats: %v", err)
	}
	return resp, nil
}

func (s *MatchServer) Close() {
	s.matchmaker.Close()
}
