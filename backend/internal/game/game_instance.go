package game

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game/components"
	"github.com/ecscard/game/internal/game/systems"
	pb "github.com/ecscard/game/proto/v1"
)

type GameInstance struct {
	GameID       string
	MatchID      string
	World        *ecs.World
	CardSystem   *systems.CardSystem
	DrawSystem   *systems.DrawSystem
	CombatSystem *systems.CombatSystem
	TurnSystem   *systems.TurnSystem
	Player1ID    string
	Player2ID    string
	CreatedAt    time.Time
	StartedAt    time.Time
	EndedAt      time.Time
	DurationMs   int64
	mu           sync.RWMutex
	streams      map[string]chan *pb.GameFrame
	actionSeq    uint64
	eventSeq     uint64
}

func NewGameInstance(gameID, matchID, player1ID, player2ID, player1Name, player2Name string) *GameInstance {
	world := ecs.NewWorld()

	cardSystem := systems.NewCardSystem()
	drawSystem := systems.NewDrawSystem()
	combatSystem := systems.NewCombatSystem()
	turnSystem := systems.NewTurnSystem()

	world.AddSystem(cardSystem)
	world.AddSystem(drawSystem)
	world.AddSystem(combatSystem)
	world.AddSystem(turnSystem)

	gi := &GameInstance{
		GameID:       gameID,
		MatchID:      matchID,
		World:        world,
		CardSystem:   cardSystem,
		DrawSystem:   drawSystem,
		CombatSystem: combatSystem,
		TurnSystem:   turnSystem,
		Player1ID:    player1ID,
		Player2ID:    player2ID,
		CreatedAt:    time.Now(),
		streams:      make(map[string]chan *pb.GameFrame),
	}

	turnSystem.StartGame(world, player1ID, player2ID, player1Name, player2Name)
	gi.StartedAt = time.Now()

	go gi.startGameLoop()
	go gi.listenEvents()

	return gi
}

func (g *GameInstance) startGameLoop() {
	ticker := time.NewTicker(33 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-g.World.Context().Done():
			return
		case <-ticker.C:
			g.mu.Lock()
			state := g.TurnSystem.GetGameState(g.World)
			if state != nil && state.State == components.GameStateFinished {
				g.EndedAt = time.Now()
				g.DurationMs = g.EndedAt.Sub(g.StartedAt).Milliseconds()
				g.mu.Unlock()
				return
			}
			g.World.Update(0.033)
			g.mu.Unlock()
		}
	}
}

func (g *GameInstance) listenEvents() {
	for event := range g.World.Events() {
		g.broadcastEvent(event)
	}
}

func (g *GameInstance) broadcastEvent(event *ecs.Event) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	frame := &pb.GameFrame{
		FrameNumber: g.getFrameNumber(),
		Events: []*pb.GameEvent{
			{
				Sequence:    g.nextEventSeq(),
				EventType:   event.Type,
				FrameNumber: g.getFrameNumber(),
				Data:        g.eventDataToStringMap(event.Data),
				EntityId:    uint64(event.Entity.ID),
				Timestamp:   time.Now().UnixNano() / 1e6,
			},
		},
	}

	for _, ch := range g.streams {
		select {
		case ch <- frame:
		default:
		}
	}
}

func (g *GameInstance) getFrameNumber() uint64 {
	state := g.TurnSystem.GetGameState(g.World)
	if state != nil {
		return state.FrameNumber
	}
	return 0
}

func (g *GameInstance) nextActionSeq() uint64 {
	g.actionSeq++
	return g.actionSeq
}

func (g *GameInstance) nextEventSeq() uint64 {
	g.eventSeq++
	return g.eventSeq
}

