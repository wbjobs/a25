package ai

import (
	pb "github.com/ecscard/game/internal/proto"
)

type AIDifficulty int

const (
	AIDifficultyEasy   AIDifficulty = 1
	AIDifficultyNormal AIDifficulty = 2
	AIDifficultyHard   AIDifficulty = 3
)

const (
	FeatureDim            = 256
	MaxHandCards          = 10
	MaxBoardMinions       = 7
	ActionPlayCard        = 0
	ActionAttack          = 1
	ActionEndTurn         = 2
	NormHealth            = 30.0
	NormMaxHealth         = 30.0
	NormMana              = 10.0
	NormArmor             = 30.0
	NormAttack            = 12.0
	NormCost              = 10.0
	NormHandSize          = 10.0
	NormBoardSize         = 7.0
	NormDeckSize          = 30.0
	NormFatigue           = 30.0
	NormDurability        = 5.0
	NormSpellValue        = 10.0
	NormTurn              = 30.0
)

type ActionMask struct {
	Type      int
	CardIdx   int
	TargetIdx int
	Valid     bool
}

type GameFeatureExtractor struct{}

func NewGameFeatureExtractor() *GameFeatureExtractor {
	return &GameFeatureExtractor{}
}

func (gfe *GameFeatureExtractor) ExtractFeatures(playerID string, state *pb.GameStatus) []float32 {
	features := make([]float32, FeatureDim)
	for i := range features {
		features[i] = 0.0
	}

	if state == nil {
		return features
	}

	var selfPlayer, oppPlayer *pb.Player
	for _, p := range state.Players {
		if p.PlayerId == playerID {
			selfPlayer = p
		} else {
			oppPlayer = p
		}
	}

	if selfPlayer == nil {
		return features
	}

	cardMap := make(map[uint64]*pb.Card)
	for _, c := range state.Cards {
		cardMap[c.EntityId] = c
	}

	offset := 0

	features[offset] = clamp01(float32(state.Turn) / NormTurn)
	offset++

	if state.CurrentTurnPlayerId == playerID {
		features[offset] = 1.0
	} else {
		features[offset] = 0.0
	}
	offset++

	phaseVal := float32(0.0)
	switch state.Phase {
	case pb.TurnPhase_TURN_PHASE_BEGIN:
		phaseVal = 0.0
	case pb.TurnPhase_TURN_PHASE_MAIN:
		phaseVal = 0.5
	case pb.TurnPhase_TURN_PHASE_END:
		phaseVal = 1.0
	}
	features[offset] = phaseVal
	offset++

	stateVal := float32(0.0)
	switch state.State {
	case pb.GameState_GAME_STATE_WAITING:
		stateVal = 0.0
	case pb.GameState_GAME_STATE_PLAYING:
		stateVal = 0.5
	case pb.GameState_GAME_STATE_FINISHED:
		stateVal = 1.0
	}
	features[offset] = stateVal
	offset++

	offset = encodePlayer(features, offset, selfPlayer, cardMap, true)

	if oppPlayer != nil {
		offset = encodePlayer(features, offset, oppPlayer, cardMap, false)
	} else {
		offset += 32
	}

	offset = encodeHand(features, offset, selfPlayer, cardMap)

	offset = encodeBoard(features, offset, selfPlayer, cardMap)

	if oppPlayer != nil {
		offset = encodeBoard(features, offset, oppPlayer, cardMap)
	} else {
		offset += MaxBoardMinions * 6
	}

	if oppPlayer != nil {
		features[offset] = clamp01(float32(len(oppPlayer.Hand)) / NormHandSize)
		offset++
		features[offset] = clamp01(float32(oppPlayer.DeckSize) / NormDeckSize)
		offset++
	} else {
		offset += 2
	}

	return features
}

