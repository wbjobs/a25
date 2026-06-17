using Unity.Collections;
using Unity.Entities;
using Unity.Mathematics;
using Unity.Transforms;
using UnityEngine;
using CardGame.Proto;

namespace CardGame.ECS
{
    public class EntityFactory
    {
        private readonly EntityManager _entityManager;
        private readonly Entity _cardPrefab;
        private readonly Entity _playerPrefab;

        public EntityFactory(EntityManager entityManager, Entity cardPrefab, Entity playerPrefab)
        {
            _entityManager = entityManager;
            _cardPrefab = cardPrefab;
            _playerPrefab = playerPrefab;
        }

        public Entity CreateCard(Proto.Card protoCard, ulong localPlayerId)
        {
            var entity = _entityManager.Instantiate(_cardPrefab);

            var cardComp = new CardComponent
            {
                EntityId = protoCard.EntityId,
                TemplateId = int.Parse(protoCard.TemplateId.Replace("minion_", "").Replace("spell_", "").Replace("weapon_", "")),
                Type = (CardType)protoCard.Type,
                Cost = protoCard.Cost,
                Attack = protoCard.Attack,
                Health = protoCard.Health,
                MaxHealth = protoCard.MaxHealth,
                Durability = protoCard.Durability,
                Effect = (SpellEffect)protoCard.SpellEffect,
                EffectValue = protoCard.SpellValue,
                CanAttack = protoCard.CanAttack,
                HasTaunt = protoCard.Taunt,
                HasCharge = protoCard.Charge,
                HasDivineShield = protoCard.DivineShield,
                IsFrozen = protoCard.Frozen,
                IsInHand = protoCard.EntityId != 0,
                IsOnBoard = false
            };

            _entityManager.SetComponentData(entity, cardComp);
            _entityManager.SetComponentData(entity, new OwnerComponent { PlayerId = localPlayerId });
            _entityManager.SetComponentData(entity, new HealthComponent { Current = protoCard.Health, Max = protoCard.MaxHealth });
            _entityManager.SetComponentData(entity, new AttackComponent { Value = protoCard.Attack });

            switch (cardComp.Type)
            {
                case CardType.Minion:
                    _entityManager.AddComponentData(entity, new MinionComponent
                    {
                        BoardPosition = -1,
                        AttacksThisTurn = 0,
                        MaxAttacks = 1,
                        CanAttack = protoCard.CanAttack
                    });
                    break;
                case CardType.Spell:
                    _entityManager.AddComponentData(entity, new SpellComponent
                    {
                        Effect = cardComp.Effect,
                        Value = cardComp.EffectValue,
                        IsAOE = false
                    });
                    break;
                case CardType.Weapon:
                    _entityManager.AddComponentData(entity, new WeaponComponent
                    {
                        Durability = protoCard.Durability,
                        MaxDurability = protoCard.Durability,
                        IsEquipped = false
                    });
                    break;
            }

            return entity;
        }

        public Entity CreatePlayer(Proto.Player protoPlayer, bool isLocalPlayer)
        {
            var entity = _entityManager.Instantiate(_playerPrefab);

            var playerComp = new PlayerComponent
            {
                PlayerId = ulong.Parse(protoPlayer.PlayerId),
                Health = protoPlayer.Health,
                MaxHealth = protoPlayer.MaxHealth,
                Mana = protoPlayer.Mana,
                MaxMana = protoPlayer.MaxMana,
                Armor = protoPlayer.Armor,
                Attack = protoPlayer.Attack,
                DeckSize = protoPlayer.DeckSize,
                HandSize = protoPlayer.Hand.Count,
                BoardSize = protoPlayer.Board.Count,
                IsCurrentTurn = false
            };

            _entityManager.SetComponentData(entity, playerComp);
            _entityManager.SetComponentData(entity, new HealthComponent { Current = protoPlayer.Health, Max = protoPlayer.MaxHealth });
            _entityManager.SetComponentData(entity, new PositionComponent
            {
                Value = isLocalPlayer ? new float3(0, 0, -5) : new float3(0, 0, 5),
                Target = isLocalPlayer ? new float3(0, 0, -5) : new float3(0, 0, 5)
            });

            return entity;
        }

