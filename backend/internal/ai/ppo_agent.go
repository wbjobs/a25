package ai

import (
	"math"
	"math/rand"

	pb "github.com/ecscard/game/internal/proto"
)

const (
	PPOInputDim  = 256
	PPOOutputDim = 128
)

type PPOAgent struct {
	extractor *GameFeatureExtractor
	engine    *ONNXEngine
	difficulty AIDifficulty
	epsilon    float32
	actionSeq  []ActionMask
}

func NewPPOAgent(modelPath string, difficulty AIDifficulty) (*PPOAgent, error) {
	var epsilon float32
	switch difficulty {
	case AIDifficultyEasy:
		epsilon = 0.6
	case AIDifficultyNormal:
		epsilon = 0.3
	case AIDifficultyHard:
		epsilon = 0.05
	default:
		epsilon = 0.3
	}

	agent := &PPOAgent{
		extractor:  NewGameFeatureExtractor(),
		difficulty: difficulty,
		epsilon:    epsilon,
		actionSeq:  make([]ActionMask, 0),
	}

	engine, err := NewONNXEngine(modelPath, PPOInputDim, PPOOutputDim)
	if err != nil {
		agent.engine = nil
	} else {
		agent.engine = engine
	}

	return agent, nil
}

func (a *PPOAgent) ChooseAction(playerID string, state *pb.GameStatus) *ActionMask {
	legalActions := a.extractor.GetLegalActions(playerID, state)
	if len(legalActions) == 0 {
		return nil
	}

	if a.engine == nil {
		return a.chooseActionHeuristic(playerID, state, legalActions)
	}

	features := a.extractor.ExtractFeatures(playerID, state)
	logits, err := a.engine.RunInference(features)
	if err != nil {
		return a.chooseActionHeuristic(playerID, state, legalActions)
	}

	validLogits := make([]float32, len(legalActions))
	for i := range legalActions {
		if i < len(logits) {
			validLogits[i] = logits[i]
		} else {
			validLogits[i] = -1e9
		}
	}

	probs := softmax(validLogits)

	if rand.Float32() < a.epsilon {
		idx := rand.Intn(len(legalActions))
		a.actionSeq = append(a.actionSeq, legalActions[idx])
		return &legalActions[idx]
	}

	maxIdx := 0
	maxProb := float32(-1.0)
	for i, p := range probs {
		if legalActions[i].Valid && p > maxProb {
			maxProb = p
			maxIdx = i
		}
	}

	a.actionSeq = append(a.actionSeq, legalActions[maxIdx])
	return &legalActions[maxIdx]
}