func encodePlayer(features []float32, offset int, p *pb.Player, cardMap map[uint64]*pb.Card, isSelf bool) int {
	features[offset] = clamp01(float32(p.Health) / NormHealth)
	offset++
	features[offset] = clamp01(float32(p.MaxHealth) / NormMaxHealth)
	offset++
	features[offset] = clamp01(float32(p.Mana) / NormMana)
	offset++
	features[offset] = clamp01(float32(p.MaxMana) / NormMana)
	offset++
	features[offset] = clamp01(float32(p.Armor) / NormArmor)
	offset++
	features[offset] = clamp01(float32(p.Attack) / NormAttack)
	offset++
	features[offset] = clamp01(float32(len(p.Hand)) / NormHandSize)
	offset++
	features[offset] = clamp01(float32(len(p.Board)) / NormBoardSize)
	offset++
	features[offset] = clamp01(float32(p.DeckSize) / NormDeckSize)
	offset++
	features[offset] = clamp01(float32(p.FatigueCounter) / NormFatigue)
	offset++

	if p.Weapon != 0 {
		if weaponCard, ok := cardMap[p.Weapon]; ok {
			features[offset] = 1.0
			offset++
			features[offset] = clamp01(float32(weaponCard.Attack) / NormAttack)
			offset++
			features[offset] = clamp01(float32(weaponCard.Durability) / NormDurability)
			offset++
		} else {
			features[offset] = 0.0
			offset++
			features[offset] = 0.0
			offset++
			features[offset] = 0.0
			offset++
		}
	} else {
		features[offset] = 0.0
		offset++
		features[offset] = 0.0
		offset++
		features[offset] = 0.0
		offset++
	}

	if isSelf {
		features[offset] = 1.0
	} else {
		features[offset] = 0.0
	}
	offset++

	for i := offset; i < offset+17; i++ {
		if i < len(features) {
			features[i] = 0.0
		}
	}
	offset += 17

	return offset
}

func encodeHand(features []float32, offset int, p *pb.Player, cardMap map[uint64]*pb.Card) int {
	for i := 0; i < MaxHandCards; i++ {
		base := offset + i*8
		for j := 0; j < 8; j++ {
			if base+j < len(features) {
				features[base+j] = 0.0
			}
		}

		if i >= len(p.Hand) {
			continue
		}

		cardID := p.Hand[i]
		card, ok := cardMap[cardID]
		if !ok {
			continue
		}

		features[base] = 1.0
		features[base+1] = clamp01(float32(card.Cost) / NormCost)

		typeVal := float32(0.0)
		switch card.Type {
		case pb.CardType_CARD_TYPE_MINION:
			typeVal = 0.0
		case pb.CardType_CARD_TYPE_SPELL:
			typeVal = 0.5
		case pb.CardType_CARD_TYPE_WEAPON:
			typeVal = 1.0
		}
		features[base+2] = typeVal

		atkVal := float32(0.0)
		hpVal := float32(0.0)
		spellVal := float32(0.0)

		switch card.Type {
		case pb.CardType_CARD_TYPE_MINION:
			atkVal = clamp01(float32(card.Attack) / NormAttack)
			hpVal = clamp01(float32(card.Health) / NormHealth)
		case pb.CardType_CARD_TYPE_SPELL:
			spellVal = clamp01(float32(card.SpellValue) / NormSpellValue)
		case pb.CardType_CARD_TYPE_WEAPON:
			atkVal = clamp01(float32(card.Attack) / NormAttack)
			hpVal = clamp01(float32(card.Durability) / NormDurability)
		}

		features[base+3] = atkVal
		features[base+4] = hpVal
		features[base+5] = spellVal

		if card.CanAttack {
			features[base+6] = 1.0
		}
		if card.Taunt {
			features[base+7] = 1.0
		}
	}

	return offset + MaxHandCards*8
}

func encodeBoard(features []float32, offset int, p *pb.Player, cardMap map[uint64]*pb.Card) int {
	for i := 0; i < MaxBoardMinions; i++ {
		base := offset + i*6
		for j := 0; j < 6; j++ {
			if base+j < len(features) {
				features[base+j] = 0.0
			}
		}

		if i >= len(p.Board) {
			continue
		}

		minionID := p.Board[i]
		minion, ok := cardMap[minionID]
		if !ok {
			continue
		}

		features[base] = 1.0
		features[base+1] = clamp01(float32(minion.Cost) / NormCost)
		features[base+2] = clamp01(float32(minion.Attack) / NormAttack)
		features[base+3] = clamp01(float32(minion.Health) / NormHealth)

		if minion.CanAttack && !minion.Frozen {
			features[base+4] = 1.0
		}
		if minion.Taunt {
			features[base+5] = 1.0
		}
	}

	return offset + MaxBoardMinions*6
}

