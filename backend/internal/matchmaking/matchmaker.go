package matchmaking

import (
	"context"
	"sync"
	"time"

	"github.com/ecscard/game/internal/mongodb"
	"github.com/ecscard/game/internal/redis"
	pb "github.com/ecscard/game/proto/v1"
	"github.com/google/uuid"
)

type QueuedPlayer struct {
	Player   *pb.Player
	GameType string
	Channel  chan *pb.MatchResponse
	JoinedAt time.Time
}

type MatchResult struct {
	MatchID          string
	Player1          *pb.Player
	Player2          *pb.Player
	GameID           string
	GameServerAddr   string
}

type Matchmaker struct {
	queue         map[string][]*QueuedPlayer
	mu            sync.RWMutex
	redisStore    *redis.StateStore
	mongoStore    *mongodb.Store
	gameServer    string
	gameClient    pb.GameServiceClient
	matches       map[string]*MatchResult
	waitingGames  map[string]chan *MatchResult
}

func NewMatchmaker(redisAddr, mongoURI, gameServerAddr string, gameClient pb.GameServiceClient) (*Matchmaker, error) {
	redisStore := redis.NewStateStore(redisAddr, "cardgame")
	mongoStore, err := mongodb.NewStore(mongoURI, "cardgame")
	if err != nil {
		return nil, err
	}

	m := &Matchmaker{
		queue:        make(map[string][]*QueuedPlayer),
		redisStore:   redisStore,
		mongoStore:   mongoStore,
		gameServer:   gameServerAddr,
		gameClient:   gameClient,
		matches:      make(map[string]*MatchResult),
		waitingGames: make(map[string]chan *MatchResult),
	}

	go m.matchLoop()
	return m, nil
}

func (m *Matchmaker) FindMatch(player *pb.Player, gameType string) (chan *pb.MatchResponse, error) {
	if gameType == "" {
		gameType = "normal"
	}

	if _, err := m.mongoStore.GetPlayer(context.Background(), player.PlayerId); err != nil {
		doc := &mongodb.PlayerDocument{
			ID:       player.PlayerId,
			Username: player.PlayerName,
			Rating:   1500,
			Wins:     0,
			Losses:   0,
			Cards:    []string{},
			Decks:    []string{},
		}
		m.mongoStore.CreatePlayer(context.Background(), doc)
	}

	ch := make(chan *pb.MatchResponse, 10)

	m.mu.Lock()
	m.queue[gameType] = append(m.queue[gameType], &QueuedPlayer{
		Player:   player,
		GameType: gameType,
		Channel:  ch,
		JoinedAt: time.Now(),
	})
	m.mu.Unlock()

	ch <- &pb.MatchResponse{
		Status: pb.MatchStatus_MATCH_STATUS_IN_QUEUE,
	}

	return ch, nil
}

func (m *Matchmaker) CancelMatch(playerID, matchID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for gameType, players := range m.queue {
		for i, p := range players {
			if p.Player.PlayerId == playerID {
				m.queue[gameType] = append(players[:i], players[i+1:]...)
				close(p.Channel)
				return true
			}
		}
	}

	return false
}

func (m *Matchmaker) matchLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		m.tryMatch()
	}
}

func (m *Matchmaker) tryMatch() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for gameType, players := range m.queue {
		if len(players) < 2 {
			continue
		}

		matched := m.findMatchingPair(players)
		if matched == nil {
			continue
		}

		p1 := matched[0]
		p2 := matched[1]

		m.removeFromQueue(gameType, p1.Player.PlayerId)
		m.removeFromQueue(gameType, p2.Player.PlayerId)

		matchID := uuid.New().String()

		resp1 := &pb.MatchResponse{
			MatchId: matchID,
			Status:  pb.MatchStatus_MATCH_STATUS_MATCHED,
			Opponent: &pb.Player{
				PlayerId:   p2.Player.PlayerId,
				PlayerName: p2.Player.PlayerName,
			},
		}

		resp2 := &pb.MatchResponse{
			MatchId: matchID,
			Status:  pb.MatchStatus_MATCH_STATUS_MATCHED,
			Opponent: &pb.Player{
				PlayerId:   p1.Player.PlayerId,
				PlayerName: p1.Player.PlayerName,
			},
		}

		p1.Channel <- resp1
		p2.Channel <- resp2

		go m.startGame(matchID, p1, p2, gameType)
	}
}