func (a *PPOAgent) chooseActionHeuristic(playerID string, state *pb.GameStatus, legalActions []ActionMask) *ActionMask {
	if len(legalActions) == 0 {
		return nil
	}

	var selfPlayer, oppPlayer *pb.Player
	for _, p := range state.Players {
		if p.PlayerId == playerID {
			selfPlayer = p
		} else {
			oppPlayer = p
		}
	}

	cardMap := make(map[uint64]*pb.Card)
	for _, c := range state.Cards {
		cardMap[c.EntityId] = c
	}

	bestIdx := 0
	bestScore := -1e9

	for i, action := range legalActions {
		if !action.Valid {
			continue
		}
		score := 0.0

		switch action.Type {
		case ActionPlayCard:
			if selfPlayer != nil && action.CardIdx >= 0 && action.CardIdx < len(selfPlayer.Hand) {
				card, ok := cardMap[selfPlayer.Hand[action.CardIdx]]
				if ok {
					score += float64(card.Cost) * 2.0
					switch card.Type {
					case pb.CardType_CARD_TYPE_MINION:
						score += float64(card.Attack) * 1.5
						score += float64(card.Health) * 1.0
						if card.Taunt {
							score += 3.0
						}
						if card.Charge {
							score += 2.0
						}
					case pb.CardType_CARD_TYPE_SPELL:
						score += float64(card.SpellValue) * 2.0
						if oppPlayer != nil && card.SpellEffect == pb.SpellEffect_SPELL_EFFECT_DAMAGE {
							if oppPlayer.Health <= int32(card.SpellValue) {
								score += 100.0
							}
						}
						if card.SpellEffect == pb.SpellEffect_SPELL_EFFECT_DRAW {
							score += 5.0
						}
						if card.SpellEffect == pb.SpellEffect_SPELL_EFFECT_HEAL && selfPlayer.Health < selfPlayer.MaxHealth/2 {
							score += float64(card.SpellValue) * 3.0
						}
					case pb.CardType_CARD_TYPE_WEAPON:
						score += float64(card.Attack) * 2.0
						score += float64(card.Durability) * 1.5
					}
				}
			}

		case ActionAttack:
			if oppPlayer != nil && action.TargetIdx == -1 {
				score += float64(oppPlayer.Health) * 0.5
				if selfPlayer != nil {
					var atk int32
					if action.CardIdx == MaxBoardMinions {
						atk = selfPlayer.Attack
					} else if action.CardIdx >= 0 && action.CardIdx < len(selfPlayer.Board) {
						if minion, ok := cardMap[selfPlayer.Board[action.CardIdx]]; ok {
							atk = minion.Attack
						}
					}
					if oppPlayer.Health <= atk {
						score += 1000.0
					} else {
						score += float64(atk) * 3.0
					}
				}
			} else if oppPlayer != nil && action.TargetIdx >= 0 && action.TargetIdx < len(oppPlayer.Board) {
				targetMinion, ok := cardMap[oppPlayer.Board[action.TargetIdx]]
				if ok {
					score += float64(targetMinion.Attack) * 2.0
					score += float64(targetMinion.Health) * 1.0
					if targetMinion.Taunt {
						score += 10.0
					}
					if selfPlayer != nil && action.CardIdx >= 0 && action.CardIdx < len(selfPlayer.Board) {
						attackerMinion, ok := cardMap[selfPlayer.Board[action.CardIdx]]
						if ok {
							if attackerMinion.Attack >= targetMinion.Health {
								score += 5.0
							}
							if attackerMinion.Health <= targetMinion.Attack {
								score -= float64(attackerMinion.Attack+attackerMinion.Health) * 0.5
							}
						}
					}
				}
			}

		case ActionEndTurn:
			score = 0.1
			if selfPlayer != nil {
				if selfPlayer.Mana == 0 {
					score += 2.0
				}
				if len(selfPlayer.Board) == 0 && len(selfPlayer.Hand) == 0 {
					score += 5.0
				}
				allAttacked := true
				for _, minionID := range selfPlayer.Board {
					if m, ok := cardMap[minionID]; ok && m.CanAttack && !m.Frozen {
						allAttacked = false
						break
					}
				}
				if allAttacked {
					score += 3.0
				}
			}
		}

		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	if a.difficulty == AIDifficultyEasy && rand.Float32() < 0.4 {
		idx := rand.Intn(len(legalActions))
		a.actionSeq = append(a.actionSeq, legalActions[idx])
		return &legalActions[idx]
	}

	if a.difficulty == AIDifficultyNormal && rand.Float32() < 0.15 {
		idx := rand.Intn(len(legalActions))
		a.actionSeq = append(a.actionSeq, legalActions[idx])
		return &legalActions[idx]
	}

	a.actionSeq = append(a.actionSeq, legalActions[bestIdx])
	return &legalActions[bestIdx]
}

func (a *PPOAgent) Close() {
	if a.engine != nil {
		a.engine.Close()
		a.engine = nil
	}
}

func softmax(logits []float32) []float32 {
	if len(logits) == 0 {
		return logits
	}

	maxLogit := float32(math.Inf(-1))
	for _, l := range logits {
		if l > maxLogit {
			maxLogit = l
		}
	}

	sumExp := float32(0.0)
	expVals := make([]float32, len(logits))
	for i, l := range logits {
		ev := float32(math.Exp(float64(l - maxLogit)))
		expVals[i] = ev
		sumExp += ev
	}

	if sumExp <= 0 {
		uniform := float32(1.0) / float32(len(logits))
		for i := range logits {
			expVals[i] = uniform
		}
		return expVals
	}

	for i := range expVals {
		expVals[i] /= sumExp
	}

	return expVals
}
