using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using Grpc.Net.Client;
using UnityEngine;
using CardGame.Proto;

namespace CardGame.Networking
{
    public class GameNetworkClient : MonoBehaviour
    {
        private GrpcChannel _channel;
        private GameService.GameServiceClient _gameClient;
        private MatchService.MatchServiceClient _matchClient;

        private string _playerId;
        private string _playerName;
        private string _gameId;
        private string _matchId;

        private ulong _latestSnapshotFrame;
        private ulong _latestSnapshotHash;
        private uint _sequenceCounter;
        private Queue<Action> _pendingActions = new Queue<Action>();
        private bool _needsRollback;
        private GameSnapshot _rollbackSnapshot;
        private float _snapshotIntervalMs = 200f;
        private int _frameCountSinceLastAck;
        private ulong _confirmedFrame;

        public event Action<GameStatus> OnGameStateUpdate;
        public event Action<GameFrame> OnGameFrame;
        public event Action<MatchResponse> OnMatchUpdate;

        public bool IsConnected { get; private set; }
        public bool IsInGame { get; private set; }

        public static GameNetworkClient Instance { get; private set; }

        private void Awake()
        {
            if (Instance != null && Instance != this)
            {
                Destroy(gameObject);
                return;
            }
            Instance = this;
            DontDestroyOnLoad(gameObject);
        }

        public async Task Connect(string serverAddress, string playerId, string playerName)
        {
            _playerId = playerId;
            _playerName = playerName;

            try
            {
                _channel = GrpcChannel.ForAddress($"http://{serverAddress}");
                _gameClient = new GameService.GameServiceClient(_channel);
                _matchClient = new MatchService.MatchServiceClient(_channel);
                IsConnected = true;
                Debug.Log($"Connected to server: {serverAddress}");
            }
            catch (Exception e)
            {
                Debug.LogError($"Failed to connect: {e.Message}");
                throw;
            }
        }

        public async Task FindMatch(string gameType = "normal")
        {
            if (!IsConnected) throw new InvalidOperationException("Not connected");

            var request = new MatchRequest
            {
                Player = new Proto.Player
                {
                    PlayerId = _playerId,
                    PlayerName = _playerName
                },
                GameType = gameType
            };

            using var call = _matchClient.FindMatch(request);
            var responseStream = call.ResponseStream;

            await Task.Run(async () =>
            {
                await foreach (var response in responseStream.ReadAllAsync())
                {
                    UnityMainThreadDispatcher.Enqueue(() =>
                    {
                        HandleMatchResponse(response);
                    });
                }
            });
        }

        private void HandleMatchResponse(MatchResponse response)
        {
            OnMatchUpdate?.Invoke(response);

            switch (response.Status)
            {
                case MatchStatus.InQueue:
                    Debug.Log("In matching queue...");
                    break;
                case MatchStatus.Matched:
                    Debug.Log($"Matched with: {response.Opponent.PlayerName}");
                    break;
                case MatchStatus.InGame:
                    _matchId = response.MatchId;
                    _gameId = response.GameId;
                    IsInGame = true;
                    Debug.Log($"Game started! Game ID: {_gameId}");
                    _ = StartGameStream();
                    break;
                case MatchStatus.Cancelled:
                    Debug.Log("Match cancelled");
                    break;
            }
        }

        public async Task StartGameStream()
        {
            StartCoroutine(AutoAckCoroutine());

            var request = new StreamGameRequest
            {
                GameId = _gameId,
                PlayerId = _playerId
            };

            using var call = _gameClient.StreamGame(request);
            var responseStream = call.ResponseStream;

            await Task.Run(async () =>
            {
                await foreach (var frame in responseStream.ReadAllAsync())
                {
                    UnityMainThreadDispatcher.Enqueue(() =>
                    {
                        HandleGameFrame(frame);
                    });
                }
            });
        }

        private void HandleGameFrame(GameFrame frame)
        {
            if (frame.LatestSnapshot != null)
            {
                _latestSnapshotFrame = frame.LatestSnapshot.Frame;
                _latestSnapshotHash = frame.LatestSnapshot.Hash;
            }

            if (frame.Rollback != null)
            {
                HandleRollback(frame.Rollback);
            }

            if (frame.ConfirmedFrame > _confirmedFrame)
            {
                _confirmedFrame = frame.ConfirmedFrame;
            }

            _frameCountSinceLastAck++;
            if (_frameCountSinceLastAck >= 3)
            {
                SendAck();
                _frameCountSinceLastAck = 0;
            }

            OnGameFrame?.Invoke(frame);
            if (frame.Status != null)
            {
                OnGameStateUpdate?.Invoke(frame.Status);
            }
        }

        public void GetLatestSnapshotInfo(out ulong frame, out ulong hash)
        {
            frame = _latestSnapshotFrame;
            hash = _latestSnapshotHash;
        }

        public uint IncrementSequence()
        {
            return ++_sequenceCounter;
        }

