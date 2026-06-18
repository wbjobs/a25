package balance

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	pb "github.com/ecscard/game/internal/proto"
)

const (
	DefaultTimeRangeDays = 7
	MinSampleSize        = 100
	TotalRankBins        = 5
	GameTypeRanked       = "ranked"
	GameTypeNormal       = "normal"
	GameTypeAll          = ""
	flushInterval        = 30 * time.Second
	maxBufferSize        = 500
	cacheTTL             = 10 * time.Minute
)

type CardUsageRecord struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	GameID        string             `bson:"game_id"`
	TemplateID    string             `bson:"template_id"`
	GameType      string             `bson:"game_type"`
	PlayerRank    int32              `bson:"player_rank"`
	Win           bool               `bson:"win"`
	PlayedInHand  bool               `bson:"played_in_hand"`
	PlayedOnBoard bool               `bson:"played_on_board"`
	Banned        bool               `bson:"banned"`
	TimestampMs   int64              `bson:"timestamp_ms"`
	TurnPlayed    int32              `bson:"turn_played"`
	Mulliganed    bool               `bson:"mulliganed"`
	KeptInOpening bool               `bson:"kept_in_opening"`
}

type CachedStats struct {
	Stats       map[string]*pb.CardBalanceStats
	GeneratedAt int64
	SampleSize  int64
	TimeRange   int32
	GameType    string
	MinRank     int32
	MaxRank     int32
}

type aggregationResult struct {
	TemplateID string `bson:"_id"`
	Total      int64  `bson:"total"`
	Wins       int64  `bson:"wins"`
	Bans       int64  `bson:"bans"`
	Played     int64  `bson:"played"`
}

type StatsCollector struct {
	mu            sync.RWMutex
	db            *mongo.Database
	usageCol      *mongo.Collection
	statsCol      *mongo.Collection
	cache         map[string]*CachedStats
	flushTicker   *time.Ticker
	buffer        []*CardUsageRecord
	bufferSize    int32
	bufferMu      sync.Mutex
	totalRecorded atomic.Int64
	hotManager    *HotUpdateManager
	done          chan struct{}
}

