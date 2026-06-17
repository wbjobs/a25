using System;
using System.Collections.Generic;
using Unity.Entities;
using UnityEngine;
using CardGame.ECS;
using CardGame.Networking;
using CardGame.Proto;

namespace CardGame.Game
{
    public class GameStateManager : MonoBehaviour
    {
        private EntityManager _entityManager;
        private EntityFactory _entityFactory;
        private Entity _gameStateEntity;
        private Entity _localPlayerEntity;
        private Entity _opponentPlayerEntity;
        private Dictionary<ulong, Entity> _cardEntities = new Dictionary<ulong, Entity>();
        private ulong _localPlayerId;
        private bool _isPlayerTurn;
        private bool _isGameOver;

        public EntityFactory EntityFactory => _entityFactory;
        public Entity LocalPlayerEntity => _localPlayerEntity;
        public Entity OpponentPlayerEntity => _opponentPlayerEntity;
        public bool IsPlayerTurn => _isPlayerTurn;
        public bool IsGameOver => _isGameOver;
        public ulong LocalPlayerId => _localPlayerId;

        public event Action OnGameStateChanged;
        public event Action<bool> OnTurnChanged;
        public event Action<string> OnGameOver;

        public static GameStateManager Instance { get; private set; }

        private void Awake()
        {
            if (Instance != null && Instance != this)
            {
                Destroy(gameObject);
                return;
            }
            Instance = this;
        }

        public void Initialize(EntityManager entityManager, Entity cardPrefab, Entity playerPrefab)
        {
            _entityManager = entityManager;
            _entityFactory = new EntityFactory(entityManager, cardPrefab, playerPrefab);

            var network = GameNetworkClient.Instance;
            _localPlayerId = ulong.Parse(network.PlayerId);

            network.OnGameStateUpdate += HandleGameStateUpdate;
            network.OnGameFrame += HandleGameFrame;
        }

        private void HandleGameStateUpdate(GameStatus status)
        {
            UpdateGameState(status);
            OnGameStateChanged?.Invoke();
        }

        private void HandleGameFrame(GameFrame frame)
        {
            foreach (var ev in frame.Events)
            {
                HandleGameEvent(ev);
            }

            if (frame.Status != null)
            {
                UpdateGameState(frame.Status);
                OnGameStateChanged?.Invoke();
            }
        }

        public void UpdateGameState(GameStatus status)
        {
            if (_gameStateEntity == Entity.Null || !_entityManager.Exists(_gameStateEntity))
            {
                _gameStateEntity = _entityFactory.CreateGameState(status);
            }
            else
            {
                _entityFactory.UpdateGameState(_gameStateEntity, status);
            }

            bool currentTurnPlayerId = ulong.Parse(status.CurrentTurnPlayerId);
            _isPlayerTurn = currentTurnPlayerId == _localPlayerId;

            foreach (var protoPlayer in status.Players)
            {
                ulong playerId = ulong.Parse(protoPlayer.PlayerId);
                bool isLocalPlayer = playerId == _localPlayerId;

                if (isLocalPlayer)
                {
                    if (_localPlayerEntity == Entity.Null || !_entityManager.Exists(_localPlayerEntity))
                    {
                        _localPlayerEntity = _entityFactory.CreatePlayer(protoPlayer, true);
                    }
                    else
                    {
                        _entityFactory.UpdatePlayerFromProto(_localPlayerEntity, protoPlayer, _isPlayerTurn);
                    }
                }
                else
                {
                    if (_opponentPlayerEntity == Entity.Null || !_entityManager.Exists(_opponentPlayerEntity))
                    {
                        _opponentPlayerEntity = _entityFactory.CreatePlayer(protoPlayer, false);
                    }
                    else
                    {
                        _entityFactory.UpdatePlayerFromProto(_opponentPlayerEntity, protoPlayer, false);
                    }
                }
            }

            HashSet<ulong> receivedCardIds = new HashSet<ulong>();
            foreach (var protoCard in status.Cards)
            {
                receivedCardIds.Add(protoCard.EntityId);

                if (_cardEntities.TryGetValue(protoCard.EntityId, out var entity))
                {
                    if (_entityManager.Exists(entity))
                    {
                        _entityFactory.UpdateCardFromProto(entity, protoCard);
                    }
                    else
                    {
                        entity = _entityFactory.CreateCard(protoCard, _localPlayerId);
                        _cardEntities[protoCard.EntityId] = entity;
                    }
                }
                else
                {
                    entity = _entityFactory.CreateCard(protoCard, _localPlayerId);
                    _cardEntities[protoCard.EntityId] = entity;
                }
            }

            List<ulong> cardsToRemove = new List<ulong>();
            foreach (var kvp in _cardEntities)
            {
                if (!receivedCardIds.Contains(kvp.Key) && _entityManager.Exists(kvp.Value))
                {
                    _entityManager.AddComponent<ECS.DyingComponent>(kvp.Value);
                    cardsToRemove.Add(kvp.Key);
                }
            }

            foreach (var id in cardsToRemove)
            {
                _cardEntities.Remove(id);
            }

            if (status.State == Proto.GameState.Finished)
            {
                _isGameOver = true;
                OnGameOver?.Invoke(status.Winner);
            }

            OnTurnChanged?.Invoke(_isPlayerTurn);
        }

