using System;
using System.Collections.Generic;
using UnityEngine;
using UnityEngine.EventSystems;
using CardGame.Game;
using CardGame.Networking;
using CardGame.ECS;
using CardGame.UI;
using Unity.Entities;

namespace CardGame.Input
{
    public class InputHandler : MonoBehaviour
    {
        private EntityManager _entityManager;
        private Camera _mainCamera;
        private Entity _selectedEntity;
        private CardVisual _selectedCard;
        private bool _isSelectingTarget;
        private Action<ulong> _targetSelectedCallback;
        private ulong _selectedCardEntityId;

        public event Action<Entity> OnCardSelected;
        public event Action<Entity> OnTargetSelected;

        public static InputHandler Instance { get; private set; }

        private void Awake()
        {
            if (Instance != null && Instance != this)
            {
                Destroy(gameObject);
                return;
            }
            Instance = this;
        }

        private void Start()
        {
            _entityManager = World.DefaultGameObjectInjectionWorld.EntityManager;
            _mainCamera = Camera.main;
        }

        private void Update()
        {
            HandleMouseInput();
            HandleKeyboardInput();
        }

        private void HandleMouseInput()
        {
            if (EventSystem.current.IsPointerOverGameObject())
            {
                return;
            }

            if (UnityEngine.Input.GetMouseButtonDown(0))
            {
                HandleLeftClick();
            }
            else if (UnityEngine.Input.GetMouseButtonDown(1))
            {
                HandleRightClick();
            }
        }

        private void HandleLeftClick()
        {
            var gameStateManager = GameStateManager.Instance;
            if (gameStateManager == null || !gameStateManager.IsPlayerTurn || gameStateManager.IsGameOver)
            {
                return;
            }

            Ray ray = _mainCamera.ScreenPointToRay(UnityEngine.Input.mousePosition);
            RaycastHit hit;

            if (Physics.Raycast(ray, out hit))
            {
                var cardVisual = hit.collider.GetComponent<CardVisual>();
                if (cardVisual != null)
                {
                    HandleCardClick(cardVisual);
                }
                else
                {
                    HandleBoardClick(hit.point);
                }
            }
            else
            {
                ClearSelection();
            }
        }

        private void HandleCardClick(CardVisual cardVisual)
        {
            var cardEntity = cardVisual.Entity;
            if (!_entityManager.Exists(cardEntity)) return;

            var card = _entityManager.GetComponentData<CardComponent>(cardEntity);

            if (_isSelectingTarget)
            {
                if (IsValidTarget(cardVisual, card))
                {
                    _targetSelectedCallback?.Invoke(card.EntityId);
                    ClearSelection();
                }
                else
                {
                    Debug.Log("Invalid target");
                }
            }
            else if (cardVisual.IsOwner)
            {
                if (card.IsInHand)
                {
                    TryPlayCard(cardVisual, cardEntity, card);
                }
                else if (card.IsOnBoard && card.Type == CardType.Minion)
                {
                    TrySelectAttacker(cardVisual, cardEntity, card);
                }
            }
        }

        private void HandleBoardClick(Vector3 position)
        {
            if (_isSelectingTarget)
            {
                var gameStateManager = GameStateManager.Instance;
                if (gameStateManager == null) return;

                if (position.z < 0)
                {
                    if (_selectedCardEntityId != 0)
                    {
                        _targetSelectedCallback?.Invoke(gameStateManager.LocalPlayerId);
                    }
                }
                else
                {
                    if (_selectedCardEntityId != 0)
                    {
                        _targetSelectedCallback?.Invoke(gameStateManager.OpponentPlayerEntity != Entity.Null ? 
                            _entityManager.GetComponentData<PlayerComponent>(gameStateManager.OpponentPlayerEntity).PlayerId : 0);
                    }
                }
                ClearSelection();
            }
        }

        private void HandleRightClick()
        {
            ClearSelection();
        }

        private async void TryPlayCard(CardVisual cardVisual, Entity cardEntity, CardComponent card)
        {
            var gameStateManager = GameStateManager.Instance;
            if (gameStateManager == null || !gameStateManager.IsPlayerTurn) return;

            var player = _entityManager.GetComponentData<PlayerComponent>(gameStateManager.LocalPlayerEntity);
            if (card.Cost > player.Mana)
            {
                Debug.Log("Not enough mana");
                return;
            }

            if (NeedsTarget(card))
            {
                _isSelectingTarget = true;
                _selectedCard = cardVisual;
                _selectedCardEntityId = card.EntityId;
                cardVisual.SetSelected(true);

                _targetSelectedCallback = async (targetId) =>
                {
                    var network = GameNetworkClient.Instance;
                    if (network != null)
                    {
                        await network.PlayCard(card.EntityId, targetId);
                    }
                };
            }
            else
            {
                var network = GameNetworkClient.Instance;
                if (network != null)
                {
                    await network.PlayCard(card.EntityId, 0);
                }
            }
        }

        private bool NeedsTarget(CardComponent card)
        {
            if (card.Type == CardType.Spell)
            {
                return card.Effect == SpellEffect.Damage || 
                       card.Effect == SpellEffect.Heal || 
                       card.Effect == SpellEffect.Freeze ||
                       card.Effect == SpellEffect.Buff;
            }
            return false;
        }

        private bool IsValidTarget(CardVisual target, CardComponent targetCard)
        {
            if (_selectedCard == null) return false;

            var selectedCard = _entityManager.GetComponentData<CardComponent>(_selectedCard.Entity);

            if (selectedCard.Type == CardType.Minion)
            {
                if (targetCard.IsOnBoard)
                {
                    if (targetCard.HasTaunt)
                    {
                        return !target.IsOwner;
                    }
                    return !target.IsOwner;
                }
                return false;
            }
            else if (selectedCard.Type == CardType.Spell)
            {
                switch (selectedCard.Effect)
                {
                    case SpellEffect.Damage:
                        return true;
                    case SpellEffect.Heal:
                        return target.IsOwner;
                    case SpellEffect.Freeze:
                        return !target.IsOwner;
                    case SpellEffect.Buff:
                        return target.IsOwner && targetCard.IsOnBoard && targetCard.Type == CardType.Minion;
                }
            }

            return false;
        }

        private async void TrySelectAttacker(CardVisual cardVisual, Entity cardEntity, CardComponent card)
        {
            if (!card.CanAttack)
            {
                Debug.Log("This minion can't attack");
                return;
            }

            _isSelectingTarget = true;
            _selectedCard = cardVisual;
            _selectedCardEntityId = card.EntityId;
            cardVisual.SetSelected(true);

            _targetSelectedCallback = async (targetId) =>
            {
                var network = GameNetworkClient.Instance;
                if (network != null)
                {
                    await network.Attack(card.EntityId, targetId);
                }
            };
        }

        private void ClearSelection()
        {
            if (_selectedCard != null)
            {
                _selectedCard.SetSelected(false);
            }

            _selectedCard = null;
            _selectedEntity = Entity.Null;
            _selectedCardEntityId = 0;
            _isSelectingTarget = false;
            _targetSelectedCallback = null;
        }

        private void HandleKeyboardInput()
        {
            if (UnityEngine.Input.GetKeyDown(KeyCode.Escape))
            {
                ClearSelection();
            }

            if (UnityEngine.Input.GetKeyDown(KeyCode.Space))
            {
                var gameStateManager = GameStateManager.Instance;
                if (gameStateManager != null && gameStateManager.IsPlayerTurn && !gameStateManager.IsGameOver)
                {
                    _ = GameNetworkClient.Instance?.EndTurn();
                }
            }
        }
    }
}
