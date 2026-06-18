package game

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/ecscard/game/internal/ai"
	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game/components"
	"github.com/ecscard/game/internal/game/systems"
	"github.com/ecscard/game/internal/lockstep"
	pb "github.com/ecscard/game/internal/proto"
)

const (
	TickIntervalMs     = 30
	SnapshotIntervalMs = 100
	GameFrameDuration  = SnapshotIntervalMs
)

type ActionWithValid struct {
	Action      *pb.Action
	Validated   bool
	Applied     bool
	Rollback    bool
}

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
	Player1Name  string
	Player2Name  string
	CreatedAt    time.Time
	StartedAt    time.Time
	EndedAt      time.Time
	DurationMs   int64
	IsAIGame     bool
	AIDifficulty string
	aiAgent      *ai.PPOAgent
	aiPlayerID   string
	aiThinking   bool

	mu sync.RWMutex

	streams      map[string]chan *pb.GameFrame
	eventSeq     uint64

	lockstep *lockstep.LockstepEngine

	confirmedActions map[uint64][]ActionWithValid
	rollbackInProgress bool
	lastSnapshotFrame  uint64

	ctx    context.Context
	cancel context.CancelFunc
}

func NewGameInstance(gameID, matchID, player1ID, player2ID, player1Name, player2Name string, isAI bool, aiDifficulty string) *GameInstance {
	world := ecs.NewWorld()

	cardSystem := systems.NewCardSystem()
	drawSystem := systems.NewDrawSystem()
	combatSystem := systems.NewCombatSystem()
	turnSystem := systems.NewTurnSystem()

	world.AddSystem(cardSystem)
	world.AddSystem(drawSystem)
	world.AddSystem(combatSystem)
	world.AddSystem(turnSystem)

	ctx, cancel := context.WithCancel(context.Background())

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
		Player1Name:  player1Name,
		Player2Name:  player2Name,
		CreatedAt:    time.Now(),
		IsAIGame:     isAI,
		AIDifficulty: aiDifficulty,
		streams:      make(map[string]chan *pb.GameFrame),
		confirmedActions: make(map[uint64][]ActionWithValid),
		ctx:          ctx,
		cancel:       cancel,
	}

	gi.lockstep = lockstep.NewLockstepEngine(gameID, world)
	gi.lockstep.RegisterPlayer(player1ID)
	gi.lockstep.RegisterPlayer(player2ID)

	gi.lockstep.SetCallbacks(
		gi.handleRollback,
		gi.handleSnapshotCreated,
		gi.handleDesync,
	)

	if isAI {
		var difficulty ai.AIDifficulty
		switch aiDifficulty {
		case "easy":
			difficulty = ai.AIDifficultyEasy
		case "normal":
			difficulty = ai.AIDifficultyNormal
		case "hard":
			difficulty = ai.AIDifficultyHard
		default:
			difficulty = ai.AIDifficultyNormal
		}
		agent, _ := ai.NewPPOAgent("", difficulty)
		gi.aiAgent = agent
		gi.aiPlayerID = player2ID
		go gi.aiTurnLoop()
	}

	turnSystem.StartGame(world, player1ID, player2ID, player1Name, player2Name)
	gi.StartedAt = time.Now()

	initialStatus := gi.buildInternalStatus()
	_ = gi.lockstep.GenerateSnapshot(initialStatus)
	gi.lastSnapshotFrame = 1

	go gi.startGameLoop()
	go gi.snapshotLoop()
	go gi.listenEvents()

	return gi
}

func (g *GameInstance) startGameLoop() {
	ticker := time.NewTicker(time.Duration(TickIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.processTick()
		}
	}
}

func (g *GameInstance) snapshotLoop() {
	ticker := time.NewTicker(time.Duration(SnapshotIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.takeSnapshot()
		}
	}
}

func (g *GameInstance) aiTurnLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			if g.IsFinished() {
				return
			}

			g.mu.RLock()
			isAITurn := g.TurnSystem.IsPlayerTurn(g.World, g.aiPlayerID)
			thinking := g.aiThinking
			g.mu.RUnlock()

			if isAITurn && !thinking {
				g.aiChooseAction()

				var minDelay, maxDelay int
				switch g.AIDifficulty {
				case "easy":
					minDelay = 600
					maxDelay = 1000
				case "normal":
					minDelay = 400
					maxDelay = 800
				case "hard":
					minDelay = 200
					maxDelay = 500
				default:
					minDelay = 400
					maxDelay = 800
				}
				delay := minDelay + rand.Intn(maxDelay-minDelay)
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}
	}
}