        private void HandleGameEvent(GameEvent ev)
        {
            switch (ev.EventType)
            {
                case "card_drawn":
                    ev.Data.TryGetValue("card_id", out var cardId);
                    Debug.Log($"Card drawn: {cardId}");
                    break;
                case "minion_summoned":
                    ev.Data.TryGetValue("card_name", out var cardName);
                    Debug.Log($"Minion summoned: {cardName}");
                    break;
                case "spell_cast":
                    ev.Data.TryGetValue("card_name", out var spellName);
                    Debug.Log($"Spell cast: {spellName}");
                    break;
                case "attack":
                    Debug.Log($"Attack from {ev.Data["attacker_id"]} to {ev.Data["target_id"]}");
                    break;
                case "damage_dealt":
                    Debug.Log($"Damage: {ev.Data["damage"]} to {ev.Data["target_id"]}");
                    break;
                case "turn_started":
                    Debug.Log($"Turn started for {ev.Data["player_id"]}");
                    break;
                case "turn_ended":
                    Debug.Log($"Turn ended for {ev.Data["player_id"]}");
                    break;
                case "player_conceded":
                    Debug.Log($"Player {ev.Data["player_id"]} conceded");
                    break;
                case "game_ended":
                    Debug.Log($"Game ended. Winner: {ev.Data["winner_id"]}");
                    break;
            }
        }

        public Entity GetCardEntity(ulong entityId)
        {
            if (_cardEntities.TryGetValue(entityId, out var entity))
            {
                return entity;
            }
            return Entity.Null;
        }

        public bool TryGetCardEntity(ulong entityId, out Entity entity)
        {
            return _cardEntities.TryGetValue(entityId, out entity);
        }

        public void Clear()
        {
            foreach (var kvp in _cardEntities)
            {
                if (_entityManager.Exists(kvp.Value))
                {
                    _entityManager.DestroyEntity(kvp.Value);
                }
            }
            _cardEntities.Clear();

            if (_entityManager.Exists(_localPlayerEntity))
            {
                _entityManager.DestroyEntity(_localPlayerEntity);
            }
            if (_entityManager.Exists(_opponentPlayerEntity))
            {
                _entityManager.DestroyEntity(_opponentPlayerEntity);
            }
            if (_entityManager.Exists(_gameStateEntity))
            {
                _entityManager.DestroyEntity(_gameStateEntity);
            }

            _localPlayerEntity = Entity.Null;
            _opponentPlayerEntity = Entity.Null;
            _gameStateEntity = Entity.Null;
            _isPlayerTurn = false;
            _isGameOver = false;
        }

        private void OnDestroy()
        {
            if (GameNetworkClient.Instance != null)
            {
                GameNetworkClient.Instance.OnGameStateUpdate -= HandleGameStateUpdate;
                GameNetworkClient.Instance.OnGameFrame -= HandleGameFrame;
            }
        }
    }
}