func (g *GameInstance) eventDataToStringMap(data interface{}) map[string]string {
	result := make(map[string]string)
	if m, ok := data.(map[string]interface{}); ok {
		for k, v := range m {
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}

func (g *GameInstance) PlayCard(playerID string, cardID, targetID ecs.EntityID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.TurnSystem.IsPlayerTurn(g.World, playerID) {
		return false
	}

	player := g.getPlayerEntity(playerID)
	if player == nil {
		return false
	}

	return g.CombatSystem.PlayCard(g.World, player, cardID, targetID)
}

func (g *GameInstance) Attack(playerID string, attackerID, targetID ecs.EntityID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.TurnSystem.IsPlayerTurn(g.World, playerID) {
		return false
	}

	return g.CombatSystem.Attack(g.World, attackerID, targetID)
}

func (g *GameInstance) EndTurn(playerID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.TurnSystem.EndTurn(g.World, playerID)
}

func (g *GameInstance) Concede(playerID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	state := g.TurnSystem.GetGameState(g.World)
	if state == nil || state.State == components.GameStateFinished {
		return false
	}

	opponentID := g.Player2ID
	if playerID == g.Player2ID {
		opponentID = g.Player1ID
	}

	gameStateEntity := g.World.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"game_state"},
	})
	if len(gameStateEntity) > 0 {
		if stateComp, ok := gameStateEntity[0].GetComponent("game_state").(*components.GameStateComponent); ok {
			stateComp.State = components.GameStateFinished
			stateComp.Winner = opponentID
		}
	}

	g.EndedAt = time.Now()
	g.DurationMs = g.EndedAt.Sub(g.StartedAt).Milliseconds()

	g.World.EmitEvent("player_conceded", map[string]interface{}{
		"player_id": playerID,
		"winner_id": opponentID,
	}, nil)

	return true
}

func (g *GameInstance) GetGameState(playerID string) *pb.GameStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.buildGameStatus(playerID)
}

func (g *GameInstance) buildGameStatus(playerID string) *pb.GameStatus {
	state := g.TurnSystem.GetGameState(g.World)
	if state == nil {
		return nil
	}

	players := make([]*pb.Player, 0)
	cards := make([]*pb.Card, 0)

	playerEntities := g.World.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"player", "hero", "mana", "hand", "board", "deck"},
	})

	for _, p := range playerEntities {
		players = append(players, g.buildPlayer(p, playerID))
	}

	allCards := g.World.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"card", "owner"},
	})

	for _, c := range allCards {
		if card := g.buildCard(c, playerID); card != nil {
			cards = append(cards, card)
		}
	}

	return &pb.GameStatus{
		GameId:             g.GameID,
		State:              pb.GameState(state.State),
		Turn:               int32(state.Turn),
		CurrentTurnPlayerId: state.CurrentTurn,
		Phase:              pb.TurnPhase(state.Phase),
		FrameNumber:        state.FrameNumber,
		Winner:             state.Winner,
		Players:            players,
		Cards:              cards,
	}
}

func (g *GameInstance) buildPlayer(player *ecs.Entity, viewerID string) *pb.Player {
	playerComp, _ := player.GetComponent("player").(*components.PlayerComponent)
	heroComp, _ := player.GetComponent("hero").(*components.HeroComponent)
	manaComp, _ := player.GetComponent("mana").(*ecs.ManaComponent)
	handComp, _ := player.GetComponent("hand").(*components.HandComponent)
	boardComp, _ := player.GetComponent("board").(*components.BoardComponent)
	deckComp, _ := player.GetComponent("deck").(*components.DeckComponent)

	p := &pb.Player{
		PlayerId:   playerComp.PlayerID,
		PlayerName: playerComp.PlayerName,
		Health:     int32(heroComp.Health),
		MaxHealth:  int32(heroComp.MaxHealth),
		Mana:       int32(manaComp.CurrentMana),
		MaxMana:    int32(manaComp.MaxMana),
		Armor:      int32(heroComp.Armor),
		Attack:     int32(heroComp.Attack),
		DeckSize:   int32(len(deckComp.Cards)),
		Weapon:     uint64(heroComp.Weapon),
	}

	if playerComp.PlayerID == viewerID {
		p.Hand = make([]uint64, len(handComp.Cards))
		for i, id := range handComp.Cards {
			p.Hand[i] = uint64(id)
		}
	} else {
		p.Hand = make([]uint64, len(handComp.Cards))
		for i := range handComp.Cards {
			p.Hand[i] = 0
		}
	}

	p.Board = make([]uint64, len(boardComp.Minions))
	for i, id := range boardComp.Minions {
		p.Board[i] = uint64(id)
	}

	return p
}

