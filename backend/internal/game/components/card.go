package components

import (
	"github.com/ecscard/game/internal/ecs"
)

type CardType string

const (
	CardTypeMinion  CardType = "minion"
	CardTypeSpell   CardType = "spell"
	CardTypeWeapon  CardType = "weapon"
)

type SpellEffect string

const (
	SpellEffectDamage SpellEffect = "damage"
	SpellEffectDraw   SpellEffect = "draw"
	SpellEffectHeal   SpellEffect = "heal"
	SpellEffectBuff   SpellEffect = "buff"
)

type CardComponent struct {
	ecs.BaseComponent
	CardID          string
	TemplateID      string
	Type            CardType
	Cost            int
	Name            string
	Description     string
	Rarity          string
}

func (CardComponent) Type() string { return "card" }

type MinionComponent struct {
	ecs.BaseComponent
	Attack          int
	Health          int
	MaxHealth       int
	CanAttack       bool
	AttacksThisTurn int
	MaxAttacks      int
	Taunt           bool
	Charge          bool
	DivineShield    bool
}

func (MinionComponent) Type() string { return "minion" }

type SpellComponent struct {
	ecs.BaseComponent
	Effect      SpellEffect
	Value       int
	TargetType  TargetType
	AOE         bool
}

func (SpellComponent) Type() string { return "spell" }

type WeaponComponent struct {
	ecs.BaseComponent
	Attack      int
	Durability  int
	MaxDurability int
	Equipped    bool
}

func (WeaponComponent) Type() string { return "weapon" }

type TargetType string

const (
	TargetNone     TargetType = "none"
	TargetAny      TargetType = "any"
	TargetEnemy    TargetType = "enemy"
	TargetFriendly TargetType = "friendly"
	TargetHero     TargetType = "hero"
	TargetMinion   TargetType = "minion"
)

type TargetComponent struct {
	ecs.BaseComponent
	TargetType TargetType
	TargetID   ecs.EntityID
}

func (TargetComponent) Type() string { return "target" }

type DeckComponent struct {
	ecs.BaseComponent
	Cards []ecs.EntityID
}

func (DeckComponent) Type() string { return "deck" }

type HandComponent struct {
	ecs.BaseComponent
	Cards     []ecs.EntityID
	MaxCards  int
}

func (HandComponent) Type() string { return "hand" }

type BoardComponent struct {
	ecs.BaseComponent
	Minions    []ecs.EntityID
	MaxMinions int
}

func (BoardComponent) Type() string { return "board" }

type HeroComponent struct {
	ecs.BaseComponent
	PlayerID    string
	Health      int
	MaxHealth   int
	Attack      int
	Weapon      ecs.EntityID
	Armor       int
}

func (HeroComponent) Type() string { return "hero" }
