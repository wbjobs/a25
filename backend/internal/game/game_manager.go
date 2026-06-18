package game

import (
	"context"
	"sync"

	"github.com/ecscard/game/internal/cache"
	"github.com/ecscard/game/internal/mongodb"
	"github.com/ecscard/game/internal/redis"
	"github.com/google/uuid"
)

type GameManager struct {
	games        map[string]*GameInstance
	mu           sync.RWMutex
	cacheManager *cache.CacheManager
	redisStore   *redis.StateStore
	mongoStore   *mongodb.Store
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

	gm := &GameManager{
		games:        make(map[string]*GameInstance),
		cacheManager: cacheManager,
		redisStore:   redisStore,
		mongoStore:   mongoStore,
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

	game := NewGameInstance(gameID, matchID, player1ID, player2ID, player1Name, player2Name, isAI, aiDifficulty)

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

func (gm *GameManager) Close() {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	for _, game := range gm.games {
		game.Close()
	}
	gm.games = make(map[string]*GameInstance)

	gm.cacheManager.Close()
	gm.redisStore.Close()
	gm.mongoStore.Close(context.Background())
}