func NewStatsCollector(mongoURI, dbName string, hotMgr *HotUpdateManager) (*StatsCollector, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("connect mongo: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("ping mongo: %w", err)
	}

	db := client.Database(dbName)
	usageCol := db.Collection("card_usage")
	statsCol := db.Collection("balance_stats")

	_, err = usageCol.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "timestamp_ms", Value: -1}}},
		{Keys: bson.D{{Key: "template_id", Value: 1}}},
		{Keys: bson.D{{Key: "game_type", Value: 1}}},
		{Keys: bson.D{{Key: "player_rank", Value: 1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("create indexes: %w", err)
	}

	sc := &StatsCollector{
		db:          db,
		usageCol:    usageCol,
		statsCol:    statsCol,
		cache:       make(map[string]*CachedStats),
		flushTicker: time.NewTicker(flushInterval),
		buffer:      make([]*CardUsageRecord, 0, maxBufferSize),
		hotManager:  hotMgr,
		done:        make(chan struct{}),
	}

	go sc.flushLoop()

	return sc, nil
}

func (sc *StatsCollector) RecordCardUsage(gameID, templateID, gameType string, playerRank int32, win, playedOnBoard, banned bool) {
	record := &CardUsageRecord{
		GameID:        gameID,
		TemplateID:    templateID,
		GameType:      gameType,
		PlayerRank:    playerRank,
		Win:           win,
		PlayedOnBoard: playedOnBoard,
		Banned:        banned,
		TimestampMs:   time.Now().UnixMilli(),
		PlayedInHand:  true,
		TurnPlayed:    0,
		Mulliganed:    false,
		KeptInOpening: true,
	}

	sc.bufferMu.Lock()
	sc.buffer = append(sc.buffer, record)
	sc.bufferSize = int32(len(sc.buffer))
	shouldFlush := sc.bufferSize >= maxBufferSize
	sc.bufferMu.Unlock()

	sc.totalRecorded.Add(1)

	if shouldFlush {
		sc.FlushBuffer()
	}
}

func (sc *StatsCollector) RecordGameResult(gameID string, playerDecks map[string][]string, playerRanks map[string]int32, winnerID, gameType string) {
	now := time.Now().UnixMilli()

	sc.bufferMu.Lock()
	defer sc.bufferMu.Unlock()

	for playerID, deck := range playerDecks {
		rank := playerRanks[playerID]
		win := playerID == winnerID

		for _, templateID := range deck {
			record := &CardUsageRecord{
				GameID:        gameID,
				TemplateID:    templateID,
				GameType:      gameType,
				PlayerRank:    rank,
				Win:           win,
				PlayedInHand:  true,
				PlayedOnBoard: false,
				Banned:        false,
				TimestampMs:   now,
				TurnPlayed:    0,
				Mulliganed:    false,
				KeptInOpening: true,
			}
			sc.buffer = append(sc.buffer, record)
		}
	}

	sc.bufferSize = int32(len(sc.buffer))
	sc.totalRecorded.Add(int64(sc.bufferSize))

	if sc.bufferSize >= maxBufferSize {
		go sc.FlushBuffer()
	}
}

func (sc *StatsCollector) ComputeStats(timeRangeDays int32, templateIDs []string, gameType string, minRank, maxRank int32, sortBy, sortOrder string, page, pageSize int32) (*pb.GetBalanceStatsResponse, error) {
	if timeRangeDays <= 0 {
		timeRangeDays = DefaultTimeRangeDays
	}

	cacheKey := fmt.Sprintf("%d:%s:%d:%d", timeRangeDays, gameType, minRank, maxRank)

	sc.mu.RLock()
	cached, ok := sc.cache[cacheKey]
	sc.mu.RUnlock()

	now := time.Now().UnixMilli()
	if ok && now-cached.GeneratedAt < int64(cacheTTL/time.Millisecond) {
		return sc.buildResponseFromCache(cached, templateIDs, sortBy, sortOrder, page, pageSize)
	}

	return sc.computeAndCache(timeRangeDays, templateIDs, gameType, minRank, maxRank, sortBy, sortOrder, page, pageSize, cacheKey)
}

func (sc *StatsCollector) computeAndCache(timeRangeDays int32, templateIDs []string, gameType string, minRank, maxRank int32, sortBy, sortOrder string, page, pageSize int32, cacheKey string) (*pb.GetBalanceStatsResponse, error) {
	ctx := context.Background()
	nowMs := time.Now().UnixMilli()
	startMs := nowMs - int64(timeRangeDays)*86400000

	matchStage := bson.M{
		"timestamp_ms": bson.M{"$gte": startMs},
	}

	if gameType != GameTypeAll {
		matchStage["game_type"] = gameType
	}

	if minRank > 0 || maxRank > 0 {
		rankFilter := bson.M{}
		if minRank > 0 {
			rankFilter["$gte"] = minRank
		}
		if maxRank > 0 {
			rankFilter["$lte"] = maxRank
		}
		if len(rankFilter) > 0 {
			matchStage["player_rank"] = rankFilter
		}
	}

	totalCount, err := sc.usageCol.CountDocuments(ctx, matchStage)
	if err != nil {
		return nil, fmt.Errorf("count docs: %w", err)
	}

	banMatch := bson.M{}
	for k, v := range matchStage {
		banMatch[k] = v
	}
	banMatch["banned"] = true
	totalWithBanPhase, _ := sc.usageCol.CountDocuments(ctx, banMatch)
	if totalWithBanPhase == 0 {
		totalWithBanPhase = totalCount
	}

	totalGames, _ := sc.usageCol.Distinct(ctx, "game_id", matchStage)
	totalGamesInRange := int64(len(totalGames))
	if totalGamesInRange == 0 {
		totalGamesInRange = 1
	}

	groupStage := bson.M{
		"$group": bson.M{
			"_id":    "$template_id",
			"total":  bson.M{"$sum": 1},
			"wins":   bson.M{"$sum": bson.M{"$cond": bson.A{"$win", 1, 0}}},
			"bans":   bson.M{"$sum": bson.M{"$cond": bson.A{"$banned", 1, 0}}},
			"played": bson.M{"$sum": bson.M{"$cond": bson.A{"$played_on_board", 1, 0}}},
		},
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchStage}},
		{{Key: "$group", Value: groupStage["$group"]}},
	}

	cursor, err := sc.usageCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("aggregate: %w", err)
	}
	defer cursor.Close(ctx)

	var results []aggregationResult
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("decode results: %w", err)
	}

	statsMap := make(map[string]*pb.CardBalanceStats)
	sampleSize := int64(0)

	for _, r := range results {
		sampleSize += r.Total
		winRate := 0.0
		if r.Total > 0 {
			winRate = float64(r.Wins) / float64(r.Total) * 100
		}
		playRate := float64(r.Total) / float64(totalGamesInRange) * 100
		banRate := 0.0
		if totalWithBanPhase > 0 {
			banRate = float64(r.Bans) / float64(totalWithBanPhase) * 100
		}

		statsMap[r.TemplateID] = &pb.CardBalanceStats{
			TemplateId:       r.TemplateID,
			TotalUsage:       r.Total,
			Wins:             r.Wins,
			Losses:           r.Total - r.Wins,
			WinRate:          winRate,
			PlayRate:         playRate,
			BanCount:         r.Bans,
			BanRate:          banRate,
			Tier:             sc.getTierForStats(winRate, playRate, banRate, r.Total),
			SampleSize:       r.Total,
			LastUpdatedUnixMs: nowMs,
		}
	}

	cached := &CachedStats{
		Stats:       statsMap,
		GeneratedAt: nowMs,
		SampleSize:  sampleSize,
		TimeRange:   timeRangeDays,
		GameType:    gameType,
		MinRank:     minRank,
		MaxRank:     maxRank,
	}

	sc.mu.Lock()
	sc.cache[cacheKey] = cached
	sc.mu.Unlock()

	return sc.buildResponseFromCache(cached, templateIDs, sortBy, sortOrder, page, pageSize)
}

