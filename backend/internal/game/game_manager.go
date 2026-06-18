package game

import (
	"context"
	"log"
	"sync"

	"github.com/ecscard/game/internal/balance"
	"github.com/ecscard/game/internal/cache"
	"github.com/ecscard/game/internal/game/systems"
	"github.com/ecscard/game/internal/mongodb"
	"github.com/ecscard/game/internal/redis"
	"github.com/ecscard/game/internal/replay"
	"github.com/google/uuid"
)

type GameManager struct {
	games           map[string]*GameInstance
	mu              sync.RWMutex
	cacheManager    *cache.CacheManager
	redisStore      *redis.StateStore
	mongoStore      *mongodb.Store
	replayStore     *replay.ReplayStore
	specMgr         *replay.SpectatorManager
	statsCollector  *balance.StatsCollector
	hotUpdateMgr    *balance.HotUpdateManager
	changeLogStore  *balance.ChangeLogStore
	balanceRegistry *balance.BalancedCardRegistry
}

func NewGameManager(redisAddr string, redisPassword string, redisDB int, mongoURI string, useCluster bool, clusterAddrs []string) (*GameManager, error) {
	var redisAddrs []string
	if useCluster && len(clusterAddrs) > 0 {
		redisAddrs = clusterAddrs
	} else {
		redisAddrs = []string{redisAddr}
	}

	cacheManager, err := cache.NewCacheManager(useCluster, redisAddrs, redisPassword, redisDB)
	if err != nil {
		return nil, err
	}

	redisStore := redis.NewStateStore(redisAddr, "cardgame")
	mongoStore, err := mongodb.NewStore(mongoURI, "cardgame")
	if err != nil {
		return nil, err
	}

	replayStore, err := replay.NewReplayStore(mongoURI, "cardgame")
	if err != nil {
		log.Printf("[GameManager] Warning: failed to init ReplayStore: %v", err)
		replayStore = nil
	}

	hotUpdateMgr, err := balance.NewHotUpdateManager(mongoURI, "cardgame")
	if err != nil {
		log.Printf("[GameManager] Warning: failed to init HotUpdateManager: %v", err)
		hotUpdateMgr = nil
	}
	if hotUpdateMgr != nil {
		if err := hotUpdateMgr.LoadOverridesFromDB(); err != nil {
			log.Printf("[GameManager] Warning: failed to load overrides from DB: %v", err)
		}
	}

	changeLogStore, err := balance.NewChangeLogStore(mongoURI, "cardgame")
	if err != nil {
		log.Printf("[GameManager] Warning: failed to init ChangeLogStore: %v", err)
		changeLogStore = nil
	}

	statsCollector, err := balance.NewStatsCollector(mongoURI, "cardgame", hotUpdateMgr)
	if err != nil {
		log.Printf("[GameManager] Warning: failed to init StatsCollector: %v", err)
		statsCollector = nil
	}

	var specMgr *replay.SpectatorManager
	if replayStore != nil {
		specMgr = replay.NewSpectatorManager(replayStore)
	}

	balanceRegistry := balance.NewBalancedCardRegistry(hotUpdateMgr)
	balanceRegistry.InitFromDefaultTemplates(CardTemplates)

	gm := &GameManager{
		games:           make(map[string]*GameInstance),
		cacheManager:    cacheManager,
		redisStore:      redisStore,
		mongoStore:      mongoStore,
		replayStore:     replayStore,
		specMgr:         specMgr,
		statsCollector:  statsCollector,
		hotUpdateMgr:    hotUpdateMgr,
		changeLogStore:  changeLogStore,
		balanceRegistry: balanceRegistry,
	}

	return gm, nil
}

func (gm *GameManager) CreateGame(matchID, player1ID, player2ID, player1Name, player2Name string, gameType string, isAI bool, aiDifficulty string) (*GameInstance, error) {
	gameID := uuid.New().String()

	gameDoc := &mongodb.GameDocument{
		ID:          gameID,
		MatchID:     matchID,
		Player1ID:   player1ID,
		Player2ID:   player2ID,
		Player1Name: player1Name,
		Player2Name: player2Name,
		GameType:    gameType,
		Turns:       0,
	}

	if err := gm.mongoStore.CreateGame(context.Background(), gameDoc); err != nil {
		return nil, err
	}

	game := NewGameInstance(gameID, matchID, player1ID, player2ID, player1Name, player2Name, isAI, aiDifficulty, gm.replayStore, gm.statsCollector, gm.balanceRegistry)

	gm.mu.Lock()
	gm.games[gameID] = game
	gm.mu.Unlock()

	go gm.monitorGame(game)

	return game, nil
}

