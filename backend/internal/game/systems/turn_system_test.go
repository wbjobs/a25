package systems

import (
	"testing"

	"cardgame/internal/ecs"
	"cardgame/internal/game/components"
	"github.com/stretchr/testify/assert"
)

func TestTurnSystem_StartGame(t *testing.T) {
	world := ecs.NewWorld()
	turnSystem := NewTurnSystem()
	cardSystem := NewCardSystem()

	world.AddSystem(turnSystem)
	world.AddSystem(cardSystem)

	player1ID := "player_1"
	player2ID := "player_2"
	player1Name := "Alice"
	player2Name := "Bob"

	turnSystem.StartGame(world, player1ID, player2ID, player1Name, player2Name)

	gameState := components.GetGameStateComponent(world)
	assert.NotNil(t, gameState)
	assert.Equal(t, 1, gameState.Turn)
	assert.Equal(t, player1ID, gameState.CurrentTurnPlayerID)
	assert.Equal(t, components.GameStatePlaying, gameState.State)

	player1Entity := turnSystem.GetPlayerEntity(world, player1ID)
	player2Entity := turnSystem.GetPlayerEntity(world, player2ID)

	assert.NotNil(t, player1Entity)
	assert.NotNil(t, player2Entity)

	player1Comp := components.GetPlayerComponent(world, player1Entity)
	player2Comp := components.GetPlayerComponent(world, player2Entity)

	assert.NotNil(t, player1Comp)
	assert.NotNil(t, player2Comp)

	assert.Equal(t, 20, player1Comp.Health)
	assert.Equal(t, 20, player2Comp.Health)

	assert.Equal(t, 1, player1Comp.MaxMana)
	assert.Equal(t, 0, player2Comp.MaxMana)

	assert.Equal(t, 1, player1Comp.Mana)
	assert.Equal(t, 0, player2Comp.Mana)

	assert.Equal(t, 30, player1Comp.DeckSize)
	assert.Equal(t, 30, player2Comp.DeckSize)

	assert.Equal(t, 3, len(player1Comp.Hand))
	assert.Equal(t, 4, len(player2Comp.Hand))
}

func TestTurnSystem_EndTurn(t *testing.T) {
	world := ecs.NewWorld()
	turnSystem := NewTurnSystem()
	cardSystem := NewCardSystem()
	combatSystem := NewCombatSystem()
	drawSystem := NewDrawSystem()

	world.AddSystem(turnSystem)
	world.AddSystem(cardSystem)
	world.AddSystem(combatSystem)
	world.AddSystem(drawSystem)

	player1ID := "player_1"
	player2ID := "player_2"

	turnSystem.StartGame(world, player1ID, player2ID, "Alice", "Bob")

	turnSystem.EndTurn(world)

	gameState := components.GetGameStateComponent(world)
	assert.Equal(t, 2, gameState.Turn)
	assert.Equal(t, player2ID, gameState.CurrentTurnPlayerID)

	player2Entity := turnSystem.GetPlayerEntity(world, player2ID)
	player2Comp := components.GetPlayerComponent(world, player2Entity)
	assert.Equal(t, 2, player2Comp.MaxMana)
	assert.Equal(t, 2, player2Comp.Mana)
	assert.Equal(t, 5, len(player2Comp.Hand))
}

func TestTurnSystem_MultipleTurns(t *testing.T) {
	world := ecs.NewWorld()
	turnSystem := NewTurnSystem()
	cardSystem := NewCardSystem()

	world.AddSystem(turnSystem)
	world.AddSystem(cardSystem)

	player1ID := "player_1"
	player2ID := "player_2"

	turnSystem.StartGame(world, player1ID, player2ID, "Alice", "Bob")

	for i := 0; i < 10; i++ {
		turnSystem.EndTurn(world)
	}

	gameState := components.GetGameStateComponent(world)
	assert.Equal(t, 11, gameState.Turn)

	player1Entity := turnSystem.GetPlayerEntity(world, player1ID)
	player1Comp := components.GetPlayerComponent(world, player1Entity)
	assert.Equal(t, 6, player1Comp.MaxMana)
}

func TestTurnSystem_GameOver_HeroDeath(t *testing.T) {
	world := ecs.NewWorld()
	turnSystem := NewTurnSystem()
	cardSystem := NewCardSystem()
	combatSystem := NewCombatSystem()

	world.AddSystem(turnSystem)
	world.AddSystem(cardSystem)
	world.AddSystem(combatSystem)

	player1ID := "player_1"
	player2ID := "player_2"

	turnSystem.StartGame(world, player1ID, player2ID, "Alice", "Bob")

	player1Entity := turnSystem.GetPlayerEntity(world, player1ID)
	healthComp := components.GetHealthComponent(world, player1Entity)
	healthComp.CurrentHP = 0
	world.SetComponent(player1Entity.ID, healthComp)

	turnSystem.checkGameOver(world)

	gameState := components.GetGameStateComponent(world)
	assert.Equal(t, components.GameStateFinished, gameState.State)
	assert.Equal(t, player2ID, gameState.WinnerID)
}

func TestTurnSystem_FatigueDamage(t *testing.T) {
	world := ecs.NewWorld()
	turnSystem := NewTurnSystem()
	cardSystem := NewCardSystem()
	drawSystem := NewDrawSystem()

	world.AddSystem(turnSystem)
	world.AddSystem(cardSystem)
	world.AddSystem(drawSystem)

	player1ID := "player_1"
	player2ID := "player_2"

	turnSystem.StartGame(world, player1ID, player2ID, "Alice", "Bob")

	player1Entity := turnSystem.GetPlayerEntity(world, player1ID)
	player1Comp := components.GetPlayerComponent(world, player1Entity)
	player1Comp.DeckSize = 0
	player1Comp.Hand = nil
	world.SetComponent(player1Entity.ID, player1Comp)

	initialHealth := components.GetHealthComponent(world, player1Entity).CurrentHP

	turnSystem.EndTurn(world)

	newHealth := components.GetHealthComponent(world, player1Entity).CurrentHP
	assert.Equal(t, initialHealth-1, newHealth)

	turnSystem.EndTurn(world)
	turnSystem.EndTurn(world)

	newHealth = components.GetHealthComponent(world, player1Entity).CurrentHP
	assert.Equal(t, initialHealth-1-2, newHealth)
}
