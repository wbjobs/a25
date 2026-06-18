package balance

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	pb "github.com/ecscard/game/internal/proto"
)

type ImpactAnalysis struct {
	ChangeID        string  `bson:"change_id"`
	TemplateID      string  `bson:"template_id"`
	PreSampleSize   int64   `bson:"pre_sample_size"`
	PostSampleSize  int64   `bson:"post_sample_size"`
	PreWinRate      float64 `bson:"pre_win_rate"`
	PostWinRate     float64 `bson:"post_win_rate"`
	WinRateDelta    float64 `bson:"win_rate_delta"`
	PrePlayRate     float64 `bson:"pre_play_rate"`
	PostPlayRate    float64 `bson:"post_play_rate"`
	PlayRateDelta   float64 `bson:"play_rate_delta"`
	PreBanRate      float64 `bson:"pre_ban_rate"`
	PostBanRate     float64 `bson:"post_ban_rate"`
	BanRateDelta    float64 `bson:"ban_rate_delta"`
	PredictedImpact float64 `bson:"predicted_impact"`
	ActualImpact    float64 `bson:"actual_impact"`
	AccuracyScore   float64 `bson:"accuracy_score"`
	WindowHours     int32   `bson:"window_hours"`
	AnalyzedAt      int64   `bson:"analyzed_at"`
}

type impactAnalysisDocument struct {
	ID              primitive.ObjectID `bson:"_id,omitempty"`
	ChangeID        string             `bson:"change_id"`
	TemplateID      string             `bson:"template_id"`
	PreSampleSize   int64              `bson:"pre_sample_size"`
	PostSampleSize  int64              `bson:"post_sample_size"`
	PreWinRate      float64            `bson:"pre_win_rate"`
	PostWinRate     float64            `bson:"post_win_rate"`
	WinRateDelta    float64            `bson:"win_rate_delta"`
	PrePlayRate     float64            `bson:"pre_play_rate"`
	PostPlayRate    float64            `bson:"post_play_rate"`
	PlayRateDelta   float64            `bson:"play_rate_delta"`
	PreBanRate      float64            `bson:"pre_ban_rate"`
	PostBanRate     float64            `bson:"post_ban_rate"`
	BanRateDelta    float64            `bson:"ban_rate_delta"`
	PredictedImpact float64            `bson:"predicted_impact"`
	ActualImpact    float64            `bson:"actual_impact"`
	AccuracyScore   float64            `bson:"accuracy_score"`
	WindowHours     int32              `bson:"window_hours"`
	AnalyzedAt      int64              `bson:"analyzed_at"`
}

type ChangeLogStore struct {
	db          *mongo.Database
	changeCol   *mongo.Collection
	analysisCol *mongo.Collection
}