func (g *GameInstance) aiChooseAction() {
	g.mu.Lock()
	g.aiThinking = true
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		g.aiThinking = false
		g.mu.Unlock()
	}()

	if g.aiAgent == nil {
		return
	}

	status := g.buildInternalStatus()
	if status == nil {
		return
	}

	action := g.aiAgent.ChooseAction(g.aiPlayerID, status)
	if action == nil || !action.Valid {
		return
	}

	currentSnap := g.lockstep.GetCurrentSnapshot()
	if currentSnap == nil {
		return
	}
	baseFrame := currentSnap.FrameNumber
	baseHash := currentSnap.Hash

	var selfPlayer, oppPlayer *pb.Player
	for _, p := range status.Players {
		if p.PlayerId == g.aiPlayerID {
			selfPlayer = p
		} else {
			oppPlayer = p
		}
	}

	if selfPlayer == nil {
		return
	}

	switch action.Type {
	case ai.ActionPlayCard:
		if action.CardIdx < 0 || action.CardIdx >= len(selfPlayer.Hand) {
			return
		}
		cardID := ecs.EntityID(selfPlayer.Hand[action.CardIdx])

		var targetID ecs.EntityID
		if action.TargetIdx == -1 {
			targetID = 0
		} else {
			targetIdx := action.TargetIdx
			found := false
			if oppPlayer != nil {
				if targetIdx < len(oppPlayer.Board) {
					targetID = ecs.EntityID(oppPlayer.Board[targetIdx])
					found = true
				}
				targetIdx -= len(oppPlayer.Board)
			}
			if !found && targetIdx >= 0 && targetIdx < len(selfPlayer.Board) {
				targetID = ecs.EntityID(selfPlayer.Board[targetIdx])
			}
		}

		g.PlayCard(g.aiPlayerID, cardID, targetID, baseFrame, baseHash, 0)

	case ai.ActionAttack:
		var attackerID ecs.EntityID
		if action.CardIdx == ai.MaxBoardMinions {
			if selfPlayer.Weapon != 0 {
				attackerID = ecs.EntityID(selfPlayer.Weapon)
			} else {
				return
			}
		} else if action.CardIdx >= 0 && action.CardIdx < len(selfPlayer.Board) {
			attackerID = ecs.EntityID(selfPlayer.Board[action.CardIdx])
		} else {
			return
		}

		var targetID ecs.EntityID
		if action.TargetIdx == -1 {
			targetID = 0
		} else if oppPlayer != nil && action.TargetIdx >= 0 && action.TargetIdx < len(oppPlayer.Board) {
			targetID = ecs.EntityID(oppPlayer.Board[action.TargetIdx])
		} else {
			return
		}

		g.Attack(g.aiPlayerID, attackerID, targetID, baseFrame, baseHash, 0)

	case ai.ActionEndTurn:
		g.EndTurn(g.aiPlayerID, baseFrame, baseHash, 0)
	}
}

func (g *GameInstance) processTick() {
	g.mu.Lock()
	defer g.mu.Unlock()

	state := g.TurnSystem.GetGameState(g.World)
	if state != nil && state.State == components.GameStateFinished {
		if g.EndedAt.IsZero() {
			g.EndedAt = time.Now()
			g.DurationMs = g.EndedAt.Sub(g.StartedAt).Milliseconds()
		}
		return
	}

	g.World.Update(float64(TickIntervalMs) / 1000.0)
}

func (g *GameInstance) takeSnapshot() {
	g.mu.Lock()
	defer g.mu.Unlock()

	state := g.TurnSystem.GetGameState(g.World)
	if state != nil && state.State == components.GameStateFinished {
		return
	}

	status := g.buildInternalStatus()
	snap := g.lockstep.GenerateSnapshot(status)
	g.lastSnapshotFrame = snap.FrameNumber

	g.confirmedActions[snap.FrameNumber] = make([]ActionWithValid, 0)

	protoSnap := g.lockstep.ConvertToProtoSnapshot(snap)
	frame := &pb.GameFrame{
		FrameNumber:      snap.FrameNumber,
		Status:           snap.Status,
		SnapshotInterval: SnapshotIntervalMs,
		LatestSnapshot:   protoSnap,
	}

	g.broadcastFrame(frame)
}

