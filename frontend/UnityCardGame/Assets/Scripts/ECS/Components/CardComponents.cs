using Unity.Entities;
using Unity.Mathematics;

namespace CardGame.ECS
{
    public enum CardType : byte
    {
        Minion,
        Spell,
        Weapon
    }

    public enum SpellEffect : byte
    {
        Damage,
        Draw,
        Heal,
        Buff
    }

    public enum TargetType : byte
    {
        None,
        Any,
        Enemy,
        Friendly,
        Hero,
        Minion
    }

    public struct CardComponent : IComponentData
    {
        public ulong EntityId;
        public int TemplateId;
        public CardType Type;
        public int Cost;
        public int Attack;
        public int Health;
        public int MaxHealth;
        public int Durability;
        public SpellEffect Effect;
        public int EffectValue;
        public TargetType TargetType;
        public bool CanAttack;
        public bool HasTaunt;
        public bool HasCharge;
        public bool HasDivineShield;
        public bool IsFrozen;
        public bool IsInHand;
        public bool IsOnBoard;
        public bool IsSelected;
    }

    public struct HealthComponent : IComponentData
    {
        public int Current;
        public int Max;
    }

    public struct AttackComponent : IComponentData
    {
        public int Value;
    }

    public struct OwnerComponent : IComponentData
    {
        public ulong PlayerId;
    }

    public struct PositionComponent : IComponentData
    {
        public float3 Value;
        public float3 Target;
    }

    public struct PlayerComponent : IComponentData
    {
        public ulong PlayerId;
        public int Health;
        public int MaxHealth;
        public int Mana;
        public int MaxMana;
        public int Armor;
        public int Attack;
        public int DeckSize;
        public int HandSize;
        public int BoardSize;
        public bool IsCurrentTurn;
    }

    public struct GameStateComponent : IComponentData
    {
        public ulong FrameNumber;
        public int Turn;
        public ulong CurrentTurnPlayerId;
        public int State;
        public bool IsGameOver;
        public ulong WinnerId;
    }

    public struct MinionComponent : IComponentData
    {
        public int BoardPosition;
        public int AttacksThisTurn;
        public int MaxAttacks;
        public bool CanAttack;
    }

    public struct WeaponComponent : IComponentData
    {
        public int Durability;
        public int MaxDurability;
        public bool IsEquipped;
    }

    public struct SpellComponent : IComponentData
    {
        public SpellEffect Effect;
        public int Value;
        public bool IsAOE;
    }

    public struct SelectedComponent : IComponentData, IEnableableComponent
    {
    }

    public struct TargetableComponent : IComponentData, IEnableableComponent
    {
    }

    public struct DyingComponent : IComponentData, IEnableableComponent
    {
    }

    public struct AnimationComponent : IComponentData
    {
        public float AnimationTime;
        public float AnimationDuration;
        public int AnimationType;
        public bool IsPlaying;
    }

    public struct HoverComponent : IComponentData, IEnableableComponent
    {
    }
}