func NewChangeLogStore(mongoURI, dbName string) (*ChangeLogStore, error) {
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
	changeCol := db.Collection("balance_change_logs")
	analysisCol := db.Collection("balance_impact_analysis")

	_, err = analysisCol.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "change_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "template_id", Value: 1}}},
		{Keys: bson.D{{Key: "analyzed_at", Value: -1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("create analysis indexes: %w", err)
	}

	return &ChangeLogStore{
		db:          db,
		changeCol:   changeCol,
		analysisCol: analysisCol,
	}, nil
}

func (cls *ChangeLogStore) SaveChange(ctx context.Context, change *pb.CardBalanceChangeLog) error {
	doc := &ChangeLogDocument{
		ChangeID:        change.ChangeId,
		TemplateID:      change.TemplateId,
		CardName:        change.CardName,
		TimestampUnixMs: change.TimestampUnixMs,
		ChangedBy:       change.ChangedBy,
		ChangeReason:    change.ChangeReason,
		ValuesBefore:    change.ValuesBefore,
		ValuesAfter:     change.ValuesAfter,
		PredictedImpact: change.PredictedImpact,
		ActualImpact24H: change.ActualImpact24H,
	}

	_, err := cls.changeCol.UpdateOne(
		ctx,
		bson.M{"change_id": change.ChangeId},
		bson.M{"$set": doc},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("upsert change: %w", err)
	}
	return nil
}

func (cls *ChangeLogStore) GetChanges(ctx context.Context, templateID string, limit int32, fromMs, toMs int64) ([]*pb.CardBalanceChangeLog, error) {
	filter := bson.M{}
	if templateID != "" {
		filter["template_id"] = templateID
	}
	if fromMs > 0 || toMs > 0 {
		timeFilter := bson.M{}
		if fromMs > 0 {
			timeFilter["$gte"] = fromMs
		}
		if toMs > 0 {
			timeFilter["$lte"] = toMs
		}
		if len(timeFilter) > 0 {
			filter["timestamp_unix_ms"] = timeFilter
		}
	}

	opts := options.Find().SetSort(bson.M{"timestamp_unix_ms": -1})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}

	cursor, err := cls.changeCol.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find changes: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*pb.CardBalanceChangeLog
	for cursor.Next(ctx) {
		var doc ChangeLogDocument
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		result = append(result, &pb.CardBalanceChangeLog{
			ChangeId:        doc.ChangeID,
			TemplateId:      doc.TemplateID,
			CardName:        doc.CardName,
			TimestampUnixMs: doc.TimestampUnixMs,
			ChangedBy:       doc.ChangedBy,
			ChangeReason:    doc.ChangeReason,
			ValuesBefore:    doc.ValuesBefore,
			ValuesAfter:     doc.ValuesAfter,
			PredictedImpact: doc.PredictedImpact,
			ActualImpact24H: doc.ActualImpact24H,
		})
	}
	return result, nil
}

func (cls *ChangeLogStore) AnalyzeImpact(ctx context.Context, changeID string, windowHours int32, collector *StatsCollector) (*ImpactAnalysis, error) {
	var doc ChangeLogDocument
	err := cls.changeCol.FindOne(ctx, bson.M{"change_id": changeID}).Decode(&doc)
	if err != nil {
		return nil, fmt.Errorf("find change log: %w", err)
	}

	if windowHours <= 0 {
		windowHours = 24
	}
	windowMs := int64(windowHours) * 3600 * 1000
	effectiveTime := doc.TimestampUnixMs

	preStartMs := effectiveTime - windowMs
	preEndMs := effectiveTime
	postStartMs := effectiveTime
	postEndMs := effectiveTime + windowMs

	preStats, preSample, err := cls.queryWindowStats(ctx, collector, doc.TemplateID, preStartMs, preEndMs)
	if err != nil {
		return nil, fmt.Errorf("query pre stats: %w", err)
	}

	postStats, postSample, err := cls.queryWindowStats(ctx, collector, doc.TemplateID, postStartMs, postEndMs)
	if err != nil {
		return nil, fmt.Errorf("query post stats: %w", err)
	}

	winRateDelta := postStats.WinRate - preStats.WinRate
	playRateDelta := postStats.PlayRate - preStats.PlayRate
	banRateDelta := postStats.BanRate - preStats.BanRate

	actualImpact := (winRateDelta*2.0 + playRateDelta*1.0 + banRateDelta*1.5) / 4.5

	accuracyScore := 0.0
	absPredicted := absFloat64(doc.PredictedImpact)
	if absPredicted < 0.1 {
		absPredicted = 0.1
	}
	accuracyScore = 1.0 - absFloat64(actualImpact-doc.PredictedImpact)/absPredicted
	if accuracyScore < 0 {
		accuracyScore = 0
	}

	analysis := &ImpactAnalysis{
		ChangeID:        changeID,
		TemplateID:      doc.TemplateID,
		PreSampleSize:   preSample,
		PostSampleSize:  postSample,
		PreWinRate:      preStats.WinRate,
		PostWinRate:     postStats.WinRate,
		WinRateDelta:    winRateDelta,
		PrePlayRate:     preStats.PlayRate,
		PostPlayRate:    postStats.PlayRate,
		PlayRateDelta:   playRateDelta,
		PreBanRate:      preStats.BanRate,
		PostBanRate:     postStats.BanRate,
		BanRateDelta:    banRateDelta,
		PredictedImpact: doc.PredictedImpact,
		ActualImpact:    actualImpact,
		AccuracyScore:   accuracyScore,
		WindowHours:     windowHours,
		AnalyzedAt:      time.Now().UnixMilli(),
	}

	return analysis, nil
}

func (cls *ChangeLogStore) queryWindowStats(ctx context.Context, collector *StatsCollector, templateID string, startMs, endMs int64) (*pb.CardBalanceStats, int64, error) {
	if collector == nil {
		return &pb.CardBalanceStats{TemplateId: templateID}, 0, nil
	}

	matchStage := bson.M{
		"template_id":  templateID,
		"timestamp_ms": bson.M{"$gte": startMs, "$lte": endMs},
	}

	totalCount, err := collector.usageCol.CountDocuments(ctx, matchStage)
	if err != nil {
		return nil, 0, fmt.Errorf("count: %w", err)
	}

	banMatch := bson.M{}
	for k, v := range matchStage {
		banMatch[k] = v
	}
	banMatch["banned"] = true
	banCount, _ := collector.usageCol.CountDocuments(ctx, banMatch)

	totalGamesArr, _ := collector.usageCol.Distinct(ctx, "game_id", matchStage)
	totalGames := int64(len(totalGamesArr))
	if totalGames == 0 {
		totalGames = 1
	}

	groupStage := bson.M{
		"_id":    "$template_id",
		"total":  bson.M{"$sum": 1},
		"wins":   bson.M{"$sum": bson.M{"$cond": bson.A{"$win", 1, 0}}},
		"bans":   bson.M{"$sum": bson.M{"$cond": bson.A{"$banned", 1, 0}}},
		"played": bson.M{"$sum": bson.M{"$cond": bson.A{"$played_on_board", 1, 0}}},
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchStage}},
		{{Key: "$group", Value: groupStage}},
	}

	cursor, err := collector.usageCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("aggregate: %w", err)
	}
	defer cursor.Close(ctx)

	var results []aggregationResult
	if err := cursor.All(ctx, &results); err != nil {
		return nil, 0, fmt.Errorf("decode: %w", err)
	}

	var r aggregationResult
	if len(results) > 0 {
		r = results[0]
	}

	winRate := 0.0
	if r.Total > 0 {
		winRate = float64(r.Wins) / float64(r.Total) * 100
	}
	playRate := float64(r.Total) / float64(totalGames) * 100
	banRate := 0.0
	if totalCount > 0 {
		banRate = float64(banCount) / float64(totalCount) * 100
	}

	stats := &pb.CardBalanceStats{
		TemplateId: templateID,
		TotalUsage: r.Total,
		Wins:       r.Wins,
		Losses:     r.Total - r.Wins,
		WinRate:    winRate,
		PlayRate:   playRate,
		BanCount:   banCount,
		BanRate:    banRate,
		SampleSize: r.Total,
	}

	return stats, totalCount, nil
}