func (g *GameInstance) listenEvents() {
	for event := range g.World.Events() {
		g.broadcastEvent(event)
	}
}

func (g *GameInstance) broadcastEvent(event *ecs.Event) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	currentSnap := g.lockstep.GetCurrentSnapshot()
	frameNum := uint64(0)
	if currentSnap != nil {
		frameNum = currentSnap.FrameNumber
	}

	frame := &pb.GameFrame{
		FrameNumber:      frameNum,
		SnapshotInterval: SnapshotIntervalMs,
		Events: []*pb.GameEvent{
			{
				Sequence:    g.nextEventSeq(),
				EventType:   event.Type,
				FrameNumber: frameNum,
				Data:        g.eventDataToStringMap(event.Data),
				EntityId:    uint64(event.Entity.ID),
				Timestamp:   time.Now().UnixNano() / int64(time.Millisecond),
			},
		},
	}

	if currentSnap != nil {
		frame.LatestSnapshot = g.lockstep.ConvertToProtoSnapshot(currentSnap)
	}

	g.broadcastFrameNoLock(frame)
}

func (g *GameInstance) broadcastFrame(frame *pb.GameFrame) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	g.broadcastFrameNoLock(frame)
}

func (g *GameInstance) broadcastFrameNoLock(frame *pb.GameFrame) {
	for _, ch := range g.streams {
		select {
		case ch <- frame:
		default:
		}
	}
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

func (g *GameInstance) validateAndApplyAction(action *pb.Action) (bool, *pb.GameSnapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.rollbackInProgress {
		return false, g.lockstep.ConvertToProtoSnapshot(g.lockstep.GetCurrentSnapshot())
	}

	valid, rollbackSnap := g.lockstep.SubmitAction(action)
	if !valid {
		return false, g.lockstep.ConvertToProtoSnapshot(rollbackSnap)
	}

	var applied bool
	switch action.Type {
	case pb.ActionType_ACTION_TYPE_PLAY_CARD:
		applied = g.applyPlayCard(action)
	case pb.ActionType_ACTION_TYPE_ATTACK:
		applied = g.applyAttack(action)
	case pb.ActionType_ACTION_TYPE_END_TURN:
		applied = g.applyEndTurn(action)
	case pb.ActionType_ACTION_TYPE_CONCEDE:
		applied = g.applyConcede(action)
	}

	currentSnap := g.lockstep.GetCurrentSnapshot()
	act := ActionWithValid{
		Action:    action,
		Validated: valid,
		Applied:   applied,
	}
	if currentSnap != nil {
		if _, ok := g.confirmedActions[currentSnap.FrameNumber]; !ok {
			g.confirmedActions[currentSnap.FrameNumber] = make([]ActionWithValid, 0)
		}
		g.confirmedActions[currentSnap.FrameNumber] = append(g.confirmedActions[currentSnap.FrameNumber], act)
	}

	return applied, nil
}

func (g *GameInstance) applyPlayCard(action *pb.Action) bool {
	if !g.TurnSystem.IsPlayerTurn(g.World, action.PlayerId) {
		return false
	}

	player := g.getPlayerEntity(action.PlayerId)
	if player == nil {
		return false
	}

	return g.CombatSystem.PlayCard(g.World, player, ecs.EntityID(action.CardId), ecs.EntityID(action.TargetId))
}

func (g *GameInstance) applyAttack(action *pb.Action) bool {
	if !g.TurnSystem.IsPlayerTurn(g.World, action.PlayerId) {
		return false
	}

	return g.CombatSystem.Attack(g.World, ecs.EntityID(action.CardId), ecs.EntityID(action.TargetId))
}

func (g *GameInstance) applyEndTurn(action *pb.Action) bool {
	return g.TurnSystem.EndTurn(g.World, action.PlayerId)
}

func (g *GameInstance) applyConcede(action *pb.Action) bool {
	state := g.TurnSystem.GetGameState(g.World)
	if state == nil || state.State == components.GameStateFinished {
		return false
	}

	opponentID := g.Player2ID
	if action.PlayerId == g.Player2ID {
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
		"player_id": action.PlayerId,
		"winner_id": opponentID,
	}, nil)

	return true
}

