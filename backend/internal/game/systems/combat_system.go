package systems

import (
	"math/rand"

	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game/components"
)

type CombatSystem struct {
	ecs.BaseSystem
}

func NewCombatSystem() *CombatSystem {
	return &CombatSystem{}
}

func (s *CombatSystem) Name() string { return "combat_system" }

func (s *CombatSystem) Update(world *ecs.World, dt float64) {
	s.processDeaths(world)
	s.cleanupDyingEntities(world)
}

func (s *CombatSystem) PlayCard(world *ecs.World, playerEntity *ecs.Entity, cardID ecs.EntityID, targetID ecs.EntityID) bool {
	handComp, ok := playerEntity.GetComponent("hand").(*components.HandComponent)
	if !ok {
		return false
	}

	manaComp, ok := playerEntity.GetComponent("mana").(*ecs.ManaComponent)
	if !ok {
		return false
	}

	cardEntity, ok := world.GetEntity(cardID)
	if !ok {
		return false
	}

	cardComp, ok := cardEntity.GetComponent("card").(*components.CardComponent)
	if !ok {
		return false
	}

	if !manaComp.SpendMana(cardComp.Cost) {
		return false
	}

	handComp.Cards = s.removeCardFromHand(handComp.Cards, cardID)

	switch cardComp.CardType {
	case components.CardTypeMinion:
		return s.playMinion(world, playerEntity, cardEntity)
	case components.CardTypeSpell:
		return s.playSpell(world, playerEntity, cardEntity, targetID)
	case components.CardTypeWeapon:
		return s.playWeapon(world, playerEntity, cardEntity)
	}

	return false
}

func (s *CombatSystem) playMinion(world *ecs.World, playerEntity *ecs.Entity, cardEntity *ecs.Entity) bool {
	boardComp, ok := playerEntity.GetComponent("board").(*components.BoardComponent)
	if !ok {
		return false
	}

	if len(boardComp.Minions) >= boardComp.MaxMinions {
		return false
	}

	boardComp.Minions = append(boardComp.Minions, cardEntity.ID)

	minionComp, _ := cardEntity.GetComponent("minion").(*components.MinionComponent)
	if minionComp != nil && !minionComp.Charge {
		minionComp.CanAttack = false
	}

	world.EmitEvent("minion_summoned", map[string]interface{}{
		"player_id":  s.getPlayerID(playerEntity),
		"card_id":    cardEntity.ID,
		"card_name":  s.getCardName(cardEntity),
		"board_pos":  len(boardComp.Minions) - 1,
	}, cardEntity)

	return true
}

func (s *CombatSystem) playSpell(world *ecs.World, playerEntity *ecs.Entity, cardEntity *ecs.Entity, targetID ecs.EntityID) bool {
	spellComp, ok := cardEntity.GetComponent("spell").(*components.SpellComponent)
	if !ok {
		return false
	}

	switch spellComp.Effect {
	case components.SpellEffectDamage:
		s.applySpellDamage(world, playerEntity, spellComp, targetID)
	case components.SpellEffectHeal:
		s.applySpellHeal(world, playerEntity, spellComp, targetID)
	case components.SpellEffectDraw:
		s.applySpellDraw(world, playerEntity, spellComp)
	case components.SpellEffectBuff:
		s.applySpellBuff(world, playerEntity, spellComp, targetID)
	}

	cardEntity.AddComponent(&components.DyingComponent{Destroyed: true})

	world.EmitEvent("spell_cast", map[string]interface{}{
		"player_id":  s.getPlayerID(playerEntity),
		"card_id":    cardEntity.ID,
		"card_name":  s.getCardName(cardEntity),
		"target_id":  targetID,
	}, cardEntity)

	return true
}

func (s *CombatSystem) playWeapon(world *ecs.World, playerEntity *ecs.Entity, cardEntity *ecs.Entity) bool {
	heroComp, ok := playerEntity.GetComponent("hero").(*components.HeroComponent)
	if !ok {
		return false
	}

	weaponComp, ok := cardEntity.GetComponent("weapon").(*components.WeaponComponent)
	if !ok {
		return false
	}

	if heroComp.Weapon != 0 {
		if oldWeapon, ok := world.GetEntity(heroComp.Weapon); ok {
			oldWeapon.AddComponent(&components.DyingComponent{Destroyed: true})
		}
	}

	weaponComp.Equipped = true
	heroComp.Weapon = cardEntity.ID
	heroComp.Attack = weaponComp.Attack

	world.EmitEvent("weapon_equipped", map[string]interface{}{
		"player_id":   s.getPlayerID(playerEntity),
		"card_id":     cardEntity.ID,
		"card_name":   s.getCardName(cardEntity),
		"attack":      weaponComp.Attack,
		"durability":  weaponComp.Durability,
	}, cardEntity)

	return true
}

