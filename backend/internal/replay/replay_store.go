package replay

import (
	"context"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	pb "github.com/ecscard/game/internal/proto"
)

type ReplayDocument struct {
	ID               primitive.ObjectID `bson:"_id,omitempty"`
	GameID           string             `bson:"game_id"`
	MatchID          string             `bson:"match_id"`
	Player1ID        string             `bson:"player1_id"`
	Player1Name      string             `bson:"player1_name"`
	Player2ID        string             `bson:"player2_id"`
	Player2Name      string             `bson:"player2_name"`
	StartTimeMs      int64              `bson:"start_time_ms"`
	EndTimeMs        int64              `bson:"end_time_ms"`
	DurationMs       int64              `bson:"duration_ms"`
	WinnerID         string             `bson:"winner_id"`
	TotalTurns       int32              `bson:"total_turns"`
	TotalActions     int32              `bson:"total_actions"`
	IsLive           bool               `bson:"is_live"`
	SpectatorDelayMs int32              `bson:"spectator_delay_ms"`
	InitialSnapshot  []byte             `bson:"initial_snapshot"`
	CreatedAt        time.Time          `bson:"created_at"`
	UpdatedAt        time.Time          `bson:"updated_at"`
}

type ReplayActionDocument struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	GameID      string             `bson:"game_id"`
	RelativeMs  int64              `bson:"relative_ms"`
	FrameNumber uint64             `bson:"frame_number"`
	ActionData  []byte             `bson:"action_data"`
	StateAfter  []byte             `bson:"state_after"`
	EventsData  [][]byte           `bson:"events_data"`
	ActionIndex int32              `bson:"action_index"`
}

type ReplayStore struct {
	db             *mongo.Database
	replayCol      *mongo.Collection
	actionCol      *mongo.Collection
	liveGamesCache map[string]*ReplayRecorder
	liveMu         sync.RWMutex
}

type LiveGameEntry struct {
	GameID           string
	Player1Name      string
	Player2Name      string
	Player1Health    int32
	Player2Health    int32
	CurrentTurn      int32
	SpectatorCount   int32
	SpectatorDelayMs int32
	ElapsedMs        int64
	GameType         string
}

func NewReplayStore(mongoURI, dbName string) (*ReplayStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}

	db := client.Database(dbName)
	replayCol := db.Collection("replays")
	actionCol := db.Collection("replay_actions")

	store := &ReplayStore{
		db:             db,
		replayCol:      replayCol,
		actionCol:      actionCol,
		liveGamesCache: make(map[string]*ReplayRecorder),
	}

	store.createIndexes(ctx)

	return store, nil
}

func (s *ReplayStore) createIndexes(ctx context.Context) {
	actionIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "game_id", Value: 1},
			{Key: "action_index", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	}
	_, _ = s.actionCol.Indexes().CreateOne(ctx, actionIndexModel)

	replayIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "game_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
	_, _ = s.replayCol.Indexes().CreateOne(ctx, replayIndexModel)
}

func (s *ReplayStore) SaveReplayMeta(ctx context.Context, doc *ReplayDocument) error {
	doc.UpdatedAt = time.Now()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}

	opts := options.Update().SetUpsert(true)
	update := bson.M{
		"$set": bson.M{
			"match_id":           doc.MatchID,
			"player1_id":         doc.Player1ID,
			"player1_name":       doc.Player1Name,
			"player2_id":         doc.Player2ID,
			"player2_name":       doc.Player2Name,
			"start_time_ms":      doc.StartTimeMs,
			"end_time_ms":        doc.EndTimeMs,
			"duration_ms":        doc.DurationMs,
			"winner_id":          doc.WinnerID,
			"total_turns":        doc.TotalTurns,
			"total_actions":      doc.TotalActions,
			"is_live":            doc.IsLive,
			"spectator_delay_ms": doc.SpectatorDelayMs,
			"initial_snapshot":   doc.InitialSnapshot,
			"updated_at":         doc.UpdatedAt,
		},
		"$setOnInsert": bson.M{
			"created_at": doc.CreatedAt,
		},
	}

	_, err := s.replayCol.UpdateOne(ctx, bson.M{"game_id": doc.GameID}, update, opts)
	return err
}

