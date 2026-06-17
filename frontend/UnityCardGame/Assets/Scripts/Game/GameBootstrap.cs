using System;
using System.Collections.Generic;
using Unity.Entities;
using Unity.Mathematics;
using Unity.Transforms;
using UnityEngine;
using CardGame.ECS;
using CardGame.Networking;
using CardGame.Proto;
using CardGame.UI;
using CardGame.Utils;

namespace CardGame.Game
{
    public class GameBootstrap : MonoBehaviour
    {
        [Header("Prefabs")]
        [SerializeField] private GameObject _cardPrefab;
        [SerializeField] private GameObject _playerPrefab;
        [SerializeField] private GameObject _cardVisualPrefab;
        [SerializeField] private Transform _playerHandContainer;
        [SerializeField] private Transform _opponentHandContainer;
        [SerializeField] private Transform _playerBoardContainer;
        [SerializeField] private Transform _opponentBoardContainer;

        [Header("Server Settings")]
        [SerializeField] private string _serverAddress = "localhost:8080";
        [SerializeField] private string _defaultPlayerId = "player_1";
        [SerializeField] private string _defaultPlayerName = "Player1";

        private EntityManager _entityManager;
        private Entity _ecsCardPrefab;
        private Entity _ecsPlayerPrefab;
        private GameObjectConverter _converter;
        private Dictionary<ulong, GameObject> _cardGameObjects = new Dictionary<ulong, GameObject>();

        public static GameBootstrap Instance { get; private set; }

        private void Awake()
        {
            if (Instance != null && Instance != this)
            {
                Destroy(gameObject);
                return;
            }
            Instance = this;

            DontDestroyOnLoad(gameObject);

            if (FindObjectOfType<UnityMainThreadDispatcher>() == null)
            {
                var dispatcher = new GameObject("UnityMainThreadDispatcher");
                dispatcher.AddComponent<UnityMainThreadDispatcher>();
            }

            if (FindObjectOfType<GameNetworkClient>() == null)
            {
                var networkObj = new GameObject("GameNetworkClient");
                networkObj.AddComponent<GameNetworkClient>();
            }

            if (FindObjectOfType<GameStateManager>() == null)
            {
                var stateObj = new GameObject("GameStateManager");
                stateObj.AddComponent<GameStateManager>();
            }
        }

        private async void Start()
        {
            _entityManager = World.DefaultGameObjectInjectionWorld.EntityManager;
            _converter = new GameObjectConverter(_entityManager);

            InitializeECSPrefabs();

            var network = GameNetworkClient.Instance;
            network.OnMatchUpdate += OnMatchUpdate;
            network.OnGameStarted += OnGameStarted;

            try
            {
                await network.Connect(_serverAddress, _defaultPlayerId, _defaultPlayerName);
                await network.FindMatch();
            }
            catch (Exception e)
            {
                Debug.LogError($"Failed to connect: {e.Message}");
            }
        }

        private void InitializeECSPrefabs()
        {
            _ecsCardPrefab = _converter.CreateCardPrefab();
            _ecsPlayerPrefab = _converter.CreatePlayerPrefab();

            var gameStateManager = GameStateManager.Instance;
            gameStateManager.Initialize(_entityManager, _ecsCardPrefab, _ecsPlayerPrefab);
            gameStateManager.OnGameStateChanged += UpdateCardVisuals;
        }

        private void OnMatchUpdate(MatchResponse response)
        {
            Debug.Log($"Match status: {response.Status}");
        }

        private void OnGameStarted(string gameId)
        {
            Debug.Log($"Game started: {gameId}");
            _ = GameNetworkClient.Instance?.StartGameStream();
        }

