package systems

import (
	"testing"

	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game/components"
	"github.com/stretchr/testify/assert"
)

func TestCombatSystem_PlayMinionCard(t *testing.T) {
	world := ecs.NewWorld()
	cardSystem := NewCardSystem()
	combatSystem := NewCombatSystem()
	drawSystem := NewDrawSystem()
	turnSystem := NewTurnSystem()

	world.AddSystem(cardSystem)
	world.AddSystem(combatSystem)
	world.AddSystem(drawSystem)
	world.AddSystem(turnSystem)

	playerID := "player_1"
	turnSystem.StartGame(world, playerID, "player_2", "Player1", "Player2")

	template := GetCardTemplateByID("minion_1")
	cardEntity := cardSystem.CreateCard(world, template, playerID)

	playerEntity := turnSystem.GetPlayerEntity(world, playerID)

	success := combatSystem.PlayCard(world, playerEntity, cardEntity.ID, 0)
	assert.True(t, success, "Should be able to play minion card")

	cardComp := components.GetCardComponent(world, cardEntity)
	assert.NotNil(t, cardComp)
	assert.False(t, cardComp.IsInHand)
	assert.True(t, cardComp.IsOnBoard)

	playerComp := components.GetPlayerComponent(world, playerEntity)
	assert.NotNil(t, playerComp)
	assert.Equal(t, 10-template.Cost, playerComp.Mana)
}

func TestCombatSystem_PlaySpellCard_Damage(t *testing.T) {
	world := ecs.NewWorld()
	cardSystem := NewCardSystem()
	combatSystem := NewCombatSystem()
	drawSystem := NewDrawSystem()
	turnSystem := NewTurnSystem()

	world.AddSystem(cardSystem)
	world.AddSystem(combatSystem)
	world.AddSystem(drawSystem)
	world.AddSystem(turnSystem)

	player1ID := "player_1"
	player2ID := "player_2"
	turnSystem.StartGame(world, player1ID, player2ID, "Player1", "Player2")

	template := GetCardTemplateByID("spell_1")
	cardEntity := cardSystem.CreateCard(world, template, player1ID)

	player1Entity := turnSystem.GetPlayerEntity(world, player1ID)
	player2Entity := turnSystem.GetPlayerEntity(world, player2ID)

	initialHealth := components.GetHealthComponent(world, player2Entity).CurrentHP

	targetID := player2Entity.ID
	success := combatSystem.PlayCard(world, player1Entity, cardEntity.ID, targetID)
	assert.True(t, success, "Should be able to play damage spell")

	newHealth := components.GetHealthComponent(world, player2Entity).CurrentHP
	assert.Equal(t, initialHealth-6, newHealth, "Target should take 6 damage")
}

func TestCombatSystem_Attack(t *testing.T) {
	world := ecs.NewWorld()
	cardSystem := NewCardSystem()
	combatSystem := NewCombatSystem()
	drawSystem := NewDrawSystem()
	turnSystem := NewTurnSystem()

	world.AddSystem(cardSystem)
	world.AddSystem(combatSystem)
	world.AddSystem(drawSystem)
	world.AddSystem(turnSystem)

	player1ID := "player_1"
	player2ID := "player_2"
	turnSystem.StartGame(world, player1ID, player2ID, "Player1", "Player2")

	minionTemplate := GetCardTemplateByID("minion_1")
	attacker := cardSystem.CreateCard(world, minionTemplate, player1ID)

	defenderTemplate := GetCardTemplateByID("minion_2")
	defender := cardSystem.CreateCard(world, defenderTemplate, player2ID)

	player1Entity := turnSystem.GetPlayerEntity(world, player1ID)
	combatSystem.PlayCard(world, player1Entity, attacker.ID, 0)

	player2Entity := turnSystem.GetPlayerEntity(world, player2ID)
	combatSystem.PlayCard(world, player2Entity, defender.ID, 0)

	attackerMinion := components.GetMinionComponent(world, attacker)
	attackerMinion.CanAttack = true
	world.SetComponent(attacker.ID, attackerMinion)

	initialAttackerHealth := components.GetHealthComponent(world, attacker).CurrentHP
	initialDefenderHealth := components.GetHealthComponent(world, defender).CurrentHP

	success := combatSystem.Attack(world, attacker.ID, defender.ID)
	assert.True(t, success, "Attack should succeed")

	attackerHealth := components.GetHealthComponent(world, attacker).CurrentHP
	defenderHealth := components.GetHealthComponent(world, defender).CurrentHP

	attackerCard := components.GetCardComponent(world, attacker)
	defenderCard := components.GetCardComponent(world, defender)

	assert.Equal(t, initialAttackerHealth-defenderCard.Attack, attackerHealth)
	assert.Equal(t, initialDefenderHealth-attackerCard.Attack, defenderHealth)
}

func TestCombatSystem_TauntMechanic(t *testing.T) {
	world := ecs.NewWorld()
	cardSystem := NewCardSystem()
	combatSystem := NewCombatSystem()
	drawSystem := NewDrawSystem()
	turnSystem := NewTurnSystem()

	world.AddSystem(cardSystem)
	world.AddSystem(combatSystem)
	world.AddSystem(drawSystem)
	world.AddSystem(turnSystem)

	player1ID := "player_1"
	player2ID := "player_2"
	turnSystem.StartGame(world, player1ID, player2ID, "Player1", "Player2")

	attackerTemplate := GetCardTemplateByID("minion_1")
	attacker := cardSystem.CreateCard(world, attackerTemplate, player1ID)

	tauntTemplate := GetCardTemplateByID("minion_6")
	tauntMinion := cardSystem.CreateCard(world, tauntTemplate, player2ID)

	normalTemplate := GetCardTemplateByID("minion_2")
	normalMinion := cardSystem.CreateCard(world, normalTemplate, player2ID)

	player1Entity := turnSystem.GetPlayerEntity(world, player1ID)
	player2Entity := turnSystem.GetPlayerEntity(world, player2ID)

	combatSystem.PlayCard(world, player1Entity, attacker.ID, 0)
	combatSystem.PlayCard(world, player2Entity, tauntMinion.ID, 0)
	combatSystem.PlayCard(world, player2Entity, normalMinion.ID, 0)

	attackerMinion := components.GetMinionComponent(world, attacker)
	attackerMinion.CanAttack = true
	world.SetComponent(attacker.ID, attackerMinion)

	success := combatSystem.Attack(world, attacker.ID, normalMinion.ID)
	assert.False(t, success, "Cannot attack non-taunt minion when taunt exists")

	success = combatSystem.Attack(world, attacker.ID, tauntMinion.ID)
	assert.True(t, success, "Can attack taunt minion")
}