func (s *CombatSystem) Attack(world *ecs.World, attackerID ecs.EntityID, targetID ecs.EntityID) bool {
	attacker, ok := world.GetEntity(attackerID)
	if !ok {
		return false
	}

	target, ok := world.GetEntity(targetID)
	if !ok {
		return false
	}

	if !s.canAttack(world, attacker, target) {
		return false
	}

	minionComp, hasMinion := attacker.GetComponent("minion").(*components.MinionComponent)
	heroComp, hasHero := attacker.GetComponent("hero").(*components.HeroComponent)
	attackerOwner := s.getOwnerPlayer(world, attacker)
	targetOwner := s.getOwnerPlayer(world, target)

	if attackerOwner == nil || targetOwner == nil {
		return false
	}

	if s.getPlayerID(attackerOwner) == s.getPlayerID(targetOwner) {
		return false
	}

	if s.hasTaunt(targetOwner, target) && !s.isTargetTaunt(target) {
		return false
	}

	var attackPower int
	if hasMinion {
		attackPower = minionComp.Attack
		minionComp.CanAttack = false
		minionComp.AttacksThisTurn++
	} else if hasHero {
		attackPower = heroComp.Attack
		s.consumeWeaponDurability(world, attacker)
	}

	s.dealDamage(world, attacker, target, attackPower)

	if targetMinion, ok := target.GetComponent("minion").(*components.MinionComponent); ok {
		s.dealDamage(world, target, attacker, targetMinion.Attack)
	}

	world.EmitEvent("attack", map[string]interface{}{
		"attacker_id": attackerID,
		"target_id":   targetID,
		"damage":      attackPower,
	}, attacker)

	return true
}

func (s *CombatSystem) canAttack(world *ecs.World, attacker *ecs.Entity, target *ecs.Entity) bool {
	if minionComp, ok := attacker.GetComponent("minion").(*components.MinionComponent); ok {
		if !minionComp.CanAttack {
			return false
		}
		if minionComp.AttacksThisTurn >= minionComp.MaxAttacks {
			return false
		}
	}

	if _, ok := attacker.GetComponent("frozen"); ok {
		return false
	}

	return true
}

func (s *CombatSystem) dealDamage(world *ecs.World, source *ecs.Entity, target *ecs.Entity, damage int) {
	if healthComp, ok := target.GetComponent("health").(*ecs.HealthComponent); ok {
		healthComp.TakeDamage(damage)

		if minionComp, ok := target.GetComponent("minion").(*components.MinionComponent); ok {
			minionComp.Health = healthComp.CurrentHP
		}

		if healthComp.IsDead() {
			target.AddComponent(&components.DyingComponent{Destroyed: true})
			s.triggerDeathrattle(world, target)
		}
	} else if heroComp, ok := target.GetComponent("hero").(*components.HeroComponent); ok {
		if heroComp.Armor >= damage {
			heroComp.Armor -= damage
		} else {
			remainingDamage := damage - heroComp.Armor
			heroComp.Armor = 0
			heroComp.Health -= remainingDamage
			if heroComp.Health < 0 {
				heroComp.Health = 0
			}
		}
	}

	world.EmitEvent("damage_dealt", map[string]interface{}{
		"source_id": source.ID,
		"target_id": target.ID,
		"damage":    damage,
	}, target)
}

func (s *CombatSystem) applySpellDamage(world *ecs.World, caster *ecs.Entity, spellComp *components.SpellComponent, targetID ecs.EntityID) {
	casterPlayer := s.getOwnerPlayer(world, caster)
	opponent := s.getOpponent(world, casterPlayer)

	if spellComp.AOE {
		if opponent != nil {
			if boardComp, ok := opponent.GetComponent("board").(*components.BoardComponent); ok {
				for _, minionID := range boardComp.Minions {
					if minion, ok := world.GetEntity(minionID); ok {
						s.dealDamage(world, caster, minion, spellComp.Value)
					}
				}
			}
		}
	} else if spellComp.TargetType == components.TargetEnemy && !spellComp.AOE {
		for i := 0; i < spellComp.Value; i++ {
			randomTarget := s.getRandomEnemyTarget(world, casterPlayer)
			if randomTarget != nil {
				s.dealDamage(world, caster, randomTarget, 1)
			}
		}
	} else {
		if target, ok := world.GetEntity(targetID); ok {
			s.dealDamage(world, caster, target, spellComp.Value)
		}
	}
}

