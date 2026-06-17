package systems

import (
	"math/rand"

	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game/components"
)

type DrawSystem struct {
	ecs.BaseSystem
}

func NewDrawSystem() *DrawSystem {
	return &DrawSystem{}
}

func (s *DrawSystem) Name() string { return "draw_system" }

func (s *DrawSystem) Update(world *ecs.World, dt float64) {
}

func (s *DrawSystem) ShuffleDeck(world *ecs.World, playerEntity *ecs.Entity) {
	deckComp, ok := playerEntity.GetComponent("deck").(*components.DeckComponent)
	if !ok {
		return
	}

	shuffled := make([]ecs.EntityID, len(deckComp.Cards))
	copy(shuffled, deckComp.Cards)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	deckComp.Cards = shuffled

	world.EmitEvent("deck_shuffled", map[string]interface{}{
		"player_id": s.getPlayerID(playerEntity),
		"deck_size": len(deckComp.Cards),
	}, playerEntity)
}

func (s *DrawSystem) DrawCard(world *ecs.World, playerEntity *ecs.Entity, count int) []ecs.EntityID {
	deckComp, ok := playerEntity.GetComponent("deck").(*components.DeckComponent)
	if !ok {
		return nil
	}

	handComp, ok := playerEntity.GetComponent("hand").(*components.HandComponent)
	if !ok {
		return nil
	}

	drawnCards := make([]ecs.EntityID, 0)
	for i := 0; i < count; i++ {
		if len(deckComp.Cards) == 0 {
			s.dealFatigue(world, playerEntity)
			continue
		}

		if len(handComp.Cards) >= handComp.MaxCards {
			cardID := deckComp.Cards[0]
			deckComp.Cards = deckComp.Cards[1:]
			world.EmitEvent("card_burned", map[string]interface{}{
				"player_id": s.getPlayerID(playerEntity),
				"card_id":   cardID,
			}, playerEntity)
			continue
		}

		cardID := deckComp.Cards[0]
		deckComp.Cards = deckComp.Cards[1:]
		handComp.Cards = append(handComp.Cards, cardID)
		drawnCards = append(drawnCards, cardID)

		world.EmitEvent("card_drawn", map[string]interface{}{
			"player_id": s.getPlayerID(playerEntity),
			"card_id":   cardID,
		}, playerEntity)
	}

	return drawnCards
}

func (s *DrawSystem) dealFatigue(world *ecs.World, playerEntity *ecs.Entity) {
	heroComp, ok := playerEntity.GetComponent("hero").(*components.HeroComponent)
	if !ok {
		return
	}

	heroComp.Health--
	if heroComp.Health < 0 {
		heroComp.Health = 0
	}

	world.EmitEvent("fatigue_damage", map[string]interface{}{
		"player_id": s.getPlayerID(playerEntity),
		"damage":    1,
	}, playerEntity)
}

func (s *DrawSystem) CreateDeck(world *ecs.World, playerEntity *ecs.Entity, templates []*CardTemplate, cardSystem *CardSystem) {
	playerID := s.getPlayerID(playerEntity)
	deckComp := &components.DeckComponent{
		Cards: make([]ecs.EntityID, 0, len(templates)),
	}

	for _, tpl := range templates {
		card := cardSystem.CreateCard(world, tpl, playerID)
		deckComp.Cards = append(deckComp.Cards, card.ID)
	}

	playerEntity.AddComponent(deckComp)
	playerEntity.AddComponent(&components.HandComponent{
		Cards:    make([]ecs.EntityID, 0, 10),
		MaxCards: 10,
	})
	playerEntity.AddComponent(&components.BoardComponent{
		Minions:    make([]ecs.EntityID, 0, 7),
		MaxMinions: 7,
	})
}

func (s *DrawSystem) getPlayerID(playerEntity *ecs.Entity) string {
	playerComp, ok := playerEntity.GetComponent("player").(*components.PlayerComponent)
	if ok {
		return playerComp.PlayerID
	}
	return ""
}
