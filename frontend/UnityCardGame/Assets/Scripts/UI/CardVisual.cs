using TMPro;
using UnityEngine;
using UnityEngine.UI;
using CardGame.ECS;
using Unity.Entities;

namespace CardGame.UI
{
    public class CardVisual : MonoBehaviour
    {
        [Header("References")]
        [SerializeField] private Image _cardBackground;
        [SerializeField] private Image _cardArt;
        [SerializeField] private TextMeshProUGUI _cardNameText;
        [SerializeField] private TextMeshProUGUI _cardCostText;
        [SerializeField] private TextMeshProUGUI _cardAttackText;
        [SerializeField] private TextMeshProUGUI _cardHealthText;
        [SerializeField] private TextMeshProUGUI _cardDescriptionText;
        [SerializeField] private GameObject _minionStatsPanel;
        [SerializeField] private GameObject _spellIcon;
        [SerializeField] private GameObject _weaponIcon;
        [SerializeField] private GameObject _tauntIndicator;
        [SerializeField] private GameObject _chargeIndicator;
        [SerializeField] private GameObject _divineShieldIndicator;
        [SerializeField] private GameObject _frozenIndicator;
        [SerializeField] private GameObject _canAttackIndicator;
        [SerializeField] private Image _selectedHighlight;

        [Header("Colors")]
        [SerializeField] private Color _minionColor;
        [SerializeField] private Color _spellColor;
        [SerializeField] private Color _weaponColor;
        [SerializeField] private Color _disabledColor;
        [SerializeField] private Color _selectedColor;

        private Entity _entity;
        private bool _isSelected;
        private bool _isHovered;
        private bool _isOwner;

        public Entity Entity => _entity;
        public bool IsOwner => _isOwner;

        public void Initialize(Entity entity, CardComponent card, bool isOwner)
        {
            _entity = entity;
            _isOwner = isOwner;

            UpdateVisual(card);
            _selectedHighlight.enabled = false;
        }

        public void UpdateVisual(CardComponent card)
        {
            _cardNameText.text = GetCardName(card);
            _cardCostText.text = card.Cost.ToString();
            _cardDescriptionText.text = GetCardDescription(card);

            switch (card.Type)
            {
                case CardType.Minion:
                    _cardBackground.color = _minionColor;
                    _minionStatsPanel.SetActive(true);
                    _spellIcon.SetActive(false);
                    _weaponIcon.SetActive(false);
                    _cardAttackText.text = card.Attack.ToString();
                    _cardHealthText.text = card.Health.ToString();
                    break;
                case CardType.Spell:
                    _cardBackground.color = _spellColor;
                    _minionStatsPanel.SetActive(false);
                    _spellIcon.SetActive(true);
                    _weaponIcon.SetActive(false);
                    break;
                case CardType.Weapon:
                    _cardBackground.color = _weaponColor;
                    _minionStatsPanel.SetActive(true);
                    _spellIcon.SetActive(false);
                    _weaponIcon.SetActive(true);
                    _cardAttackText.text = card.Attack.ToString();
                    _cardHealthText.text = card.Durability.ToString();
                    break;
            }

            _tauntIndicator.SetActive(card.HasTaunt);
            _chargeIndicator.SetActive(card.HasCharge);
            _divineShieldIndicator.SetActive(card.HasDivineShield);
            _frozenIndicator.SetActive(card.IsFrozen);
            _canAttackIndicator.SetActive(card.CanAttack && card.IsOnBoard);
        }

        private string GetCardName(CardComponent card)
        {
            string[] minionNames = { "Warrior", "Archer", "Mage", "Knight", "Dragon", "Giant", "Spirit" };
            string[] spellNames = { "Fireball", "Arcane Intellect", "Frostbolt", "Lightning Bolt", "Heal" };
            string[] weaponNames = { "Sword", "Axe", "Bow" };

            switch (card.Type)
            {
                case CardType.Minion:
                    if (card.TemplateId >= 0 && card.TemplateId < minionNames.Length)
                        return minionNames[card.TemplateId];
                    break;
                case CardType.Spell:
                    if (card.TemplateId >= 0 && card.TemplateId < spellNames.Length)
                        return spellNames[card.TemplateId];
                    break;
                case CardType.Weapon:
                    if (card.TemplateId >= 0 && card.TemplateId < weaponNames.Length)
                        return weaponNames[card.TemplateId];
                    break;
            }
            return "Unknown";
        }

        private string GetCardDescription(CardComponent card)
        {
            switch (card.Type)
            {
                case CardType.Minion:
                    string desc = "";
                    if (card.HasTaunt) desc += "Taunt. ";
                    if (card.HasCharge) desc += "Charge. ";
                    if (card.HasDivineShield) desc += "Divine Shield. ";
                    return desc;
                case CardType.Spell:
                    switch (card.Effect)
                    {
                        case SpellEffect.Damage:
                            return $"Deal {card.EffectValue} damage.";
                        case SpellEffect.Heal:
                            return $"Restore {card.EffectValue} health.";
                        case SpellEffect.Draw:
                            return $"Draw {card.EffectValue} cards.";
                        case SpellEffect.Freeze:
                            return $"Freeze a character. Deal {card.EffectValue} damage.";
                        case SpellEffect.Buff:
                            return $"Give a minion +{card.EffectValue}/+{card.EffectValue}.";
                    }
                    break;
                case CardType.Weapon:
                    return "Equip this weapon.";
            }
            return "";
        }

        public void SetSelected(bool selected)
        {
            _isSelected = selected;
            _selectedHighlight.enabled = selected;
        }

        public void SetHovered(bool hovered)
        {
            _isHovered = hovered;
            transform.localScale = hovered ? Vector3.one * 1.1f : Vector3.one;
        }

        public void SetCanPlay(bool canPlay)
        {
            _cardBackground.color = canPlay ? GetTypeColor() : _disabledColor;
        }

        private Color GetTypeColor()
        {
            var entityManager = World.DefaultGameObjectInjectionWorld.EntityManager;
            if (entityManager.HasComponent<CardComponent>(_entity))
            {
                var card = entityManager.GetComponentData<CardComponent>(_entity);
                switch (card.Type)
                {
                    case CardType.Minion: return _minionColor;
                    case CardType.Spell: return _spellColor;
                    case CardType.Weapon: return _weaponColor;
                }
            }
            return _minionColor;
        }
    }
}
