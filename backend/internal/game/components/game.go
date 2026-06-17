package components

import (
	"github.com/ecscard/game/internal/ecs"
)

type GameState string

const (
	GameStateWaiting  GameState = "waiting"
	GameStatePlaying  GameState = "playing"
	GameStateFinished GameState = "finished"
)

type TurnPhase string

const (
	TurnPhaseBegin     TurnPhase = "begin"
	TurnPhaseMain      TurnPhase = "main"
	TurnPhaseEnd       TurnPhase = "end"
)

type GameStateComponent struct {
	ecs.BaseComponent
	State       GameState
	Turn        int
	CurrentTurn string
	Phase       TurnPhase
	FrameNumber uint64
	Winner      string
}

func (GameStateComponent) Type() string { return "game_state" }

type PlayerComponent struct {
	ecs.BaseComponent
	PlayerID    string
	PlayerName  string
	IsAI        bool
	Connected   bool
}

func (PlayerComponent) Type() string { return "player" }

type AuraComponent struct {
	ecs.BaseComponent
	AuraType    string
	Value       int
	TargetType  TargetType
}

func (AuraComponent) Type() string { return "aura" }

type DeathrattleComponent struct {
	ecs.BaseComponent
	Effect      SpellEffect
	Value       int
	TargetType  TargetType
}

func (DeathrattleComponent) Type() string { return "deathrattle" }

type FrozenComponent struct {
	ecs.BaseComponent
	Duration int
}

func (FrozenComponent) Type() string { return "frozen" }

type StealthComponent struct {
	ecs.BaseComponent
	Active bool
}

func (StealthComponent) Type() string { return "stealth" }

type WindfuryComponent struct {
	ecs.BaseComponent
	Active bool
}

func (WindfuryComponent) Type() string { return "windfury" }

type LifestealComponent struct {
	ecs.BaseComponent
	Active bool
}

func (LifestealComponent) Type() string { return "lifesteal" }

type PoisonousComponent struct {
	ecs.BaseComponent
	Active bool
}

func (PoisonousComponent) Type() string { return "poisonous" }

type DyingComponent struct {
	ecs.BaseComponent
	Destroyed bool
}

func (DyingComponent) Type() string { return "dying" }
