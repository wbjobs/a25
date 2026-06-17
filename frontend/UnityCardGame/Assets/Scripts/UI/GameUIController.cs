using TMPro;
using UnityEngine;
using UnityEngine.UI;
using CardGame.Game;
using CardGame.Networking;
using CardGame.ECS;
using Unity.Entities;

namespace CardGame.UI
{
    public class GameUIController : MonoBehaviour
    {
        [Header("Player Info")]
        [SerializeField] private TextMeshProUGUI _playerHealthText;
        [SerializeField] private TextMeshProUGUI _playerManaText;
        [SerializeField] private TextMeshProUGUI _playerArmorText;
        [SerializeField] private TextMeshProUGUI _playerDeckText;

        [Header("Opponent Info")]
        [SerializeField] private TextMeshProUGUI _opponentHealthText;
        [SerializeField] private TextMeshProUGUI _opponentManaText;
        [SerializeField] private TextMeshProUGUI _opponentArmorText;
        [SerializeField] private TextMeshProUGUI _opponentDeckText;

        [Header("Game Info")]
        [SerializeField] private TextMeshProUGUI _turnText;
        [SerializeField] private TextMeshProUGUI _turnNumberText;
        [SerializeField] private Button _endTurnButton;
        [SerializeField] private Button _concedeButton;

        [Header("Game Over")]
        [SerializeField] private GameObject _gameOverPanel;
        [SerializeField] private TextMeshProUGUI _gameOverText;
        [SerializeField] private Button _playAgainButton;
        [SerializeField] private Button _backToMenuButton;

        [Header("Connection Status")]
        [SerializeField] private TextMeshProUGUI _connectionStatusText;

        private EntityManager _entityManager;

        private void Start()
        {
            _entityManager = World.DefaultGameObjectInjectionWorld.EntityManager;

            _endTurnButton.onClick.AddListener(OnEndTurnClicked);
            _concedeButton.onClick.AddListener(OnConcedeClicked);
            _playAgainButton.onClick.AddListener(OnPlayAgainClicked);
            _backToMenuButton.onClick.AddListener(OnBackToMenuClicked);

            var gameStateManager = GameStateManager.Instance;
            if (gameStateManager != null)
            {
                gameStateManager.OnGameStateChanged += UpdateUI;
                gameStateManager.OnTurnChanged += OnTurnChanged;
                gameStateManager.OnGameOver += OnGameOver;
            }

            var network = GameNetworkClient.Instance;
            if (network != null)
            {
                network.OnConnectionStatusChanged += OnConnectionStatusChanged;
            }

            _gameOverPanel.SetActive(false);
            UpdateUI();
        }

        private void UpdateUI()
        {
            var gameStateManager = GameStateManager.Instance;
            if (gameStateManager == null) return;

            var localPlayerEntity = gameStateManager.LocalPlayerEntity;
            var opponentPlayerEntity = gameStateManager.OpponentPlayerEntity;

            if (_entityManager.Exists(localPlayerEntity))
            {
                var player = _entityManager.GetComponentData<PlayerComponent>(localPlayerEntity);
                _playerHealthText.text = player.Health.ToString();
                _playerManaText.text = $"{player.Mana}/{player.MaxMana}";
                _playerArmorText.text = player.Armor > 0 ? player.Armor.ToString() : string.Empty;
                _playerDeckText.text = player.DeckSize.ToString();
            }

            if (_entityManager.Exists(opponentPlayerEntity))
            {
                var opponent = _entityManager.GetComponentData<PlayerComponent>(opponentPlayerEntity);
                _opponentHealthText.text = opponent.Health.ToString();
                _opponentManaText.text = $"{opponent.Mana}/{opponent.MaxMana}";
                _opponentArmorText.text = opponent.Armor > 0 ? opponent.Armor.ToString() : string.Empty;
                _opponentDeckText.text = opponent.DeckSize.ToString();
            }

            var gameStateQuery = _entityManager.CreateEntityQuery(typeof(GameStateComponent));
            if (gameStateQuery.HasSingleton<GameStateComponent>())
            {
                var gameState = gameStateQuery.GetSingleton<GameStateComponent>();
                _turnNumberText.text = $"Turn {gameState.Turn}";
            }

            UpdateEndTurnButton();
        }

        private void OnTurnChanged(bool isPlayerTurn)
        {
            _turnText.text = isPlayerTurn ? "Your Turn" : "Opponent's Turn";
            UpdateEndTurnButton();
        }

        private void UpdateEndTurnButton()
        {
            var gameStateManager = GameStateManager.Instance;
            _endTurnButton.interactable = gameStateManager != null && gameStateManager.IsPlayerTurn && !gameStateManager.IsGameOver;
        }

        private void OnGameOver(string winnerId)
        {
            _gameOverPanel.SetActive(true);

            var gameStateManager = GameStateManager.Instance;
            if (gameStateManager != null)
            {
                bool isWinner = ulong.Parse(winnerId) == gameStateManager.LocalPlayerId;
                _gameOverText.text = isWinner ? "You Win!" : "You Lose!";
            }
        }

        private void OnConnectionStatusChanged(bool isConnected)
        {
            _connectionStatusText.text = isConnected ? "Connected" : "Disconnected";
            _connectionStatusText.color = isConnected ? Color.green : Color.red;
        }

        private async void OnEndTurnClicked()
        {
            var network = GameNetworkClient.Instance;
            if (network == null) return;

            _endTurnButton.interactable = false;
            await network.EndTurn();
        }

        private async void OnConcedeClicked()
        {
            var network = GameNetworkClient.Instance;
            if (network == null) return;

            bool confirm = true;
            if (confirm)
            {
                await network.Concede();
            }
        }

        private async void OnPlayAgainClicked()
        {
            _gameOverPanel.SetActive(false);

            var gameStateManager = GameStateManager.Instance;
            gameStateManager?.Clear();

            var network = GameNetworkClient.Instance;
            if (network != null)
            {
                await network.FindMatch();
            }
        }

        private void OnBackToMenuClicked()
        {
            _gameOverPanel.SetActive(false);

            var gameStateManager = GameStateManager.Instance;
            gameStateManager?.Clear();
        }

        private void OnDestroy()
        {
            var gameStateManager = GameStateManager.Instance;
            if (gameStateManager != null)
            {
                gameStateManager.OnGameStateChanged -= UpdateUI;
                gameStateManager.OnTurnChanged -= OnTurnChanged;
                gameStateManager.OnGameOver -= OnGameOver;
            }

            var network = GameNetworkClient.Instance;
            if (network != null)
            {
                network.OnConnectionStatusChanged -= OnConnectionStatusChanged;
            }
        }
    }
}
