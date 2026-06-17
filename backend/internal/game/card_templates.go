package game

import (
	"github.com/ecscard/game/internal/game/components"
	"github.com/ecscard/game/internal/game/systems"
)

func init() {
	for _, tpl := range CardTemplates {
		systems.RegisterCardTemplate(tpl)
	}
}

var CardTemplates = []*systems.CardTemplate{
	{
		ID:          "minion_001",
		Name:        "新兵",
		Type:        components.CardTypeMinion,
		Cost:        1,
		Attack:      1,
		Health:      2,
		Description: "一个普通的士兵",
		Rarity:      "common",
	},
	{
		ID:          "minion_002",
		Name:        "战士",
		Type:        components.CardTypeMinion,
		Cost:        2,
		Attack:      3,
		Health:      2,
		Description: "勇猛的战士",
		Rarity:      "common",
	},
	{
		ID:          "minion_003",
		Name:        "盾卫",
		Type:        components.CardTypeMinion,
		Cost:        3,
		Attack:      2,
		Health:      4,
		Taunt:       true,
		Description: "嘲讽：敌人必须先攻击此随从",
		Rarity:      "common",
	},
	{
		ID:          "minion_004",
		Name:        "骑士",
		Type:        components.CardTypeMinion,
		Cost:        4,
		Attack:      4,
		Health:      4,
		Description: "正义的骑士",
		Rarity:      "rare",
	},
	{
		ID:          "minion_005",
		Name:        "冲锋兵",
		Type:        components.CardTypeMinion,
		Cost:        3,
		Attack:      4,
		Health:      2,
		Charge:      true,
		Description: "冲锋：可以立即攻击",
		Rarity:      "rare",
	},
	{
		ID:          "minion_006",
		Name:        "死亡骑士",
		Type:        components.CardTypeMinion,
		Cost:        5,
		Attack:      5,
		Health:      5,
		Description: "强大的死亡骑士",
		DeathrattleEffect: components.SpellEffectDamage,
		DeathrattleValue:  3,
		DeathrattleTarget: components.TargetEnemy,
		Rarity:      "epic",
	},
	{
		ID:          "minion_007",
		Name:        "巨龙",
		Type:        components.CardTypeMinion,
		Cost:        8,
		Attack:      8,
		Health:      8,
		Description: "强大的巨龙",
		Rarity:      "legendary",
	},
	{
		ID:          "spell_001",
		Name:        "火球术",
		Type:        components.CardTypeSpell,
		Cost:        4,
		Effect:      components.SpellEffectDamage,
		Value:       6,
		TargetType:  components.TargetAny,
		Description: "造成6点伤害",
		Rarity:      "common",
	},
	{
		ID:          "spell_002",
		Name:        "奥术飞弹",
		Type:        components.CardTypeSpell,
		Cost:        1,
		Effect:      components.SpellEffectDamage,
		Value:       3,
		TargetType:  components.TargetEnemy,
		Description: "随机对敌方造成3点伤害",
		Rarity:      "common",
	},
	{
		ID:          "spell_003",
		Name:        "治疗术",
		Type:        components.CardTypeSpell,
		Cost:        2,
		Effect:      components.SpellEffectHeal,
		Value:       5,
		TargetType:  components.TargetFriendly,
		Description: "恢复5点生命值",
		Rarity:      "common",
	},
	{
		ID:          "spell_004",
		Name:        "智慧祝福",
		Type:        components.CardTypeSpell,
		Cost:        1,
		Effect:      components.SpellEffectDraw,
		Value:       2,
		TargetType:  components.TargetNone,
		Description: "抽2张牌",
		Rarity:      "common",
	},
	{
		ID:          "spell_005",
		Name:        "烈焰风暴",
		Type:        components.CardTypeSpell,
		Cost:        7,
		Effect:      components.SpellEffectDamage,
		Value:       4,
		TargetType:  components.TargetEnemy,
		AOE:         true,
		Description: "对所有敌方随从造成4点伤害",
		Rarity:      "epic",
	},
	{
		ID:          "weapon_001",
		Name:        "短剑",
		Type:        components.CardTypeWeapon,
		Cost:        1,
		Attack:      1,
		Durability:  2,
		Description: "一把普通的短剑",
		Rarity:      "common",
	},
	{
		ID:          "weapon_002",
		Name:        "战斧",
		Type:        components.CardTypeWeapon,
		Cost:        3,
		Attack:      3,
		Durability:  2,
		Description: "锋利的战斧",
		Rarity:      "common",
	},
	{
		ID:          "weapon_003",
		Name:        "巨剑",
		Type:        components.CardTypeWeapon,
		Cost:        5,
		Attack:      4,
		Durability:  3,
		Description: "沉重的巨剑",
		Rarity:      "rare",
	},
}

func GetCardTemplate(id string) *systems.CardTemplate {
	for _, t := range CardTemplates {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func GenerateRandomDeck() []*systems.CardTemplate {
	deck := make([]*systems.CardTemplate, 30)
	templateCount := len(CardTemplates)
	for i := 0; i < 30; i++ {
		idx := i % templateCount
		deck[i] = CardTemplates[idx]
	}
	return deck
}
