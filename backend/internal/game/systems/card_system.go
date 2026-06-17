package systems

import (
	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game/components"
	"github.com/google/uuid"
)

type CardSystem struct {
	ecs.BaseSystem
}

func NewCardSystem() *CardSystem {
	return &CardSystem{}
}

func (s *CardSystem) Name() string { return "card_system" }

func (s *CardSystem) Update(world *ecs.World, dt float64) {
	s.cleanupDeadCards(world)
}

func (s *CardSystem) cleanupDeadCards(world *ecs.World) {
	query := &ecs.EntityQuery{
		RequiredComponents: []string{"dying", "card"},
	}
	for _, entity := range world.Query(query) {
		world.RemoveEntity(entity.ID)
		world.EmitEvent("card_destroyed", map[string]interface{}{
			"card_id": entity.ID,
		}, entity)
	}
}

func (s *CardSystem) CreateCard(world *ecs.World, template *CardTemplate, ownerID string) *ecs.Entity {
	entity := ecs.NewEntity()

	cardComp := &components.CardComponent{
		CardID:      uuid.New().String(),
		TemplateID:  template.ID,
		Type:        template.Type,
		Cost:        template.Cost,
		Name:        template.Name,
		Description: template.Description,
		Rarity:      template.Rarity,
	}
	entity.AddComponent(cardComp)
	entity.AddComponent(&components.OwnerComponent{PlayerID: ownerID})

	switch template.Type {
	case components.CardTypeMinion:
		minionComp := &components.MinionComponent{
			Attack:     template.Attack,
			Health:     template.Health,
			MaxHealth:  template.Health,
			CanAttack:  template.Charge,
			MaxAttacks: 1,
			Taunt:      template.Taunt,
			Charge:     template.Charge,
		}
		entity.AddComponent(minionComp)
		entity.AddComponent(&ecs.HealthComponent{
			CurrentHP: template.Health,
			MaxHP:     template.Health,
		})
		entity.AddComponent(&ecs.AttackComponent{
			AttackPower: template.Attack,
		})

	case components.CardTypeSpell:
		spellComp := &components.SpellComponent{
			Effect:     template.Effect,
			Value:      template.Value,
			TargetType: template.TargetType,
			AOE:        template.AOE,
		}
		entity.AddComponent(spellComp)

	case components.CardTypeWeapon:
		weaponComp := &components.WeaponComponent{
			Attack:        template.Attack,
			Durability:    template.Durability,
			MaxDurability: template.Durability,
			Equipped:      false,
		}
		entity.AddComponent(weaponComp)
		entity.AddComponent(&ecs.AttackComponent{
			AttackPower: template.Attack,
		})
	}

	if template.DeathrattleEffect != "" {
		entity.AddComponent(&components.DeathrattleComponent{
			Effect:     template.DeathrattleEffect,
			Value:      template.DeathrattleValue,
			TargetType: template.DeathrattleTarget,
		})
	}

	world.AddEntity(entity)
	return entity
}

type CardTemplate struct {
	ID                 string
	Name               string
	Type               components.CardType
	Cost               int
	Attack             int
	Health             int
	Durability         int
	Effect             components.SpellEffect
	Value              int
	TargetType         components.TargetType
	AOE                bool
	Description        string
	Rarity             string
	Taunt              bool
	Charge             bool
	DeathrattleEffect  components.SpellEffect
	DeathrattleValue   int
	DeathrattleTarget  components.TargetType
}
