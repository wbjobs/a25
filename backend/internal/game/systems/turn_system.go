package systems

import (
	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game/components"
)

type TurnSystem struct {
	ecs.BaseSystem
}

func NewTurnSystem() *TurnSystem {
	return &TurnSystem{}
}

func (s *TurnSystem) Name() string { return "turn_system" }

func (s *TurnSystem) Update(world *ecs.World, dt float64) {
	s.checkGameOver(world)
}

func (s *TurnSystem) StartGame(world *ecs.World, player1ID, player2ID string, player1Name, player2Name string) {
	gameState := ecs.NewEntity()
	gameState.AddComponent(&components.GameStateComponent{
		State:       components.GameStatePlaying,
		Turn:        1,
		CurrentTurn: player1ID,
		Phase:       components.TurnPhaseBegin,
		FrameNumber: 0,
	})
	world.AddEntity(gameState)

	player1 := s.createPlayer(world, player1ID, player1Name, false)
	player2 := s.createPlayer(world, player2ID, player2Name, false)

	cardSystem := NewCardSystem(nil)
	drawSystem := NewDrawSystem()

	templates1 := s.generateDeck()
	templates2 := s.generateDeck()

	drawSystem.CreateDeck(world, player1, templates1, cardSystem)
	drawSystem.CreateDeck(world, player2, templates2, cardSystem)

	drawSystem.ShuffleDeck(world, player1)
	drawSystem.ShuffleDeck(world, player2)

	drawSystem.DrawCard(world, player1, 3)
	drawSystem.DrawCard(world, player2, 4)

	s.startTurn(world, player1)

	world.EmitEvent("game_started", map[string]interface{}{
		"player1_id":   player1ID,
		"player1_name": player1Name,
		"player2_id":   player2ID,
		"player2_name": player2Name,
	}, gameState)
}

func (s *TurnSystem) createPlayer(world *ecs.World, playerID, playerName string, isAI bool) *ecs.Entity {
	player := ecs.NewEntity()
	player.AddComponent(&components.PlayerComponent{
		PlayerID:   playerID,
		PlayerName: playerName,
		IsAI:       isAI,
		Connected:  true,
	})
	player.AddComponent(&components.HeroComponent{
		PlayerID:  playerID,
		Health:    20,
		MaxHealth: 20,
		Attack:    0,
		Armor:     0,
	})
	player.AddComponent(&ecs.ManaComponent{
		CurrentMana: 0,
		MaxMana:     0,
	})
	player.AddComponent(&components.OwnerComponent{PlayerID: playerID})
	player.AddComponent(&components.FatigueComponent{Counter: 0})

	world.AddEntity(player)
	return player
}

func (s *TurnSystem) EndTurn(world *ecs.World, playerID string) bool {
	gameState := s.getGameState(world)
	if gameState == nil {
		return false
	}

	stateComp, _ := gameState.GetComponent("game_state").(*components.GameStateComponent)
	if stateComp.CurrentTurn != playerID {
		return false
	}
	if stateComp.Phase != components.TurnPhaseMain {
		return false
	}

	stateComp.Phase = components.TurnPhaseEnd

	currentPlayer := s.getPlayerByID(world, playerID)
	s.endTurnEffects(world, currentPlayer)

	opponent := s.getOpponentPlayer(world, playerID)

	stateComp.Turn++
	stateComp.CurrentTurn = s.getPlayerID(opponent)
	stateComp.Phase = components.TurnPhaseBegin
	stateComp.FrameNumber++

	s.startTurn(world, opponent)

	world.EmitEvent("turn_ended", map[string]interface{}{
		"player_id":     playerID,
		"next_player":   s.getPlayerID(opponent),
		"turn_number":   stateComp.Turn,
	}, gameState)

	return true
}

func (s *TurnSystem) startTurn(world *ecs.World, player *ecs.Entity) {
	manaComp, _ := player.GetComponent("mana").(*ecs.ManaComponent)
	if manaComp.MaxMana < 10 {
		manaComp.MaxMana++
	}
	manaComp.CurrentMana = manaComp.MaxMana

	s.resetMinionAttacks(world, player)

	drawSystem := NewDrawSystem()
	drawSystem.DrawCard(world, player, 1)

	gameState := s.getGameState(world)
	if gameState != nil {
		if stateComp, ok := gameState.GetComponent("game_state").(*components.GameStateComponent); ok {
			stateComp.Phase = components.TurnPhaseMain
		}
	}

	world.EmitEvent("turn_started", map[string]interface{}{
		"player_id": s.getPlayerID(player),
		"mana":      manaComp.CurrentMana,
		"max_mana":  manaComp.MaxMana,
	}, player)
}

func (s *TurnSystem) endTurnEffects(world *ecs.World, player *ecs.Entity) {
	s.removeFrozenEffects(world, player)
}

func (s *TurnSystem) resetMinionAttacks(world *ecs.World, player *ecs.Entity) {
	boardComp, ok := player.GetComponent("board").(*components.BoardComponent)
	if !ok {
		return
	}

	for _, minionID := range boardComp.Minions {
		if minion, ok := world.GetEntity(minionID); ok {
			if minionComp, ok := minion.GetComponent("minion").(*components.MinionComponent); ok {
				minionComp.CanAttack = true
				minionComp.AttacksThisTurn = 0
				if _, ok := minion.GetComponent("windfury").(*components.WindfuryComponent); ok {
					minionComp.MaxAttacks = 2
				} else {
					minionComp.MaxAttacks = 1
				}
			}
		}
	}
}