func (g *GameInstance) PlayCard(playerID string, cardID, targetID ecs.EntityID, baseFrame uint64, baseHash uint64, seq uint64) (bool, *pb.GameSnapshot) {
	action := &pb.Action{
		Sequence:           seq,
		Type:               pb.ActionType_ACTION_TYPE_PLAY_CARD,
		PlayerId:           playerID,
		BaseSnapshotFrame:  baseFrame,
		BaseSnapshotHash:   baseHash,
		CardId:             uint64(cardID),
		TargetId:           uint64(targetID),
		Timestamp:          time.Now().UnixNano() / int64(time.Millisecond),
	}
	return g.validateAndApplyAction(action)
}

func (g *GameInstance) Attack(playerID string, attackerID, targetID ecs.EntityID, baseFrame uint64, baseHash uint64, seq uint64) (bool, *pb.GameSnapshot) {
	action := &pb.Action{
		Sequence:           seq,
		Type:               pb.ActionType_ACTION_TYPE_ATTACK,
		PlayerId:           playerID,
		BaseSnapshotFrame:  baseFrame,
		BaseSnapshotHash:   baseHash,
		CardId:             uint64(attackerID),
		TargetId:           uint64(targetID),
		Timestamp:          time.Now().UnixNano() / int64(time.Millisecond),
	}
	return g.validateAndApplyAction(action)
}

func (g *GameInstance) EndTurn(playerID string, baseFrame uint64, baseHash uint64, seq uint64) (bool, *pb.GameSnapshot) {
	action := &pb.Action{
		Sequence:           seq,
		Type:               pb.ActionType_ACTION_TYPE_END_TURN,
		PlayerId:           playerID,
		BaseSnapshotFrame:  baseFrame,
		BaseSnapshotHash:   baseHash,
		Timestamp:          time.Now().UnixNano() / int64(time.Millisecond),
	}
	return g.validateAndApplyAction(action)
}

func (g *GameInstance) Concede(playerID string, seq uint64) bool {
	action := &pb.Action{
		Sequence:  seq,
		Type:      pb.ActionType_ACTION_TYPE_CONCEDE,
		PlayerId:  playerID,
		Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
	}
	result, _ := g.validateAndApplyAction(action)
	return result
}

func (g *GameInstance) handleRollback(snap *lockstep.GameSnapshot, reason pb.RollbackReason) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.rollbackInProgress = true
	defer func() { g.rollbackInProgress = false }()

	g.lockstep.RestoreFromSnapshot(snap)

	rollbackCmd := &pb.RollbackCommand{
		RollbackToFrame: snap.FrameNumber,
		RollbackHash:    snap.Hash,
		Snapshot:        g.lockstep.ConvertToProtoSnapshot(snap),
		Reason:          reason,
		Message:         fmt.Sprintf("Rollback requested: %v", reason),
	}

	frame := &pb.GameFrame{
		FrameNumber:      snap.FrameNumber,
		Status:           snap.Status,
		SnapshotInterval: SnapshotIntervalMs,
		LatestSnapshot:   g.lockstep.ConvertToProtoSnapshot(snap),
		Rollback:         rollbackCmd,
	}

	g.broadcastFrameNoLock(frame)
}

func (g *GameInstance) handleSnapshotCreated(snap *lockstep.GameSnapshot) {
}

func (g *GameInstance) handleDesync(playerID string, expected, actual uint64) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	g.World.EmitEvent("desync_detected", map[string]interface{}{
		"player_id":    playerID,
		"expected_hash": expected,
		"actual_hash":   actual,
	}, nil)
}

func (g *GameInstance) buildInternalStatus() *pb.GameStatus {
	return g.buildGameStatus("")
}

func (g *GameInstance) GetGameState(playerID string) *pb.GameStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.buildGameStatus(playerID)
}

func (g *GameInstance) GetSnapshot(frameNumber uint64) (*pb.GameSnapshot, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	snap, ok := g.lockstep.GetSnapshot(frameNumber)
	if !ok {
		return nil, false
	}
	return g.lockstep.ConvertToProtoSnapshot(snap), true
}

func (g *GameInstance) GetCurrentSnapshot() *pb.GameSnapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.lockstep.ConvertToProtoSnapshot(g.lockstep.GetCurrentSnapshot())
}

func (g *GameInstance) GetLatestFrame() uint64 {
	return g.lockstep.GetLatestSnapshotFrame()
}

func (g *GameInstance) ReceiveAck(ack *pb.FrameAck) {
	g.lockstep.ReceiveAck(ack)
}