func (s *CombatSystem) applySpellHeal(world *ecs.World, caster *ecs.Entity, spellComp *components.SpellComponent, targetID ecs.EntityID) {
	if target, ok := world.GetEntity(targetID); ok {
		if healthComp, ok := target.GetComponent("health").(*ecs.HealthComponent); ok {
			healthComp.CurrentHP += spellComp.Value
			if healthComp.CurrentHP > healthComp.MaxHP {
				healthComp.CurrentHP = healthComp.MaxHP
			}
			if minionComp, ok := target.GetComponent("minion").(*components.MinionComponent); ok {
				minionComp.Health = healthComp.CurrentHP
			}
		} else if heroComp, ok := target.GetComponent("hero").(*components.HeroComponent); ok {
			heroComp.Health += spellComp.Value
			if heroComp.Health > heroComp.MaxHealth {
				heroComp.Health = heroComp.MaxHealth
			}
		}
	}
}

func (s *CombatSystem) applySpellDraw(world *ecs.World, caster *ecs.Entity, spellComp *components.SpellComponent) {
	casterPlayer := s.getOwnerPlayer(world, caster)
	if casterPlayer == nil {
		return
	}

	drawSystem := NewDrawSystem()
	drawSystem.DrawCard(world, casterPlayer, spellComp.Value)
}

func (s *CombatSystem) applySpellBuff(world *ecs.World, caster *ecs.Entity, spellComp *components.SpellComponent, targetID ecs.EntityID) {
	if target, ok := world.GetEntity(targetID); ok {
		if minionComp, ok := target.GetComponent("minion").(*components.MinionComponent); ok {
			minionComp.Attack += spellComp.Value
			minionComp.Health += spellComp.Value
			minionComp.MaxHealth += spellComp.Value
		}
		if attackComp, ok := target.GetComponent("attack").(*ecs.AttackComponent); ok {
			attackComp.AttackPower += spellComp.Value
		}
		if healthComp, ok := target.GetComponent("health").(*ecs.HealthComponent); ok {
			healthComp.CurrentHP += spellComp.Value
			healthComp.MaxHP += spellComp.Value
		}
	}
}

func (s *CombatSystem) processDeaths(world *ecs.World) {
	minions := world.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"minion", "health"},
	})

	for _, minion := range minions {
		if healthComp, ok := minion.GetComponent("health").(*ecs.HealthComponent); ok {
			if healthComp.IsDead() {
				if !minion.HasComponent("dying") {
					minion.AddComponent(&components.DyingComponent{Destroyed: true})
					s.triggerDeathrattle(world, minion)
				}
			}
		}
	}
}

func (s *CombatSystem) triggerDeathrattle(world *ecs.World, entity *ecs.Entity) {
	if deathrattle, ok := entity.GetComponent("deathrattle").(*components.DeathrattleComponent); ok {
		owner := s.getOwnerPlayer(world, entity)
		opponent := s.getOpponent(world, owner)

		switch deathrattle.Effect {
		case components.SpellEffectDamage:
			if opponent != nil {
				if boardComp, ok := opponent.GetComponent("board").(*components.BoardComponent); ok {
					for _, minionID := range boardComp.Minions {
						if minion, ok := world.GetEntity(minionID); ok {
							s.dealDamage(world, entity, minion, deathrattle.Value)
						}
					}
				}
			}
		case components.SpellEffectDraw:
			if owner != nil {
				drawSystem := NewDrawSystem()
				drawSystem.DrawCard(world, owner, deathrattle.Value)
			}
		}

		world.EmitEvent("deathrattle_triggered", map[string]interface{}{
			"entity_id": entity.ID,
			"effect":    deathrattle.Effect,
			"value":     deathrattle.Value,
		}, entity)
	}
}

func (s *CombatSystem) cleanupDyingEntities(world *ecs.World) {
	dying := world.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"dying"},
	})

	for _, entity := range dying {
		if owner := s.getOwnerPlayer(world, entity); owner != nil {
			s.removeFromAllZones(world, owner, entity.ID)
		}

		world.EmitEvent("entity_destroyed", map[string]interface{}{
			"entity_id": entity.ID,
		}, entity)
	}
}