func (s *ReplayStore) SaveReplayActions(ctx context.Context, gameID string, actions []*ReplayActionDocument) error {
	if len(actions) == 0 {
		return nil
	}

	docs := make([]interface{}, len(actions))
	for i, act := range actions {
		docs[i] = act
	}

	opts := options.InsertMany().SetOrdered(false)
	_, err := s.actionCol.InsertMany(ctx, docs, opts)
	return err
}

func (s *ReplayStore) GetReplayMeta(ctx context.Context, gameID string) (*ReplayDocument, error) {
	var doc ReplayDocument
	err := s.replayCol.FindOne(ctx, bson.M{"game_id": gameID}).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (s *ReplayStore) GetReplayActions(ctx context.Context, gameID string, fromIdx, toIdx int32) ([]*ReplayActionDocument, error) {
	filter := bson.M{
		"game_id": gameID,
		"action_index": bson.M{
			"$gte": fromIdx,
			"$lte": toIdx,
		},
	}

	opts := options.Find().SetSort(bson.M{"action_index": 1})
	cursor, err := s.actionCol.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var actions []*ReplayActionDocument
	for cursor.Next(ctx) {
		var act ReplayActionDocument
		if err := cursor.Decode(&act); err != nil {
			return nil, err
		}
		actions = append(actions, &act)
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}

	return actions, nil
}

func (s *ReplayStore) ListLiveGames(ctx context.Context, gameType string, page, pageSize int32) ([]*LiveGameEntry, int32, error) {
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()

	entries := make([]*LiveGameEntry, 0, len(s.liveGamesCache))
	for _, recorder := range s.liveGamesCache {
		meta := recorder.GetMeta()
		entry := &LiveGameEntry{
			GameID:           meta.GameId,
			Player1Name:      meta.Player1Name,
			Player2Name:      meta.Player2Name,
			Player1Health:    30,
			Player2Health:    30,
			CurrentTurn:      meta.TotalTurns,
			SpectatorCount:   recorder.GetSpectatorCount(),
			SpectatorDelayMs: meta.SpectatorDelayMs,
			ElapsedMs:        meta.DurationMs,
			GameType:         gameType,
		}

		latestSnap := recorder.latestSnapshot
		if latestSnap != nil && latestSnap.Status != nil {
			for _, p := range latestSnap.Status.Players {
				if p.PlayerId == recorder.player1ID {
					entry.Player1Health = p.Health
				}
				if p.PlayerId == recorder.player2ID {
					entry.Player2Health = p.Health
				}
			}
			entry.CurrentTurn = latestSnap.Status.Turn
		}

		entries = append(entries, entry)
	}

	total := int32(len(entries))
	startIdx := int((page - 1) * pageSize)
	endIdx := startIdx + int(pageSize)

	if startIdx >= len(entries) {
		return []*LiveGameEntry{}, total, nil
	}
	if endIdx > len(entries) {
		endIdx = len(entries)
	}

	return entries[startIdx:endIdx], total, nil
}

func (s *ReplayStore) RegisterLiveGame(recorder *ReplayRecorder) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	s.liveGamesCache[recorder.gameID] = recorder
}

func (s *ReplayStore) UnregisterLiveGame(gameID string) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	delete(s.liveGamesCache, gameID)
}

func (s *ReplayStore) GetLiveRecorder(gameID string) (*ReplayRecorder, bool) {
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()
	r, ok := s.liveGamesCache[gameID]
	return r, ok
}

func (s *ReplayStore) GetAllLiveRecorders() []*ReplayRecorder {
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()
	result := make([]*ReplayRecorder, 0, len(s.liveGamesCache))
	for _, r := range s.liveGamesCache {
		result = append(result, r)
	}
	return result
}

func (s *ReplayStore) Close(ctx context.Context) error {
	if s.db != nil && s.db.Client() != nil {
		return s.db.Client().Disconnect(ctx)
	}
	return nil
}