func (m *Matchmaker) findMatchingPair(players []*QueuedPlayer) []*QueuedPlayer {
	if len(players) < 2 {
		return nil
	}

	for i := 0; i < len(players); i++ {
		for j := i + 1; j < len(players); j++ {
			p1 := players[i]
			p2 := players[j]

			ratingDiff := abs(int(p1.Player.Rating) - int(p2.Player.Rating))
			waitTime := time.Since(p1.JoinedAt).Seconds()

			allowedDiff := int(50 + waitTime*10)

			if ratingDiff <= allowedDiff {
				return []*QueuedPlayer{p1, p2}
			}
		}
	}

	if len(players) >= 2 {
		return []*QueuedPlayer{players[0], players[1]}
	}

	return nil
}

func (m *Matchmaker) removeFromQueue(gameType, playerID string) {
	players := m.queue[gameType]
	for i, p := range players {
		if p.Player.PlayerId == playerID {
			m.queue[gameType] = append(players[:i], players[i+1:]...)
			return
		}
	}
}

func (m *Matchmaker) startGame(matchID string, p1, p2 *QueuedPlayer, gameType string) {
	ctx := context.Background()

	startReq := &pb.StartGameRequest{
		Player1Id:   p1.Player.PlayerId,
		Player1Name: p1.Player.PlayerName,
		Player2Id:   p2.Player.PlayerId,
		Player2Name: p2.Player.PlayerName,
		GameType:    gameType,
	}

	startResp, err := m.gameClient.StartGame(ctx, startReq)
	if err != nil {
		p1.Channel <- &pb.MatchResponse{
			Status: pb.MatchStatus_MATCH_STATUS_CANCELLED,
		}
		p2.Channel <- &pb.MatchResponse{
			Status: pb.MatchStatus_MATCH_STATUS_CANCELLED,
		}
		return
	}

	match := &MatchResult{
		MatchID:        matchID,
		Player1:        p1.Player,
		Player2:        p2.Player,
		GameID:         startResp.GameId,
		GameServerAddr: m.gameServer,
	}

	m.mu.Lock()
	m.matches[matchID] = match
	m.mu.Unlock()

	resp1 := &pb.MatchResponse{
		MatchId:          matchID,
		Status:           pb.MatchStatus_MATCH_STATUS_IN_GAME,
		GameId:           startResp.GameId,
		GameServerAddress: m.gameServer,
	}

	resp2 := &pb.MatchResponse{
		MatchId:          matchID,
		Status:           pb.MatchStatus_MATCH_STATUS_IN_GAME,
		GameId:           startResp.GameId,
		GameServerAddress: m.gameServer,
	}

	p1.Channel <- resp1
	p2.Channel <- resp2
}

func (m *Matchmaker) GetMatchStatus(playerID, matchID string) (*pb.GetMatchStatusResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	match, ok := m.matches[matchID]
	if !ok {
		return nil, nil
	}

	return &pb.GetMatchStatusResponse{
		MatchId: matchID,
		Status:  pb.MatchStatus_MATCH_STATUS_IN_GAME,
		Player1: match.Player1,
		Player2: match.Player2,
		GameId:  match.GameID,
	}, nil
}

func (m *Matchmaker) GetPlayerStats(playerID string) (*pb.GetPlayerStatsResponse, error) {
	player, err := m.mongoStore.GetPlayer(context.Background(), playerID)
	if err != nil {
		player = &mongodb.PlayerDocument{
			ID:         playerID,
			Rating:     1500,
			Wins:       0,
			Losses:     0,
			TotalGames: 0,
			WinStreak:  0,
		}
	}

	return &pb.GetPlayerStatsResponse{
		PlayerId:   playerID,
		Rating:     int32(player.Rating),
		Wins:       int32(player.Wins),
		Losses:     int32(player.Losses),
		WinStreak:  int32(player.WinStreak),
		TotalGames: int32(player.TotalGames),
	}, nil
}

func (m *Matchmaker) SubmitMatchResult(result *pb.MatchResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if match, ok := m.matches[result.MatchId]; ok {
		delete(m.matches, result.MatchId)

		m.mongoStore.UpdatePlayerStats(context.Background(), result.WinnerId, true)
		m.mongoStore.UpdatePlayerStats(context.Background(), result.LoserId, false)

		ratingDelta := 15
		m.mongoStore.UpdatePlayerRating(context.Background(), result.WinnerId, ratingDelta)
		m.mongoStore.UpdatePlayerRating(context.Background(), result.LoserId, -ratingDelta)

		_ = match
	}

	return nil
}

func (m *Matchmaker) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, players := range m.queue {
		for _, p := range players {
			close(p.Channel)
		}
	}
	m.queue = make(map[string][]*QueuedPlayer)

	m.redisStore.Close()
	m.mongoStore.Close(context.Background())
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
