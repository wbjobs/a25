package ecs

type Component interface {
	Type() string
}

type BaseComponent struct{}

func (BaseComponent) Type() string { return "base" }

type PositionComponent struct {
	BaseComponent
	X, Y int
}

func (PositionComponent) Type() string { return "position" }

type HealthComponent struct {
	BaseComponent
	CurrentHP int
	MaxHP     int
}

func (HealthComponent) Type() string { return "health" }

func (h *HealthComponent) TakeDamage(amount int) {
	h.CurrentHP -= amount
	if h.CurrentHP < 0 {
		h.CurrentHP = 0
	}
}

func (h *HealthComponent) IsDead() bool {
	return h.CurrentHP <= 0
}

type AttackComponent struct {
	BaseComponent
	AttackPower int
}

func (AttackComponent) Type() string { return "attack" }

type OwnerComponent struct {
	BaseComponent
	PlayerID string
}

func (OwnerComponent) Type() string { return "owner" }

type NameComponent struct {
	BaseComponent
	Name string
}

func (NameComponent) Type() string { return "name" }

type ManaComponent struct {
	BaseComponent
	CurrentMana int
	MaxMana     int
}

func (ManaComponent) Type() string { return "mana" }

func (m *ManaComponent) AddMana(amount int) {
	m.CurrentMana += amount
	if m.CurrentMana > m.MaxMana {
		m.CurrentMana = m.MaxMana
	}
}

func (m *ManaComponent) SpendMana(amount int) bool {
	if m.CurrentMana >= amount {
		m.CurrentMana -= amount
		return true
	}
	return false
}