func (gm *GameManager) monitorGame(game *GameInstance) {
	for {
		if game.IsFinished() {
			gm.handleGameEnd(game)
			return
		}
	}
}

func (gm *GameManager) handleGameEnd(game *GameInstance) {
	game.HandleGameEndInternal()

	winnerID := game.GetWinner()
	loserID := game.Player1ID
	if winnerID == game.Player1ID {
		loserID = game.Player2ID
	}

	gm.mongoStore.UpdateGameResult(
		context.Background(),
		game.GameID,
		winnerID,
		loserID,
		game.GetTurns(),
		game.DurationMs,
		game.IsFinished(),
	)

	gm.mongoStore.UpdatePlayerStats(context.Background(), winnerID, true)
	gm.mongoStore.UpdatePlayerStats(context.Background(), loserID, false)

	ratingDelta := 15
	gm.mongoStore.UpdatePlayerRating(context.Background(), winnerID, ratingDelta)
	gm.mongoStore.UpdatePlayerRating(context.Background(), loserID, -ratingDelta)

	gm.redisStore.DeleteGameState(context.Background(), game.GameID)

	gm.mu.Lock()
	delete(gm.games, game.GameID)
	gm.mu.Unlock()

	game.Close()
}

func (gm *GameManager) MarkGameEnded(gameID, winnerID string, turns int, durationMs int64) {
	gm.mu.RLock()
	game, ok := gm.games[gameID]
	gm.mu.RUnlock()

	if !ok {
		return
	}

	game.HandleGameEndInternal()

	loserID := game.Player1ID
	if winnerID == game.Player1ID {
		loserID = game.Player2ID
	}

	gm.mongoStore.UpdateGameResult(
		context.Background(),
		gameID,
		winnerID,
		loserID,
		turns,
		durationMs,
		true,
	)

	gm.mongoStore.UpdatePlayerStats(context.Background(), winnerID, true)
	gm.mongoStore.UpdatePlayerStats(context.Background(), loserID, false)

	ratingDelta := 15
	gm.mongoStore.UpdatePlayerRating(context.Background(), winnerID, ratingDelta)
	gm.mongoStore.UpdatePlayerRating(context.Background(), loserID, -ratingDelta)

	gm.redisStore.DeleteGameState(context.Background(), gameID)

	gm.mu.Lock()
	delete(gm.games, gameID)
	gm.mu.Unlock()

	game.Close()
}

func (gm *GameManager) GetGame(gameID string) (*GameInstance, bool) {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	game, ok := gm.games[gameID]
	return game, ok
}

func (gm *GameManager) RemoveGame(gameID string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if game, ok := gm.games[gameID]; ok {
		game.Close()
		delete(gm.games, gameID)
	}
}

func (gm *GameManager) GetActiveGames() []*GameInstance {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	games := make([]*GameInstance, 0, len(gm.games))
	for _, game := range gm.games {
		games = append(games, game)
	}
	return games
}

func (gm *GameManager) GetReplayStore() *replay.ReplayStore {
	return gm.replayStore
}

func (gm *GameManager) GetSpecMgr() *replay.SpectatorManager {
	return gm.specMgr
}

func (gm *GameManager) GetStatsCollector() *balance.StatsCollector {
	return gm.statsCollector
}

func (gm *GameManager) GetHotUpdateMgr() *balance.HotUpdateManager {
	return gm.hotUpdateMgr
}

func (gm *GameManager) GetChangeLogStore() *balance.ChangeLogStore {
	return gm.changeLogStore
}

func (gm *GameManager) GetBalanceRegistry() *balance.BalancedCardRegistry {
	return gm.balanceRegistry
}

func (gm *GameManager) Close() {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	for _, game := range gm.games {
		game.Close()
	}
	gm.games = make(map[string]*GameInstance)

	ctx := context.Background()

	if gm.changeLogStore != nil {
		_ = gm.changeLogStore.Close(ctx)
	}
	if gm.statsCollector != nil {
		_ = gm.statsCollector.Close(ctx)
	}
	if gm.hotUpdateMgr != nil {
		_ = gm.hotUpdateMgr.Close(ctx)
	}
	if gm.replayStore != nil {
		_ = gm.replayStore.Close(ctx)
	}
	if gm.specMgr != nil {
		gm.specMgr.Close()
	}

	gm.cacheManager.Close()
	gm.redisStore.Close()
	gm.mongoStore.Close(ctx)
}