        private void UpdateCardVisuals()
        {
            UnityMainThreadDispatcher.Enqueue(() =>
            {
                var gameStateManager = GameStateManager.Instance;
                if (gameStateManager == null) return;

                var localPlayerId = gameStateManager.LocalPlayerId;
                var cards = gameStateManager.EntityFactory.GetAllCards(Allocator.Temp);

                HashSet<ulong> processedCards = new HashSet<ulong>();

                foreach (var entity in cards)
                {
                    if (!_entityManager.HasComponent<CardComponent>(entity)) continue;

                    var card = _entityManager.GetComponentData<CardComponent>(entity);
                    processedCards.Add(card.EntityId);

                    GameObject cardGO;
                    CardVisual cardVisual;

                    if (_cardGameObjects.TryGetValue(card.EntityId, out var existingGO))
                    {
                        cardGO = existingGO;
                        cardVisual = cardGO.GetComponent<CardVisual>();
                    }
                    else
                    {
                        bool isOwner = card.OwnerPlayerId == localPlayerId;
                        cardGO = Instantiate(_cardVisualPrefab);
                        cardVisual = cardGO.GetComponent<CardVisual>();
                        cardVisual.Initialize(entity, card, isOwner);
                        _cardGameObjects[card.EntityId] = cardGO;
                    }

                    cardVisual.UpdateVisual(card);
                    UpdateCardPosition(cardVisual.transform, card, localPlayerId);
                }

                List<ulong> cardsToDestroy = new List<ulong>();
                foreach (var kvp in _cardGameObjects)
                {
                    if (!processedCards.Contains(kvp.Key))
                    {
                        Destroy(kvp.Value);
                        cardsToDestroy.Add(kvp.Key);
                    }
                }

                foreach (var id in cardsToDestroy)
                {
                    _cardGameObjects.Remove(id);
                }

                cards.Dispose();
            });
        }

        private void UpdateCardPosition(Transform cardTransform, CardComponent card, ulong localPlayerId)
        {
            bool isOwner = card.OwnerPlayerId == localPlayerId;
            var entity = GameStateManager.Instance.GetCardEntity(card.EntityId);

            if (card.IsInHand)
            {
                var handContainer = isOwner ? _playerHandContainer : _opponentHandContainer;
                int handIndex = GetHandIndex(card.EntityId, isOwner);

                if (handContainer != null && handIndex >= 0)
                {
                    cardTransform.SetParent(handContainer, false);
                    float spacing = 150f;
                    float totalWidth = 150f;
                    float x = (handIndex * spacing) - totalWidth / 2f;
                    cardTransform.localPosition = new Vector3(x, 0, 0);
                    cardTransform.localRotation = Quaternion.identity;
                }
            }
            else if (card.IsOnBoard)
            {
                var boardContainer = isOwner ? _playerBoardContainer : _opponentBoardContainer;

                if (boardContainer != null && entity != Entity.Null && _entityManager.HasComponent<MinionComponent>(entity))
                {
                    var minion = _entityManager.GetComponentData<MinionComponent>(entity);
                    int boardIndex = minion.BoardPosition;

                    cardTransform.SetParent(boardContainer, false);

                    float spacing = 120f;
                    float totalWidth = 120f;
                    float x = (boardIndex * spacing) - totalWidth / 2f;
                    float y = isOwner ? 100f : -100f;
                    cardTransform.localPosition = new Vector3(x, y, 0);
                    cardTransform.localRotation = Quaternion.identity;
                }
            }
        }

        private int GetHandIndex(ulong cardId, bool isOwner)
        {
            var gameStateManager = GameStateManager.Instance;
            var playerEntity = isOwner ? gameStateManager.LocalPlayerEntity : gameStateManager.OpponentPlayerEntity;

            if (!_entityManager.Exists(playerEntity))
            {
                return 0;
            }

            var player = _entityManager.GetComponentData<PlayerComponent>(playerEntity);
            return (int)(cardId % (ulong)Math.Max(1, player.HandSize));
        }

        private void OnDestroy()
        {
            var network = GameNetworkClient.Instance;
            if (network != null)
            {
                network.OnMatchUpdate -= OnMatchUpdate;
                network.OnGameStarted -= OnGameStarted;
            }

            var gameStateManager = GameStateManager.Instance;
            if (gameStateManager != null)
            {
                gameStateManager.OnGameStateChanged -= UpdateCardVisuals;
            }
        }
    }
}
