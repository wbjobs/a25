using UnityEngine;
using UnityEngine.EventSystems;
using CardGame.Game;

namespace CardGame.UI
{
    public class CardInteraction : MonoBehaviour, IPointerEnterHandler, IPointerExitHandler, IPointerClickHandler
    {
        private CardVisual _cardVisual;

        private void Awake()
        {
            _cardVisual = GetComponent<CardVisual>();
        }

        public void OnPointerEnter(PointerEventData eventData)
        {
            if (_cardVisual != null && _cardVisual.IsOwner)
            {
                _cardVisual.SetHovered(true);
            }
        }

        public void OnPointerExit(PointerEventData eventData)
        {
            if (_cardVisual != null)
            {
                _cardVisual.SetHovered(false);
            }
        }

        public void OnPointerClick(PointerEventData eventData)
        {
            var gameStateManager = GameStateManager.Instance;
            if (gameStateManager == null || !gameStateManager.IsPlayerTurn || gameStateManager.IsGameOver)
            {
                return;
            }

            if (_cardVisual != null && _cardVisual.IsOwner)
            {
                if (eventData.button == PointerEventData.InputButton.Right)
                {
                    Input.InputHandler.Instance?.ClearSelection();
                }
            }
        }
    }
}