        private void ApplySnapshot(GameSnapshot snapshot)
        {
            if (snapshot == null) return;

            GameStateManager.Instance.ApplyState(snapshot.State);
            _latestSnapshotFrame = snapshot.Frame;
            _latestSnapshotHash = snapshot.Hash;
        }

        private void HandleRollback(RollbackCommand rollback)
        {
            if (rollback == null) return;

            ApplySnapshot(rollback.Snapshot);
            _pendingActions.Clear();
            _needsRollback = false;
            _rollbackSnapshot = null;

            Debug.Log($"Rollback to frame {rollback.Snapshot.Frame}");
        }

        private void SendAck()
        {
            if (!IsInGame || _gameClient == null) return;

            var ack = new FrameAck
            {
                GameId = _gameId,
                PlayerId = _playerId,
                ConfirmedFrame = _confirmedFrame
            };

            _ = _gameClient.SendFrameAckAsync(ack);
        }

        private System.Collections.IEnumerator AutoAckCoroutine()
        {
            var wait = new WaitForSecondsRealtime(_snapshotIntervalMs / 1000f);
            while (IsInGame)
            {
                yield return wait;
                if (IsInGame)
                {
                    SendAck();
                }
            }
        }

        public async Task<PlayCardResponse> PlayCard(ulong cardEntityId, ulong targetEntityId = 0)
        {
            GetLatestSnapshotInfo(out ulong snapshotFrame, out ulong snapshotHash);

            var request = new PlayCardRequest
            {
                GameId = _gameId,
                PlayerId = _playerId,
                CardEntityId = cardEntityId,
                TargetEntityId = targetEntityId,
                BaseSnapshotFrame = snapshotFrame,
                BaseSnapshotHash = snapshotHash,
                Sequence = IncrementSequence()
            };

            var response = await _gameClient.PlayCardAsync(request);

            if (response.NeedsRollback && response.Rollback != null)
            {
                HandleRollback(response.Rollback);
            }

            if (response.ConfirmedFrame > _confirmedFrame)
            {
                _confirmedFrame = response.ConfirmedFrame;
            }

            return response;
        }

        public async Task<AttackResponse> Attack(ulong attackerEntityId, ulong targetEntityId)
        {
            GetLatestSnapshotInfo(out ulong snapshotFrame, out ulong snapshotHash);

            var request = new AttackRequest
            {
                GameId = _gameId,
                PlayerId = _playerId,
                AttackerEntityId = attackerEntityId,
                TargetEntityId = targetEntityId,
                BaseSnapshotFrame = snapshotFrame,
                BaseSnapshotHash = snapshotHash,
                Sequence = IncrementSequence()
            };

            var response = await _gameClient.AttackAsync(request);

            if (response.NeedsRollback && response.Rollback != null)
            {
                HandleRollback(response.Rollback);
            }

            if (response.ConfirmedFrame > _confirmedFrame)
            {
                _confirmedFrame = response.ConfirmedFrame;
            }

            return response;
        }

        public async Task<EndTurnResponse> EndTurn()
        {
            GetLatestSnapshotInfo(out ulong snapshotFrame, out ulong snapshotHash);

            var request = new EndTurnRequest
            {
                GameId = _gameId,
                PlayerId = _playerId,
                BaseSnapshotFrame = snapshotFrame,
                BaseSnapshotHash = snapshotHash,
                Sequence = IncrementSequence()
            };

            var response = await _gameClient.EndTurnAsync(request);

            if (response.NeedsRollback && response.Rollback != null)
            {
                HandleRollback(response.Rollback);
            }

            if (response.ConfirmedFrame > _confirmedFrame)
            {
                _confirmedFrame = response.ConfirmedFrame;
            }

            return response;
        }

        public async Task<ConcedeResponse> Concede()
        {
            var request = new ConcedeRequest
            {
                GameId = _gameId,
                PlayerId = _playerId
            };
            var response = await _gameClient.ConcedeAsync(request);
            IsInGame = false;
            return response;
        }

        public async Task<GetGameStateResponse> GetGameState()
        {
            var request = new GetGameStateRequest
            {
                GameId = _gameId,
                PlayerId = _playerId
            };
            return await _gameClient.GetGameStateAsync(request);
        }

        public async Task<CancelMatchResponse> CancelMatch()
        {
            var request = new CancelMatchRequest
            {
                PlayerId = _playerId,
                MatchId = _matchId
            };
            return await _matchClient.CancelMatchAsync(request);
        }

        public async Task<GetPlayerStatsResponse> GetPlayerStats(string playerId = null)
        {
            var request = new GetPlayerStatsRequest
            {
                PlayerId = playerId ?? _playerId
            };
            return await _matchClient.GetPlayerStatsAsync(request);
        }

        public void SendAction(Action action)
        {
        }

        private void OnDestroy()
        {
            _channel?.ShutdownAsync().Wait();
        }

        public string PlayerId => _playerId;
        public string PlayerName => _playerName;
        public string GameId => _gameId;
        public string MatchId => _matchId;
    }
}