func (g *GameInstance) GetPlayerNextSeq(playerID string) uint64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	acks := g.lockstep.GetAllPlayerAcks()
	return acks[playerID] + 1
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

	currentSnap := g.lockstep.GetCurrentSnapshot()
	snapFrame := uint64(0)
	snapHash := uint64(0)
	if currentSnap != nil {
		snapFrame = currentSnap.FrameNumber
		snapHash = currentSnap.Hash
	}

	return &pb.GameStatus{
		GameId:              g.GameID,
		State:               pb.GameState(state.State),
		Turn:                int32(state.Turn),
		CurrentTurnPlayerId: state.CurrentTurn,
		Phase:               pb.TurnPhase(state.Phase),
		FrameNumber:         state.FrameNumber,
		Winner:              state.Winner,
		Players:             players,
		Cards:               cards,
		SnapshotFrame:       snapFrame,
		SnapshotHash:        snapHash,
		Timestamp:           time.Now().UnixNano() / int64(time.Millisecond),
	}
}

func (g *GameInstance) buildPlayer(player *ecs.Entity, viewerID string) *pb.Player {
	playerComp, _ := player.GetComponent("player").(*components.PlayerComponent)
	heroComp, _ := player.GetComponent("hero").(*components.HeroComponent)
	manaComp, _ := player.GetComponent("mana").(*ecs.ManaComponent)
	handComp, _ := player.GetComponent("hand").(*components.HandComponent)
	boardComp, _ := player.GetComponent("board").(*components.BoardComponent)
	deckComp, _ := player.GetComponent("deck").(*components.DeckComponent)
	fatigueComp, _ := player.GetComponent("fatigue").(*components.FatigueComponent)

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
		LastAckFrame: g.lockstep.GetPlayerAck(playerComp.PlayerID),
	}
	if fatigueComp != nil {
		p.FatigueCounter = int32(fatigueComp.Counter)
	}

	if playerComp.PlayerID == viewerID || viewerID == "" {
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
	boardPos := int32(-1)

	if minionComp, ok := card.GetComponent("board_position"); ok {
		if bp, ok2 := minionComp.(*components.BoardPositionComponent); ok2 {
			boardPos = int32(bp.Position)
		}
	}

	c := &pb.Card{
		EntityId:      uint64(card.ID),
		CardId:        cardComp.CardID,
		TemplateId:    cardComp.TemplateID,
		Type:          pb.CardType(cardComp.CardType),
		Cost:          int32(cardComp.Cost),
		Name:          cardComp.Name,
		Description:   cardComp.Description,
		Rarity:        cardComp.Rarity,
		BoardPosition: boardPos,
	}

	if ownerComp.PlayerID != viewerID && viewerID != "" {
		if _, ok := card.GetComponent("hand"); ok {
			c.Name = "???"
			c.Description = ""
			c.Type = pb.CardType_CARD_TYPE_UNSPECIFIED
			c.Cost = 0
			return c
		}
	}

	switch cardComp.CardType {
	case components.CardTypeMinion:
		if minionComp, ok := card.GetComponent("minion").(*components.MinionComponent); ok {
			c.Attack = int32(minionComp.Attack)
			c.Health = int32(minionComp.Health)
			c.MaxHealth = int32(minionComp.MaxHealth)
			c.CanAttack = minionComp.CanAttack
			c.Taunt = minionComp.Taunt
			c.Charge = minionComp.Charge
			if ds, ok := card.GetComponent("divine_shield"); ok && ds != nil {
				c.DivineShield = true
			}
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

	ch := make(chan *pb.GameFrame, 256)
	g.streams[playerID] = ch

	currentSnap := g.lockstep.GetCurrentSnapshot()
	initialFrame := &pb.GameFrame{
		FrameNumber:      g.GetLatestFrame(),
		Status:           g.buildGameStatus(playerID),
		SnapshotInterval: SnapshotIntervalMs,
		LatestSnapshot:   g.lockstep.ConvertToProtoSnapshot(currentSnap),
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
	g.cancel()

	g.mu.Lock()
	defer g.mu.Unlock()

	for playerID, ch := range g.streams {
		close(ch)
		delete(g.streams, playerID)
	}
	if g.aiAgent != nil {
		g.aiAgent.Close()
		g.aiAgent = nil
	}
	g.World.Close()
}

func (g *GameInstance) SnapshotInterval() uint64 {
	return SnapshotIntervalMs
}
