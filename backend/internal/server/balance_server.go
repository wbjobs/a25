package server

import (
	"context"

	"github.com/ecscard/game/internal/balance"
	pb "github.com/ecscard/game/internal/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type BalanceServer struct {
	pb.UnimplementedBalanceServiceServer
	collector   *balance.StatsCollector
	hotMgr      *balance.HotUpdateManager
	changeStore *balance.ChangeLogStore
}

func NewBalanceServer(collector *balance.StatsCollector, hotMgr *balance.HotUpdateManager, changeStore *balance.ChangeLogStore) *BalanceServer {
	return &BalanceServer{
		collector:   collector,
		hotMgr:      hotMgr,
		changeStore: changeStore,
	}
}

func (s *BalanceServer) GetBalanceStats(ctx context.Context, req *pb.GetBalanceStatsRequest) (*pb.GetBalanceStatsResponse, error) {
	timeRangeDays := req.TimeRangeDays
	if timeRangeDays <= 0 {
		timeRangeDays = 7
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}

	sortBy := req.SortBy
	if sortBy == "" {
		sortBy = "win_rate"
	}

	sortOrder := req.SortOrder
	if sortOrder == "" {
		sortOrder = "desc"
	}

	resp, err := s.collector.ComputeStats(
		timeRangeDays,
		req.TemplateIds,
		req.GameType,
		req.MinRank,
		req.MaxRank,
		sortBy,
		sortOrder,
		page,
		pageSize,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to compute stats: %v", err)
	}

	return resp, nil
}

func (s *BalanceServer) GetBalanceHistory(ctx context.Context, req *pb.GetBalanceHistoryRequest) (*pb.GetBalanceHistoryResponse, error) {
	templateID := req.TemplateId
	limit := req.Limit
	fromMs := req.FromUnixMs
	toMs := req.ToUnixMs

	changes, err := s.hotMgr.GetChangeHistory(templateID, limit, fromMs, toMs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get change history: %v", err)
	}

	return &pb.GetBalanceHistoryResponse{
		Changes: changes,
	}, nil
}

func (s *BalanceServer) GetActiveOverrides(ctx context.Context, req *pb.GetActiveOverridesRequest) (*pb.GetActiveOverridesResponse, error) {
	templateID := req.TemplateId

	overrides := s.hotMgr.GetActiveOverrides(templateID)

	return &pb.GetActiveOverridesResponse{
		Overrides: overrides,
	}, nil
}

func (s *BalanceServer) HotUpdateCard(ctx context.Context, req *pb.HotUpdateRequest) (*pb.HotUpdateResponse, error) {
	templateID := req.TemplateId
	if templateID == "" {
		return &pb.HotUpdateResponse{Success: false}, status.Errorf(codes.InvalidArgument, "template_id is required")
	}
	if len(req.NumericOverrides) == 0 {
		return &pb.HotUpdateResponse{Success: false}, status.Errorf(codes.InvalidArgument, "numeric_overrides is required")
	}

	changeLog, err := s.hotMgr.ApplyHotUpdate(
		templateID,
		req.NumericOverrides,
		req.ChangeReason,
		req.ChangedBy,
		req.Immediate,
	)
	if err != nil {
		return &pb.HotUpdateResponse{Success: false}, status.Errorf(codes.Internal, "failed to apply hot update: %v", err)
	}

	effectiveTime := int64(0)
	if changeLog != nil {
		effectiveTime = changeLog.TimestampUnixMs
	}

	return &pb.HotUpdateResponse{
		Success:             true,
		ChangeId:            changeLog.ChangeId,
		TemplateId:          templateID,
		EffectiveTimeUnixMs: effectiveTime,
		ChangeLog:           changeLog,
	}, nil
}

func (s *BalanceServer) RevertHotUpdate(ctx context.Context, req *pb.RevertHotUpdateRequest) (*pb.RevertHotUpdateResponse, error) {
	changeID := req.ChangeId
	if changeID == "" {
		return &pb.RevertHotUpdateResponse{Success: false}, status.Errorf(codes.InvalidArgument, "change_id is required")
	}

	revertChangeID, err := s.hotMgr.RevertHotUpdate(changeID, req.RevertedBy)
	if err != nil {
		return &pb.RevertHotUpdateResponse{Success: false}, status.Errorf(codes.Internal, "failed to revert hot update: %v", err)
	}

	return &pb.RevertHotUpdateResponse{
		Success:        true,
		RevertChangeId: revertChangeID,
	}, nil
}

func (s *BalanceServer) GetTierDistribution(ctx context.Context, req *pb.GetTierDistributionRequest) (*pb.GetTierDistributionResponse, error) {
	timeRangeDays := req.TimeRangeDays
	if timeRangeDays <= 0 {
		timeRangeDays = 7
	}

	resp, err := s.collector.ComputeTierDistribution(timeRangeDays)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to compute tier distribution: %v", err)
	}

	return resp, nil
}