func (s *TurnSystem) removeFrozenEffects(world *ecs.World, player *ecs.Entity) {
	boardComp, ok := player.GetComponent("board").(*components.BoardComponent)
	if !ok {
		return
	}

	for _, minionID := range boardComp.Minions {
		if minion, ok := world.GetEntity(minionID); ok {
			if frozen, ok := minion.GetComponent("frozen").(*components.FrozenComponent); ok {
				frozen.Duration--
				if frozen.Duration <= 0 {
					minion.RemoveComponent("frozen")
				}
			}
		}
	}
}

func (s *TurnSystem) checkGameOver(world *ecs.World) {
	players := world.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"player", "hero"},
	})

	for _, player := range players {
		if heroComp, ok := player.GetComponent("hero").(*components.HeroComponent); ok {
			if heroComp.Health <= 0 {
				s.endGame(world, player)
				return
			}
		}
	}
}

func (s *TurnSystem) endGame(world *ecs.World, losingPlayer *ecs.Entity) {
	gameState := s.getGameState(world)
	if gameState == nil {
		return
	}

	stateComp, _ := gameState.GetComponent("game_state").(*components.GameStateComponent)
	stateComp.State = components.GameStateFinished

	winner := s.getOpponentPlayer(world, s.getPlayerID(losingPlayer))
	if winner != nil {
		stateComp.Winner = s.getPlayerID(winner)
	}

	world.EmitEvent("game_ended", map[string]interface{}{
		"winner_id":     stateComp.Winner,
		"loser_id":      s.getPlayerID(losingPlayer),
		"turn_number":   stateComp.Turn,
	}, gameState)
}

func (s *TurnSystem) IsPlayerTurn(world *ecs.World, playerID string) bool {
	gameState := s.getGameState(world)
	if gameState == nil {
		return false
	}

	stateComp, ok := gameState.GetComponent("game_state").(*components.GameStateComponent)
	if !ok {
		return false
	}

	return stateComp.CurrentTurn == playerID && stateComp.State == components.GameStatePlaying
}

func (s *TurnSystem) GetGameState(world *ecs.World) *components.GameStateComponent {
	gameState := s.getGameState(world)
	if gameState == nil {
		return nil
	}

	stateComp, _ := gameState.GetComponent("game_state").(*components.GameStateComponent)
	return stateComp
}

func (s *TurnSystem) getGameState(world *ecs.World) *ecs.Entity {
	query := &ecs.EntityQuery{
		RequiredComponents: []string{"game_state"},
	}
	entities := world.Query(query)
	if len(entities) > 0 {
		return entities[0]
	}
	return nil
}

func (s *TurnSystem) getPlayerByID(world *ecs.World, playerID string) *ecs.Entity {
	players := world.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"player"},
	})

	for _, player := range players {
		if comp, ok := player.GetComponent("player").(*components.PlayerComponent); ok {
			if comp.PlayerID == playerID {
				return player
			}
		}
	}
	return nil
}

func (s *TurnSystem) getOpponentPlayer(world *ecs.World, playerID string) *ecs.Entity {
	players := world.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"player"},
	})

	for _, player := range players {
		if comp, ok := player.GetComponent("player").(*components.PlayerComponent); ok {
			if comp.PlayerID != playerID {
				return player
			}
		}
	}
	return nil
}

func (s *TurnSystem) getPlayerID(player *ecs.Entity) string {
	if playerComp, ok := player.GetComponent("player").(*components.PlayerComponent); ok {
		return playerComp.PlayerID
	}
	return ""
}

func (s *TurnSystem) generateDeck() []*CardTemplate {
	return GenerateDefaultDeck()
}

func GenerateDefaultDeck() []*CardTemplate {
	return GenerateDeckFromIDs([]string{
		"minion_001", "minion_001", "minion_002", "minion_002",
		"minion_003", "minion_003", "minion_004", "minion_004",
		"minion_005", "minion_005", "minion_006", "minion_006",
		"minion_007",
		"spell_001", "spell_001", "spell_002", "spell_002",
		"spell_003", "spell_003", "spell_004", "spell_004",
		"spell_005",
		"weapon_001", "weapon_001", "weapon_002", "weapon_002",
		"weapon_003",
	})
}

func GenerateDeckFromIDs(ids []string) []*CardTemplate {
	templates := make([]*CardTemplate, 0, len(ids))
	for _, id := range ids {
		if tpl := GetCardTemplateByID(id); tpl != nil {
			templates = append(templates, tpl)
		}
	}
	return templates
}

func GetCardTemplateByID(id string) *CardTemplate {
	for _, t := range CardTemplateRegistry {
		if t.ID == id {
			return t
		}
	}
	return nil
}

var CardTemplateRegistry = []*CardTemplate{}

func RegisterCardTemplate(tpl *CardTemplate) {
	CardTemplateRegistry = append(CardTemplateRegistry, tpl)
}
