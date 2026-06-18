package systems

import (
	"github.com/ecscard/game/internal/balance"
	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game/components"
	"github.com/google/uuid"
)

type CardSystem struct {
	ecs.BaseSystem
	registry *balance.BalancedCardRegistry
}

func NewCardSystem(registry *balance.BalancedCardRegistry) *CardSystem {
	return &CardSystem{
		registry: registry,
	}
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

	var cardComp *components.CardComponent
	var minionComp *components.MinionComponent
	var spellComp *components.SpellComponent
	var weaponComp *components.WeaponComponent

	if s.registry != nil {
		cardComp, minionComp, spellComp, weaponComp = s.registry.CreateCardComponents(template.ID)
	}

	if cardComp == nil {
		cardComp = &components.CardComponent{
			CardID:      uuid.New().String(),
			TemplateID:  template.ID,
			CardType:    template.Type,
			Cost:        template.Cost,
			Name:        template.Name,
			Description: template.Description,
			Rarity:      template.Rarity,
		}
	} else {
		cardComp.CardID = uuid.New().String()
	}
	entity.AddComponent(cardComp)
	entity.AddComponent(&components.OwnerComponent{PlayerID: ownerID})

	switch template.Type {
	case components.CardTypeMinion:
		if minionComp == nil {
			minionComp = &components.MinionComponent{
				Attack:     template.Attack,
				Health:     template.Health,
				MaxHealth:  template.Health,
				CanAttack:  template.Charge,
				MaxAttacks: 1,
				Taunt:      template.Taunt,
				Charge:     template.Charge,
			}
		}
		entity.AddComponent(minionComp)
		entity.AddComponent(&ecs.HealthComponent{
			CurrentHP: minionComp.Health,
			MaxHP:     minionComp.MaxHealth,
		})
		entity.AddComponent(&ecs.AttackComponent{
			AttackPower: minionComp.Attack,
		})

	case components.CardTypeSpell:
		if spellComp == nil {
			spellComp = &components.SpellComponent{
				Effect:     template.Effect,
				Value:      template.Value,
				TargetType: template.TargetType,
				AOE:        template.AOE,
			}
		}
		entity.AddComponent(spellComp)

	case components.CardTypeWeapon:
		if weaponComp == nil {
			weaponComp = &components.WeaponComponent{
				Attack:        template.Attack,
				Durability:    template.Durability,
				MaxDurability: template.Durability,
				Equipped:      false,
			}
		}
		entity.AddComponent(weaponComp)
		entity.AddComponent(&ecs.AttackComponent{
			AttackPower: weaponComp.Attack,
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