func (cls *ChangeLogStore) SaveImpactAnalysis(ctx context.Context, analysis *ImpactAnalysis) error {
	doc := &impactAnalysisDocument{
		ChangeID:        analysis.ChangeID,
		TemplateID:      analysis.TemplateID,
		PreSampleSize:   analysis.PreSampleSize,
		PostSampleSize:  analysis.PostSampleSize,
		PreWinRate:      analysis.PreWinRate,
		PostWinRate:     analysis.PostWinRate,
		WinRateDelta:    analysis.WinRateDelta,
		PrePlayRate:     analysis.PrePlayRate,
		PostPlayRate:    analysis.PostPlayRate,
		PlayRateDelta:   analysis.PlayRateDelta,
		PreBanRate:      analysis.PreBanRate,
		PostBanRate:     analysis.PostBanRate,
		BanRateDelta:    analysis.BanRateDelta,
		PredictedImpact: analysis.PredictedImpact,
		ActualImpact:    analysis.ActualImpact,
		AccuracyScore:   analysis.AccuracyScore,
		WindowHours:     analysis.WindowHours,
		AnalyzedAt:      analysis.AnalyzedAt,
	}

	_, err := cls.analysisCol.UpdateOne(
		ctx,
		bson.M{"change_id": analysis.ChangeID},
		bson.M{"$set": doc},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("upsert analysis: %w", err)
	}
	return nil
}

func (cls *ChangeLogStore) GetAllPendingAnalysis(ctx context.Context, changeAgeHours int32) ([]string, error) {
	if changeAgeHours <= 0 {
		changeAgeHours = 24
	}
	cutoffMs := time.Now().Add(-time.Duration(changeAgeHours) * time.Hour).UnixMilli()

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"timestamp_unix_ms": bson.M{"$lte": cutoffMs}}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "balance_impact_analysis",
			"localField":   "change_id",
			"foreignField": "change_id",
			"as":           "analysis",
		}}},
		{{Key: "$match", Value: bson.M{"analysis": bson.M{"$size": 0}}}},
		{{Key: "$project", Value: bson.M{"change_id": 1, "_id": 0}}},
	}

	cursor, err := cls.changeCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("aggregate pending: %w", err)
	}
	defer cursor.Close(ctx)

	var result []string
	for cursor.Next(ctx) {
		var doc struct {
			ChangeID string `bson:"change_id"`
		}
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		result = append(result, doc.ChangeID)
	}
	return result, nil
}

func (cls *ChangeLogStore) Close(ctx context.Context) error {
	return cls.db.Client().Disconnect(ctx)
}

func absFloat64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