func (sc *StatsCollector) buildResponseFromCache(cached *CachedStats, templateIDs []string, sortBy, sortOrder string, page, pageSize int32) (*pb.GetBalanceStatsResponse, error) {
	var statsList []*pb.CardBalanceStats

	filterSet := make(map[string]bool)
	for _, id := range templateIDs {
		filterSet[id] = true
	}

	for _, s := range cached.Stats {
		if len(filterSet) > 0 && !filterSet[s.TemplateId] {
			continue
		}
		statsList = append(statsList, s)
	}

	sort.Slice(statsList, func(i, j int) bool {
		asc := sortOrder != "desc"
		switch sortBy {
		case "win_rate":
			if asc {
				return statsList[i].WinRate < statsList[j].WinRate
			}
			return statsList[i].WinRate > statsList[j].WinRate
		case "play_rate":
			if asc {
				return statsList[i].PlayRate < statsList[j].PlayRate
			}
			return statsList[i].PlayRate > statsList[j].PlayRate
		case "ban_rate":
			if asc {
				return statsList[i].BanRate < statsList[j].BanRate
			}
			return statsList[i].BanRate > statsList[j].BanRate
		case "total_usage":
			if asc {
				return statsList[i].TotalUsage < statsList[j].TotalUsage
			}
			return statsList[i].TotalUsage > statsList[j].TotalUsage
		default:
			if asc {
				return statsList[i].WinRate < statsList[j].WinRate
			}
			return statsList[i].WinRate > statsList[j].WinRate
		}
	})

	totalCards := int32(len(statsList))

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > int32(len(statsList)) {
		start = int32(len(statsList))
	}
	if end > int32(len(statsList)) {
		end = int32(len(statsList))
	}
	pagedStats := statsList[start:end]

	tierDist := sc.countTierDistribution(statsList)

	return &pb.GetBalanceStatsResponse{
		Stats:           pagedStats,
		TotalCards:      totalCards,
		SampleSizeTotal: int32(cached.SampleSize),
		GeneratedAt:     time.UnixMilli(cached.GeneratedAt).Format(time.RFC3339),
		TierDistribution: tierDist,
	}, nil
}

func (sc *StatsCollector) countTierDistribution(stats []*pb.CardBalanceStats) []string {
	counts := make(map[pb.CardBalanceTier]int)
	total := len(stats)
	if total == 0 {
		return []string{}
	}
	for _, s := range stats {
		counts[s.Tier]++
	}
	result := make([]string, 0, 6)
	tiers := []pb.CardBalanceTier{
		pb.CardBalanceTier_CARD_BALANCE_TIER_S,
		pb.CardBalanceTier_CARD_BALANCE_TIER_A,
		pb.CardBalanceTier_CARD_BALANCE_TIER_B,
		pb.CardBalanceTier_CARD_BALANCE_TIER_C,
		pb.CardBalanceTier_CARD_BALANCE_TIER_D,
		pb.CardBalanceTier_CARD_BALANCE_TIER_F,
	}
	for _, t := range tiers {
		pct := float64(counts[t]) / float64(total) * 100
		result = append(result, fmt.Sprintf("%s:%.2f%%", t.String(), pct))
	}
	return result
}