        public Entity CreateGameState(GameStatus status)
        {
            var entity = _entityManager.CreateEntity();

            var stateComp = new GameStateComponent
            {
                FrameNumber = status.FrameNumber,
                Turn = status.Turn,
                CurrentTurnPlayerId = ulong.Parse(status.CurrentTurnPlayerId),
                State = (int)status.State,
                IsGameOver = status.State == Proto.GameState.Finished,
                WinnerId = status.Winner != null ? ulong.Parse(status.Winner) : 0
            };

            _entityManager.AddComponentData(entity, stateComp);
            return entity;
        }

        public void UpdateCardPosition(Entity entity, float3 position, float3 target)
        {
            _entityManager.SetComponentData(entity, new PositionComponent
            {
                Value = position,
                Target = target
            });
            _entityManager.SetComponentData(entity, LocalTransform.FromPosition(position));
        }

        public void UpdateCardFromProto(Entity entity, Proto.Card protoCard)
        {
            var card = _entityManager.GetComponentData<CardComponent>(entity);
            card.Attack = protoCard.Attack;
            card.Health = protoCard.Health;
            card.MaxHealth = protoCard.MaxHealth;
            card.CanAttack = protoCard.CanAttack;
            card.IsFrozen = protoCard.Frozen;
            _entityManager.SetComponentData(entity, card);

            var health = _entityManager.GetComponentData<HealthComponent>(entity);
            health.Current = protoCard.Health;
            health.Max = protoCard.MaxHealth;
            _entityManager.SetComponentData(entity, health);

            if (_entityManager.HasComponent<MinionComponent>(entity))
            {
                var minion = _entityManager.GetComponentData<MinionComponent>(entity);
                minion.CanAttack = protoCard.CanAttack;
                _entityManager.SetComponentData(entity, minion);
            }
        }

        public void UpdatePlayerFromProto(Entity entity, Proto.Player protoPlayer, bool isCurrentTurn)
        {
            var player = _entityManager.GetComponentData<PlayerComponent>(entity);
            player.Health = protoPlayer.Health;
            player.MaxHealth = protoPlayer.MaxHealth;
            player.Mana = protoPlayer.Mana;
            player.MaxMana = protoPlayer.MaxMana;
            player.Armor = protoPlayer.Armor;
            player.Attack = protoPlayer.Attack;
            player.DeckSize = protoPlayer.DeckSize;
            player.HandSize = protoPlayer.Hand.Count;
            player.BoardSize = protoPlayer.Board.Count;
            player.IsCurrentTurn = isCurrentTurn;
            _entityManager.SetComponentData(entity, player);

            var health = _entityManager.GetComponentData<HealthComponent>(entity);
            health.Current = protoPlayer.Health;
            health.Max = protoPlayer.MaxHealth;
            _entityManager.SetComponentData(entity, health);
        }

        public void UpdateGameState(Entity entity, GameStatus status)
        {
            var state = _entityManager.GetComponentData<GameStateComponent>(entity);
            state.FrameNumber = status.FrameNumber;
            state.Turn = status.Turn;
            state.CurrentTurnPlayerId = ulong.Parse(status.CurrentTurnPlayerId);
            state.State = (int)status.State;
            state.IsGameOver = status.State == Proto.GameState.Finished;
            state.WinnerId = status.Winner != null ? ulong.Parse(status.Winner) : 0;
            _entityManager.SetComponentData(entity, state);
        }

        public void DestroyEntity(Entity entity)
        {
            if (_entityManager.Exists(entity))
            {
                _entityManager.DestroyEntity(entity);
            }
        }

        public NativeArray<Entity> GetAllCards(Allocator allocator)
        {
            var query = _entityManager.CreateEntityQuery(typeof(CardComponent));
            return query.ToEntityArray(allocator);
        }

        public NativeArray<Entity> GetAllPlayers(Allocator allocator)
        {
            var query = _entityManager.CreateEntityQuery(typeof(PlayerComponent));
            return query.ToEntityArray(allocator);
        }
    }
}
