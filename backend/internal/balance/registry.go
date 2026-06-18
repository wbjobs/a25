package balance

import (
	"sync"

	game_components "github.com/ecscard/game/internal/game/components"
	"github.com/ecscard/game/internal/game/systems"
)

type BalancedCardRegistry struct {
	mu        sync.RWMutex
	templates map[string]*systems.CardTemplate
	hotMgr    *HotUpdateManager
}

func NewBalancedCardRegistry(hotMgr *HotUpdateManager) *BalancedCardRegistry {
	return &BalancedCardRegistry{
		templates: make(map[string]*systems.CardTemplate),
		hotMgr:    hotMgr,
	}
}

func (r *BalancedCardRegistry) RegisterTemplate(tpl *systems.CardTemplate) {
	if tpl == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates[tpl.ID] = tpl
}

func (r *BalancedCardRegistry) GetTemplate(templateID string) *systems.CardTemplate {
	r.mu.RLock()
	tpl, ok := r.templates[templateID]
	r.mu.RUnlock()

	if !ok {
		return nil
	}

	copied := r.deepCopyTemplate(tpl)
	return r.ApplyOverridesToTemplate(templateID, copied)
}

func (r *BalancedCardRegistry) GetAllTemplateIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.templates))
	for id := range r.templates {
		ids = append(ids, id)
	}
	return ids
}

func (r *BalancedCardRegistry) ApplyOverridesToTemplate(templateID string, tpl *systems.CardTemplate) *systems.CardTemplate {
	if tpl == nil || r.hotMgr == nil {
		return tpl
	}

	if val, ok := r.hotMgr.GetOverrideValue(templateID, "cost"); ok {
		tpl.Cost = int(val)
	}
	if val, ok := r.hotMgr.GetOverrideValue(templateID, "attack"); ok {
		tpl.Attack = int(val)
	}
	if val, ok := r.hotMgr.GetOverrideValue(templateID, "health"); ok {
		tpl.Health = int(val)
	}
	if val, ok := r.hotMgr.GetOverrideValue(templateID, "durability"); ok {
		tpl.Durability = int(val)
	}
	if val, ok := r.hotMgr.GetOverrideValue(templateID, "spell_value"); ok {
		tpl.Value = int(val)
	}
	if val, ok := r.hotMgr.GetOverrideValue(templateID, "taunt"); ok {
		tpl.Taunt = val != 0
	}
	if val, ok := r.hotMgr.GetOverrideValue(templateID, "charge"); ok {
		tpl.Charge = val != 0
	}

	return tpl
}

func (r *BalancedCardRegistry) CreateCardComponents(templateID string) (cardComp *game_components.CardComponent, minionComp *game_components.MinionComponent, spellComp *game_components.SpellComponent, weaponComp *game_components.WeaponComponent) {
	tpl := r.GetTemplate(templateID)
	if tpl == nil {
		return nil, nil, nil, nil
	}

	cardComp = &game_components.CardComponent{
		TemplateID:  tpl.ID,
		CardType:    tpl.Type,
		Cost:        tpl.Cost,
		Name:        tpl.Name,
		Description: tpl.Description,
		Rarity:      tpl.Rarity,
	}

	switch tpl.Type {
	case game_components.CardTypeMinion:
		minionComp = &game_components.MinionComponent{
			Attack:         tpl.Attack,
			Health:         tpl.Health,
			MaxHealth:      tpl.Health,
			CanAttack:      tpl.Charge,
			AttacksThisTurn: 0,
			MaxAttacks:     1,
			Taunt:          tpl.Taunt,
			Charge:         tpl.Charge,
			DivineShield:   false,
		}
	case game_components.CardTypeSpell:
		spellComp = &game_components.SpellComponent{
			Effect:     tpl.Effect,
			Value:      tpl.Value,
			TargetType: tpl.TargetType,
			AOE:        tpl.AOE,
		}
	case game_components.CardTypeWeapon:
		weaponComp = &game_components.WeaponComponent{
			Attack:        tpl.Attack,
			Durability:    tpl.Durability,
			MaxDurability: tpl.Durability,
			Equipped:      false,
		}
	}

	if r.hotMgr != nil {
		r.hotMgr.OverrideCardComponent(templateID, cardComp)
		if minionComp != nil {
			r.hotMgr.OverrideMinionComponent(templateID, minionComp)
		}
		if spellComp != nil {
			r.hotMgr.OverrideSpellComponent(templateID, spellComp)
		}
		if weaponComp != nil {
			r.hotMgr.OverrideWeaponComponent(templateID, weaponComp)
		}
	}

	return cardComp, minionComp, spellComp, weaponComp
}

func (r *BalancedCardRegistry) InitFromDefaultTemplates(defaultTemplates []*systems.CardTemplate) {
	for _, tpl := range defaultTemplates {
		r.RegisterTemplate(tpl)
	}
}

func (r *BalancedCardRegistry) AddUpdateListener(fn func(templateID string)) {
	if r.hotMgr != nil {
		r.hotMgr.AddUpdateListener(fn)
	}
}

func (r *BalancedCardRegistry) deepCopyTemplate(tpl *systems.CardTemplate) *systems.CardTemplate {
	if tpl == nil {
		return nil
	}
	return &systems.CardTemplate{
		ID:                tpl.ID,
		Name:              tpl.Name,
		Type:              tpl.Type,
		Cost:              tpl.Cost,
		Attack:            tpl.Attack,
		Health:            tpl.Health,
		Durability:        tpl.Durability,
		Effect:            tpl.Effect,
		Value:             tpl.Value,
		TargetType:        tpl.TargetType,
		AOE:               tpl.AOE,
		Description:       tpl.Description,
		Rarity:            tpl.Rarity,
		Taunt:             tpl.Taunt,
		Charge:            tpl.Charge,
		DeathrattleEffect: tpl.DeathrattleEffect,
		DeathrattleValue:  tpl.DeathrattleValue,
		DeathrattleTarget: tpl.DeathrattleTarget,
	}
}