func (sc *StatsCollector) ComputeTierDistribution(timeRangeDays int32) (*pb.GetTierDistributionResponse, error) {
	if timeRangeDays <= 0 {
		timeRangeDays = DefaultTimeRangeDays
	}

	cacheKey := fmt.Sprintf("tier:%d", timeRangeDays)

	sc.mu.RLock()
	_, ok := sc.cache[cacheKey]
	sc.mu.RUnlock()

	if !ok {
		_, err := sc.computeAndCache(timeRangeDays, nil, GameTypeAll, 0, 0, "win_rate", "desc", 1, 10000, fmt.Sprintf("%d::0:0", timeRangeDays))
		if err != nil {
			return nil, err
		}
	}

	statsResp, err := sc.ComputeStats(timeRangeDays, nil, GameTypeAll, 0, 0, "win_rate", "desc", 1, 10000)
	if err != nil {
		return nil, err
	}

	distMap := make(map[pb.CardBalanceTier]*pb.TierDistribution)
	allTiers := []pb.CardBalanceTier{
		pb.CardBalanceTier_CARD_BALANCE_TIER_S,
		pb.CardBalanceTier_CARD_BALANCE_TIER_A,
		pb.CardBalanceTier_CARD_BALANCE_TIER_B,
		pb.CardBalanceTier_CARD_BALANCE_TIER_C,
		pb.CardBalanceTier_CARD_BALANCE_TIER_D,
		pb.CardBalanceTier_CARD_BALANCE_TIER_F,
	}
	for _, t := range allTiers {
		distMap[t] = &pb.TierDistribution{Tier: t}
	}

	totalCount := int32(0)
	totalSample := int64(0)
	for _, s := range statsResp.Stats {
		d := distMap[s.Tier]
		d.Count++
		d.AvgWinRate += s.WinRate
		d.AvgPlayRate += s.PlayRate
		totalCount++
		totalSample += s.SampleSize
	}

	tiers := make([]*pb.TierDistribution, 0, len(allTiers))
	for _, t := range allTiers {
		d := distMap[t]
		if d.Count > 0 {
			d.AvgWinRate /= float64(d.Count)
			d.AvgPlayRate /= float64(d.Count)
		}
		if totalCount > 0 {
			d.Percentage = float64(d.Count) / float64(totalCount) * 100
		}
		tiers = append(tiers, d)
	}

	return &pb.GetTierDistributionResponse{
		Tiers:           tiers,
		TotalSampleSize: totalSample,
	}, nil
}

func (sc *StatsCollector) getTierForStats(winRate, playRate, banRate float64, sampleSize int64) pb.CardBalanceTier {
	if sampleSize < MinSampleSize {
		return pb.CardBalanceTier_CARD_BALANCE_TIER_UNSPECIFIED
	}

	switch {
	case winRate > 55 && playRate > 20:
		return pb.CardBalanceTier_CARD_BALANCE_TIER_S
	case (winRate >= 52 && winRate <= 55) || (winRate > 50 && playRate > 15):
		return pb.CardBalanceTier_CARD_BALANCE_TIER_A
	case winRate >= 48 && winRate <= 52:
		return pb.CardBalanceTier_CARD_BALANCE_TIER_B
	case (winRate >= 45 && winRate < 48) || playRate < 5:
		return pb.CardBalanceTier_CARD_BALANCE_TIER_C
	case winRate >= 42 && winRate < 45:
		return pb.CardBalanceTier_CARD_BALANCE_TIER_D
	default:
		return pb.CardBalanceTier_CARD_BALANCE_TIER_F
	}
}

func (sc *StatsCollector) flushLoop() {
	for {
		select {
		case <-sc.flushTicker.C:
			sc.FlushBuffer()
		case <-sc.done:
			return
		}
	}
}

func (sc *StatsCollector) FlushBuffer() error {
	sc.bufferMu.Lock()
	if len(sc.buffer) == 0 {
		sc.bufferMu.Unlock()
		return nil
	}
	records := sc.buffer
	sc.buffer = make([]*CardUsageRecord, 0, maxBufferSize)
	sc.bufferSize = 0
	sc.bufferMu.Unlock()

	docs := make([]interface{}, len(records))
	for i, r := range records {
		docs[i] = r
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := sc.usageCol.InsertMany(ctx, docs)
	if err != nil {
		sc.bufferMu.Lock()
		sc.buffer = append(records, sc.buffer...)
		sc.bufferSize = int32(len(sc.buffer))
		sc.bufferMu.Unlock()
		return fmt.Errorf("insert many: %w", err)
	}

	return nil
}

func (sc *StatsCollector) ClearCache() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.cache = make(map[string]*CachedStats)
}

func (sc *StatsCollector) Close(ctx context.Context) error {
	close(sc.done)
	sc.flushTicker.Stop()
	if err := sc.FlushBuffer(); err != nil {
		return err
	}
	return sc.db.Client().Disconnect(ctx)
}
