using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using UnityEngine;
using UnityEngine.UI;
using CardGame.Proto;
using CardGame.Networking;
using Grpc.Net.Client;

namespace CardGame.Replay
{
    public class ReplayPlayer : MonoBehaviour
    {
        [Header("UI References")]
        public Slider TimeSlider;
        public Text CurrentTimeText;
        public Text TotalTimeText;
        public Text SpectatorCountText;
        public Button PlayPauseBtn;
        public Button RewindBtn;
        public Button ForwardBtn;
        public Button SpeedBtn;
        public Text SpeedBtnText;
        public Image PlayPauseIcon;
        public Sprite PlaySprite;
        public Sprite PauseSprite;

        [Header("Settings")]
        public float[] AvailableSpeeds = new float[] { 0.25f, 0.5f, 1f, 2f, 4f, 8f };
        public float AutoUpdateIntervalMs = 500f;

        private string _gameId;
        private string _spectatorId;
        private GrpcChannel _channel;
        private ReplayService.ReplayServiceClient _replayClient;
        private bool _isPlaying;
        private bool _isPaused;
        private int _currentSpeedIndex = 2;
        private float _currentSpeed = 1f;
        private long _currentStreamTimeMs;
        private long _totalDurationMs;
        private int _spectatorCount;
        private ReplayMeta _currentMeta;
        private bool _isLive;
        private GameStateManager _stateManager;

        private readonly List<ReplayAction> _actionBuffer = new List<ReplayAction>();
        private int _nextActionIndex;
        private float _accumulatorMs;

        public event Action<ReplayAction> OnActionReplayed;
        public event Action<bool> OnPlayStateChanged;
        public event Action<long> OnTimeChanged;
        public event Action<int> OnSpectatorCountChanged;
        public event Action<string> OnError;

        public bool IsPlaying => _isPlaying;
        public bool IsPaused => _isPaused;
        public float CurrentSpeed => _currentSpeed;
        public long CurrentTimeMs => _currentStreamTimeMs;
        public long TotalDurationMs => _totalDurationMs;
        public int SpectatorCount => _spectatorCount;
        public bool IsLive => _isLive;

        private void Awake()
        {
            _stateManager = GameStateManager.Instance;
            SetupUIListeners();
        }

        private void SetupUIListeners()
        {
            if (PlayPauseBtn) PlayPauseBtn.onClick.AddListener(TogglePlayPause);
            if (RewindBtn) RewindBtn.onClick.AddListener(() => SkipBySeconds(-10));
            if (ForwardBtn) ForwardBtn.onClick.AddListener(() => SkipBySeconds(10));
            if (SpeedBtn) SpeedBtn.onClick.AddListener(CycleSpeed);
            if (TimeSlider) TimeSlider.onValueChanged.AddListener(OnSliderValueChanged);
        }

        public async Task ConnectAndJoin(string serverAddress, string gameId, string spectatorId, string spectatorName)
        {
            try
            {
                _gameId = gameId;
                _spectatorId = spectatorId;
                _channel = GrpcChannel.ForAddress($"http://{serverAddress}");
                _replayClient = new ReplayService.ReplayServiceClient(_channel);

                var joinReq = new SpectatorJoinRequest
                {
                    GameId = gameId,
                    SpectatorId = spectatorId,
                    SpectatorName = spectatorName
                };

                var call = _replayClient.JoinAsSpectator(joinReq);
                var responseStream = call.ResponseStream;

                _ = Task.Run(async () =>
                {
                    await foreach (var frame in responseStream.ReadAllAsync())
                    {
                        UnityMainThreadDispatcher.Enqueue(() => HandleSpectatorFrame(frame));
                    }
                });

                var metaReq = new GetReplayMetaRequest { GameId = gameId };
                _currentMeta = await _replayClient.GetReplayMetaAsync(metaReq);
                _totalDurationMs = _currentMeta.DurationMs;
                _isLive = _currentMeta.IsLive;
                UpdateTimeDisplay();
            }
            catch (Exception e)
            {
                OnError?.Invoke(e.Message);
                Debug.LogError($"Failed to join spectator: {e.Message}");
            }
        }

        private void HandleSpectatorFrame(SpectatorFrame frame)
        {
            if (frame.SpectatorCount > 0)
            {
                _spectatorCount = frame.SpectatorCount;
                OnSpectatorCountChanged?.Invoke(_spectatorCount);
                if (SpectatorCountText) SpectatorCountText.text = $"👥 {_spectatorCount}";
            }

            switch (frame.PayloadCase)
            {
                case SpectatorFrame.PayloadOneofCase.FullSnapshot:
                    ApplySnapshot(frame.FullSnapshot);
                    break;
                case SpectatorFrame.PayloadOneofCase.Action:
                    _actionBuffer.Add(frame.Action);
                    _totalDurationMs = Math.Max(_totalDurationMs, frame.StreamTimeMs);
                    break;
                case SpectatorFrame.PayloadOneofCase.Segment:
                    _actionBuffer.AddRange(frame.Segment.Actions);
                    break;
            }

            _currentStreamTimeMs = frame.StreamTimeMs;
            OnTimeChanged?.Invoke(_currentStreamTimeMs);
            UpdateTimeDisplay();
        }