func (g *GameInstance) buildCard(card *ecs.Entity, viewerID string) *pb.Card {
	cardComp, _ := card.GetComponent("card").(*components.CardComponent)
	ownerComp, _ := card.GetComponent("owner").(*components.OwnerComponent)

	c := &pb.Card{
		EntityId:   uint64(card.ID),
		CardId:     cardComp.CardID,
		TemplateId: cardComp.TemplateID,
		Type:       pb.CardType(cardComp.Type),
		Cost:       int32(cardComp.Cost),
		Name:       cardComp.Name,
		Description: cardComp.Description,
		Rarity:     cardComp.Rarity,
	}

	if ownerComp.PlayerID != viewerID {
		if handComp, ok := card.GetComponent("hand"); ok {
			_ = handComp
			c.Name = "???"
			c.Description = ""
			c.Type = pb.CardType_CARD_TYPE_UNSPECIFIED
			c.Cost = 0
			return c
		}
	}

	switch cardComp.Type {
	case components.CardTypeMinion:
		if minionComp, ok := card.GetComponent("minion").(*components.MinionComponent); ok {
			c.Attack = int32(minionComp.Attack)
			c.Health = int32(minionComp.Health)
			c.MaxHealth = int32(minionComp.MaxHealth)
			c.CanAttack = minionComp.CanAttack
			c.Taunt = minionComp.Taunt
			c.Charge = minionComp.Charge
		}
	case components.CardTypeSpell:
		if spellComp, ok := card.GetComponent("spell").(*components.SpellComponent); ok {
			c.SpellEffect = pb.SpellEffect(spellComp.Effect)
			c.SpellValue = int32(spellComp.Value)
		}
	case components.CardTypeWeapon:
		if weaponComp, ok := card.GetComponent("weapon").(*components.WeaponComponent); ok {
			c.Attack = int32(weaponComp.Attack)
			c.Durability = int32(weaponComp.Durability)
		}
	}

	if _, ok := card.GetComponent("frozen"); ok {
		c.Frozen = true
	}

	return c
}

func (g *GameInstance) getPlayerEntity(playerID string) *ecs.Entity {
	players := g.World.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"player"},
	})

	for _, p := range players {
		if playerComp, ok := p.GetComponent("player").(*components.PlayerComponent); ok {
			if playerComp.PlayerID == playerID {
				return p
			}
		}
	}
	return nil
}

func (g *GameInstance) Subscribe(playerID string) chan *pb.GameFrame {
	g.mu.Lock()
	defer g.mu.Unlock()

	ch := make(chan *pb.GameFrame, 100)
	g.streams[playerID] = ch

	initialFrame := &pb.GameFrame{
		FrameNumber: g.getFrameNumber(),
		Status:      g.buildGameStatus(playerID),
	}
	ch <- initialFrame

	return ch
}

func (g *GameInstance) Unsubscribe(playerID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if ch, ok := g.streams[playerID]; ok {
		close(ch)
		delete(g.streams, playerID)
	}
}

func (g *GameInstance) IsFinished() bool {
	state := g.TurnSystem.GetGameState(g.World)
	return state != nil && state.State == components.GameStateFinished
}

func (g *GameInstance) GetWinner() string {
	state := g.TurnSystem.GetGameState(g.World)
	if state != nil {
		return state.Winner
	}
	return ""
}

func (g *GameInstance) GetTurns() int {
	state := g.TurnSystem.GetGameState(g.World)
	if state != nil {
		return state.Turn
	}
	return 0
}

func (g *GameInstance) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for playerID, ch := range g.streams {
		close(ch)
		delete(g.streams, playerID)
	}
	g.World.Close()
}