func (s *CombatSystem) removeFromAllZones(world *ecs.World, player *ecs.Entity, entityID ecs.EntityID) {
	if handComp, ok := player.GetComponent("hand").(*components.HandComponent); ok {
		handComp.Cards = s.removeCardFromHand(handComp.Cards, entityID)
	}
	if deckComp, ok := player.GetComponent("deck").(*components.DeckComponent); ok {
		deckComp.Cards = s.removeCardFromHand(deckComp.Cards, entityID)
	}
	if boardComp, ok := player.GetComponent("board").(*components.BoardComponent); ok {
		boardComp.Minions = s.removeCardFromHand(boardComp.Minions, entityID)
	}

	opponent := s.getOpponent(world, player)
	if opponent != nil {
		if handComp, ok := opponent.GetComponent("hand").(*components.HandComponent); ok {
			handComp.Cards = s.removeCardFromHand(handComp.Cards, entityID)
		}
		if deckComp, ok := opponent.GetComponent("deck").(*components.DeckComponent); ok {
			deckComp.Cards = s.removeCardFromHand(deckComp.Cards, entityID)
		}
		if boardComp, ok := opponent.GetComponent("board").(*components.BoardComponent); ok {
			boardComp.Minions = s.removeCardFromHand(boardComp.Minions, entityID)
		}
	}

	world.RemoveEntity(entityID)
}

func (s *CombatSystem) removeCardFromHand(cards []ecs.EntityID, cardID ecs.EntityID) []ecs.EntityID {
	for i, id := range cards {
		if id == cardID {
			return append(cards[:i], cards[i+1:]...)
		}
	}
	return cards
}

func (s *CombatSystem) getOwnerPlayer(world *ecs.World, entity *ecs.Entity) *ecs.Entity {
	ownerComp, ok := entity.GetComponent("owner").(*components.OwnerComponent)
	if !ok {
		return nil
	}

	players := world.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"player"},
	})

	for _, player := range players {
		if playerComp, ok := player.GetComponent("player").(*components.PlayerComponent); ok {
			if playerComp.PlayerID == ownerComp.PlayerID {
				return player
			}
		}
	}
	return nil
}

func (s *CombatSystem) getOpponent(world *ecs.World, player *ecs.Entity) *ecs.Entity {
	players := world.Query(&ecs.EntityQuery{
		RequiredComponents: []string{"player"},
	})

	playerID := s.getPlayerID(player)
	for _, p := range players {
		if comp, ok := p.GetComponent("player").(*components.PlayerComponent); ok {
			if comp.PlayerID != playerID {
				return p
			}
		}
	}
	return nil
}

func (s *CombatSystem) hasTaunt(player *ecs.Entity, target *ecs.Entity) bool {
	boardComp, ok := player.GetComponent("board").(*components.BoardComponent)
	if !ok {
		return false
	}

	for _, minionID := range boardComp.Minions {
		if minion, ok := player.GetComponent("minion").(*components.MinionComponent); ok {
			_ = minion
		}
	}
	return false
}

func (s *CombatSystem) isTargetTaunt(target *ecs.Entity) bool {
	if minionComp, ok := target.GetComponent("minion").(*components.MinionComponent); ok {
		return minionComp.Taunt
	}
	return false
}

func (s *CombatSystem) getRandomEnemyTarget(world *ecs.World, casterPlayer *ecs.Entity) *ecs.Entity {
	opponent := s.getOpponent(world, casterPlayer)
	if opponent == nil {
		return nil
	}

	targets := make([]*ecs.Entity, 0)
	targets = append(targets, opponent)

	if boardComp, ok := opponent.GetComponent("board").(*components.BoardComponent); ok {
		for _, minionID := range boardComp.Minions {
			if minion, ok := world.GetEntity(minionID); ok {
				targets = append(targets, minion)
			}
		}
	}

	if len(targets) == 0 {
		return nil
	}

	return targets[rand.Intn(len(targets))]
}

func (s *CombatSystem) consumeWeaponDurability(world *ecs.World, heroEntity *ecs.Entity) {
	heroComp, ok := heroEntity.GetComponent("hero").(*components.HeroComponent)
	if !ok || heroComp.Weapon == 0 {
		return
	}

	weaponEntity, ok := world.GetEntity(heroComp.Weapon)
	if !ok {
		return
	}

	weaponComp, ok := weaponEntity.GetComponent("weapon").(*components.WeaponComponent)
	if !ok {
		return
	}

	weaponComp.Durability--
	if weaponComp.Durability <= 0 {
		weaponEntity.AddComponent(&components.DyingComponent{Destroyed: true})
		heroComp.Weapon = 0
		heroComp.Attack = 0
	}
}

func (s *CombatSystem) getPlayerID(playerEntity *ecs.Entity) string {
	playerComp, ok := playerEntity.GetComponent("player").(*components.PlayerComponent)
	if ok {
		return playerComp.PlayerID
	}
	return ""
}

func (s *CombatSystem) getCardName(cardEntity *ecs.Entity) string {
	cardComp, ok := cardEntity.GetComponent("card").(*components.CardComponent)
	if ok {
		return cardComp.Name
	}
	return ""
}