        private void ApplySnapshot(GameSnapshot snapshot)
        {
            if (snapshot?.Status != null)
            {
                _stateManager.ApplyState(snapshot.Status);
            }
            _currentStreamTimeMs = snapshot?.Status?.Timestamp ?? _currentStreamTimeMs;
        }

        private void Update()
        {
            if (!_isPlaying || _isPaused) return;

            _accumulatorMs += Time.deltaTime * 1000f * _currentSpeed;

            while (_accumulatorMs >= 16f && _nextActionIndex < _actionBuffer.Count)
            {
                var action = _actionBuffer[_nextActionIndex];
                if (action.RelativeTimeMs <= _currentStreamTimeMs + _accumulatorMs)
                {
                    ReplayAction(action);
                    _nextActionIndex++;
                    _accumulatorMs -= 16f;
                }
                else
                {
                    break;
                }
            }

            _currentStreamTimeMs += (long)(Time.deltaTime * 1000f * _currentSpeed);
            _currentStreamTimeMs = Math.Min(_currentStreamTimeMs, _totalDurationMs);

            OnTimeChanged?.Invoke(_currentStreamTimeMs);
            UpdateSliderFromTime();
        }

        private void ReplayAction(ReplayAction replayAction)
        {
            OnActionReplayed?.Invoke(replayAction);
            if (replayAction.StateAfter != null)
            {
                _stateManager.ApplyState(replayAction.StateAfter);
            }
        }

        public void TogglePlayPause()
        {
            if (!_isPlaying)
            {
                StartPlayback();
            }
            else
            {
                _isPaused = !_isPaused;
                UpdatePlayPauseIcon();
                OnPlayStateChanged?.Invoke(!_isPaused);
            }
        }

        public void StartPlayback()
        {
            _isPlaying = true;
            _isPaused = false;
            UpdatePlayPauseIcon();
            OnPlayStateChanged?.Invoke(true);
        }

        public void Pause()
        {
            _isPaused = true;
            UpdatePlayPauseIcon();
            OnPlayStateChanged?.Invoke(false);
        }

        public async void SeekToTime(long targetMs)
        {
            try
            {
                Pause();
                var resp = await _replayClient.SeekToAsync(new SeekRequest
                {
                    GameId = _gameId,
                    TargetTimeMs = targetMs
                });
                if (resp.Success && resp.Snapshot != null)
                {
                    ApplySnapshot(resp.Snapshot);
                    _currentStreamTimeMs = resp.CurrentTimeMs;
                    UpdateTimeDisplay();
                }
            }
            catch (Exception e)
            {
                OnError?.Invoke(e.Message);
            }
        }

        public void SkipBySeconds(int seconds)
        {
            long targetMs = _currentStreamTimeMs + seconds * 1000;
            targetMs = Math.Max(0, Math.Min(targetMs, _totalDurationMs));
            SeekToTime(targetMs);
        }

        public async void CycleSpeed()
        {
            _currentSpeedIndex = (_currentSpeedIndex + 1) % AvailableSpeeds.Length;
            _currentSpeed = AvailableSpeeds[_currentSpeedIndex];
            if (SpeedBtnText) SpeedBtnText.text = $"{_currentSpeed:0.##}x";

            try
            {
                await _replayClient.SetPlaybackSpeedAsync(new SetPlaybackSpeedRequest
                {
                    SpectatorId = _spectatorId,
                    GameId = _gameId,
                    Speed = _currentSpeed
                });
            }
            catch { }
        }

        private void OnSliderValueChanged(float value)
        {
            if (_totalDurationMs <= 0) return;
            long targetMs = (long)(value * _totalDurationMs);
            if (Math.Abs(targetMs - _currentStreamTimeMs) > 500)
            {
                SeekToTime(targetMs);
            }
        }

        private void UpdateSliderFromTime()
        {
            if (!TimeSlider || _totalDurationMs <= 0) return;
            float val = (float)_currentStreamTimeMs / _totalDurationMs;
            if (Math.Abs(val - TimeSlider.value) > 0.01f)
            {
                TimeSlider.SetValueWithoutNotify(val);
            }
        }

        private void UpdateTimeDisplay()
        {
            if (CurrentTimeText) CurrentTimeText.text = FormatTimeMs(_currentStreamTimeMs);
            if (TotalTimeText)
            {
                string suffix = _isLive ? " LIVE" : "";
                TotalTimeText.text = FormatTimeMs(_totalDurationMs) + suffix;
            }
            UpdateSliderFromTime();
        }

        private void UpdatePlayPauseIcon()
        {
            if (!PlayPauseIcon) return;
            PlayPauseIcon.sprite = _isPaused ? PlaySprite : PauseSprite;
        }

        private static string FormatTimeMs(long ms)
        {
            long totalSec = ms / 1000;
            long min = totalSec / 60;
            long sec = totalSec % 60;
            return $"{min:00}:{sec:00}";
        }

        private void OnDestroy()
        {
            _channel?.ShutdownAsync().Wait(500);
        }
    }
}