func (gfe *GameFeatureExtractor) GetLegalActions(playerID string, state *pb.GameStatus) []ActionMask {
	actions := make([]ActionMask, 0)

	if state == nil {
		return actions
	}

	if state.CurrentTurnPlayerId != playerID {
		actions = append(actions, ActionMask{Type: ActionEndTurn, CardIdx: -1, TargetIdx: -1, Valid: true})
		return actions
	}

	var selfPlayer, oppPlayer *pb.Player
	for _, p := range state.Players {
		if p.PlayerId == playerID {
			selfPlayer = p
		} else {
			oppPlayer = p
		}
	}

	if selfPlayer == nil {
		return actions
	}

	cardMap := make(map[uint64]*pb.Card)
	for _, c := range state.Cards {
		cardMap[c.EntityId] = c
	}

	actions = append(actions, ActionMask{Type: ActionEndTurn, CardIdx: -1, TargetIdx: -1, Valid: true})

	for i, cardID := range selfPlayer.Hand {
		if i >= MaxHandCards {
			break
		}
		card, ok := cardMap[cardID]
		if !ok {
			continue
		}
		if selfPlayer.Mana >= card.Cost {
			if card.Type == pb.CardType_CARD_TYPE_MINION {
				if len(selfPlayer.Board) < MaxBoardMinions {
					actions = append(actions, ActionMask{Type: ActionPlayCard, CardIdx: i, TargetIdx: -1, Valid: true})
				}
			} else if card.Type == pb.CardType_CARD_TYPE_WEAPON {
				actions = append(actions, ActionMask{Type: ActionPlayCard, CardIdx: i, TargetIdx: -1, Valid: true})
			} else if card.Type == pb.CardType_CARD_TYPE_SPELL {
				if card.SpellEffect == pb.SpellEffect_SPELL_EFFECT_DRAW {
					actions = append(actions, ActionMask{Type: ActionPlayCard, CardIdx: i, TargetIdx: -1, Valid: true})
				} else {
					targetCount := 0
					if oppPlayer != nil {
						for range oppPlayer.Board {
							actions = append(actions, ActionMask{Type: ActionPlayCard, CardIdx: i, TargetIdx: targetCount, Valid: true})
							targetCount++
						}
					}
					for range selfPlayer.Board {
						actions = append(actions, ActionMask{Type: ActionPlayCard, CardIdx: i, TargetIdx: targetCount, Valid: true})
						targetCount++
					}
					if targetCount == 0 {
						actions = append(actions, ActionMask{Type: ActionPlayCard, CardIdx: i, TargetIdx: -1, Valid: true})
					}
				}
			} else {
				actions = append(actions, ActionMask{Type: ActionPlayCard, CardIdx: i, TargetIdx: -1, Valid: true})
			}
		}
	}

	oppHasTaunt := false
	if oppPlayer != nil {
		for _, minionID := range oppPlayer.Board {
			if m, ok := cardMap[minionID]; ok && m.Taunt {
				oppHasTaunt = true
				break
			}
		}
	}

	for i, minionID := range selfPlayer.Board {
		if i >= MaxBoardMinions {
			break
		}
		minion, ok := cardMap[minionID]
		if !ok {
			continue
		}
		if !minion.CanAttack || minion.Frozen {
			continue
		}

		if oppPlayer != nil && !oppHasTaunt {
			actions = append(actions, ActionMask{Type: ActionAttack, CardIdx: i, TargetIdx: -1, Valid: true})
		}

		if oppPlayer != nil {
			for j, oppMinionID := range oppPlayer.Board {
				oppMinion, ok := cardMap[oppMinionID]
				if !ok {
					continue
				}
				if oppHasTaunt && !oppMinion.Taunt {
					continue
				}
				actions = append(actions, ActionMask{Type: ActionAttack, CardIdx: i, TargetIdx: j, Valid: true})
			}
		}
	}

	if selfPlayer.Weapon != 0 && selfPlayer.Attack > 0 {
		if weaponCard, ok := cardMap[selfPlayer.Weapon]; ok && weaponCard.Durability > 0 {
			if oppPlayer != nil && !oppHasTaunt {
				actions = append(actions, ActionMask{Type: ActionAttack, CardIdx: MaxBoardMinions, TargetIdx: -1, Valid: true})
			}
			if oppPlayer != nil {
				for j, oppMinionID := range oppPlayer.Board {
					oppMinion, ok := cardMap[oppMinionID]
					if !ok {
						continue
					}
					if oppHasTaunt && !oppMinion.Taunt {
						continue
					}
					actions = append(actions, ActionMask{Type: ActionAttack, CardIdx: MaxBoardMinions, TargetIdx: j, Valid: true})
				}
			}
		}
	}

	return actions
}

func clamp01(v float32) float32 {
	if v < 0.0 {
		return 0.0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}
