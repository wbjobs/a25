using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using UnityEngine;
using UnityEngine.UI;
using CardGame.Proto;
using CardGame.Networking;
using Grpc.Net.Client;

namespace CardGame.UI
{
    public class BalancePanel : MonoBehaviour
    {
        [Header("Filters")]
        public Dropdown TimeRangeDropdown;
        public Dropdown GameTypeDropdown;
        public Dropdown SortByDropdown;
        public Dropdown SortOrderDropdown;
        public InputField MinRankInput;
        public InputField MaxRankInput;
        public Button RefreshBtn;

        [Header("Stats View")]
        public Transform StatsContainer;
        public GameObject CardBalanceRowPrefab;
        public Text TotalSampleSizeText;
        public Text TierDistributionText;
        public ScrollRect StatsScrollRect;
        public int PageSize = 100;

        [Header("Change History View")]
        public Transform ChangeHistoryContainer;
        public GameObject ChangeLogRowPrefab;
        public InputField CardIdHistoryInput;
        public Button LoadHistoryBtn;

        [Header("Hot Update Panel")]
        public InputField HotUpdateCardIdInput;
        public InputField HotUpdateFieldInput;
        public InputField HotUpdateValueInput;
        public InputField HotUpdateReasonInput;
        public InputField HotUpdateAuthorInput;
        public Toggle HotUpdateImmediateToggle;
        public Button ApplyHotUpdateBtn;
        public Button RevertHotUpdateBtn;
        public InputField RevertChangeIdInput;
        public Text HotUpdateResultText;

        [Header("Tier Distribution View")]
        public Image TierSBg; public Text TierSCount;
        public Image TierABg; public Text TierACount;
        public Image TierBBg; public Text TierBCount;
        public Image TierCBg; public Text TierCCount;
        public Image TierDBg; public Text TierDCount;
        public Image TierFBg; public Text TierFCount;

        private GrpcChannel _channel;
        private BalanceService.BalanceServiceClient _balanceClient;
        private GetBalanceStatsResponse _latestStats;
        private readonly Dictionary<string, GameObject> _rowCache = new Dictionary<string, GameObject>();
        private bool _isLoading;

        public event Action<string> OnHotUpdateApplied;
        public event Action<string> OnError;

        private void Awake()
        {
            SetupFiltersDefault();
            BindUIEvents();
        }

        private void SetupFiltersDefault()
        {
            if (TimeRangeDropdown)
            {
                TimeRangeDropdown.ClearOptions();
                TimeRangeDropdown.AddOptions(new List<string> { "24小时", "7天", "30天", "90天" });
                TimeRangeDropdown.value = 1;
            }
            if (GameTypeDropdown)
            {
                GameTypeDropdown.ClearOptions();
                GameTypeDropdown.AddOptions(new List<string> { "全部", "排位赛", "休闲模式" });
            }
            if (SortByDropdown)
            {
                SortByDropdown.ClearOptions();
                SortByDropdown.AddOptions(new List<string> { "胜率", "出场率", "禁用率", "综合评分", "使用率变化" });
            }
            if (SortOrderDropdown)
            {
                SortOrderDropdown.ClearOptions();
                SortOrderDropdown.AddOptions(new List<string> { "降序", "升序" });
            }
        }

        private void BindUIEvents()
        {
            if (RefreshBtn) RefreshBtn.onClick.AddListener(() => { _ = LoadBalanceStats(); });
            if (TimeRangeDropdown) TimeRangeDropdown.onValueChanged.AddListener(_ => { _ = LoadBalanceStats(); });
            if (GameTypeDropdown) GameTypeDropdown.onValueChanged.AddListener(_ => { _ = LoadBalanceStats(); });
            if (SortByDropdown) SortByDropdown.onValueChanged.AddListener(_ => { _ = LoadBalanceStats(); });
            if (SortOrderDropdown) SortOrderDropdown.onValueChanged.AddListener(_ => { _ = LoadBalanceStats(); });
            if (LoadHistoryBtn) LoadHistoryBtn.onClick.AddListener(() => { _ = LoadChangeHistory(); });
            if (ApplyHotUpdateBtn) ApplyHotUpdateBtn.onClick.AddListener(() => { _ = ApplyHotUpdate(); });
            if (RevertHotUpdateBtn) RevertHotUpdateBtn.onClick.AddListener(() => { _ = RevertHotUpdate(); });
        }

        public void Initialize(string serverAddress)
        {
            _channel = GrpcChannel.ForAddress($"http://{serverAddress}");
            _balanceClient = new BalanceService.BalanceServiceClient(_channel);
        }

        public async Task LoadBalanceStats()
        {
            if (_isLoading) return;
            _isLoading = true;

            try
            {
                int timeRange = 7;
                if (TimeRangeDropdown)
                {
                    int[] ranges = { 1, 7, 30, 90 };
                    timeRange = ranges[Math.Clamp(TimeRangeDropdown.value, 0, ranges.Length - 1)];
                }

                string gameType = "";
                if (GameTypeDropdown)
                {
                    string[] types = { "", "ranked", "normal" };
                    gameType = types[Math.Clamp(GameTypeDropdown.value, 0, types.Length - 1)];
                }

                string sortBy = "win_rate";
                if (SortByDropdown)
                {
                    string[] sorts = { "win_rate", "play_rate", "ban_rate", "tier", "delta_win_rate_7d" };
                    sortBy = sorts[Math.Clamp(SortByDropdown.value, 0, sorts.Length - 1)];
                }

                string sortOrder = "desc";
                if (SortOrderDropdown)
                {
                    sortOrder = SortOrderDropdown.value == 0 ? "desc" : "asc";
                }

                int minRank = 0, maxRank = 99999;
                if (!string.IsNullOrEmpty(MinRankInput?.text)) int.TryParse(MinRankInput.text, out minRank);
                if (!string.IsNullOrEmpty(MaxRankInput?.text)) int.TryParse(MaxRankInput.text, out maxRank);

                var req = new GetBalanceStatsRequest
                {
                    TimeRangeDays = (int)timeRange,
                    GameType = gameType,
                    SortBy = sortBy,
                    SortOrder = sortOrder,
                    MinRank = minRank,
                    MaxRank = maxRank,
                    Page = 1,
                    PageSize = PageSize
                };

                _latestStats = await _balanceClient.GetBalanceStatsAsync(req);
                RenderStats(_latestStats);
                _ = LoadTierDistribution(timeRange);
            }
            catch (Exception e)
            {
                OnError?.Invoke(e.Message);
                Debug.LogError($"LoadBalanceStats failed: {e.Message}");
            }
            finally
            {
                _isLoading = false;
            }
        }

        private void RenderStats(GetBalanceStatsResponse resp)
        {
            if (resp == null) return;

            if (TotalSampleSizeText)
            {
                TotalSampleSizeText.text = $"样本量: {resp.SampleSizeTotal:N0} 局";
            }

            if (TierDistributionText && resp.TierDistribution.Count > 0)
            {
                TierDistributionText.text = string.Join(" | ", resp.TierDistribution);
            }

            ClearStatsRows();
            int rank = 0;
            foreach (var stat in resp.Stats)
            {
                rank++;
                CreateOrUpdateRow(rank, stat);
            }
        }

        private void CreateOrUpdateRow(int rank, CardBalanceStats stat)
        {
            if (!StatsContainer || !CardBalanceRowPrefab) return;

            if (!_rowCache.TryGetValue(stat.TemplateId, out var row))
            {
                row = Instantiate(CardBalanceRowPrefab, StatsContainer);
                _rowCache[stat.TemplateId] = row;
            }

            row.name = $"Row_{stat.TemplateId}";

            var texts = row.GetComponentsInChildren<Text>();
            if (texts.Length >= 10)
            {
                texts[0].text = $"#{rank}";
                texts[1].text = stat.CardName;
                texts[2].text = stat.CardType;
                texts[3].text = $"{stat.CardCost}费";
                texts[4].text = $"{stat.WinRate * 100:F2}%";
                texts[5].text = $"{stat.PlayRate * 100:F2}%";
                texts[6].text = $"{stat.BanRate * 100:F2}%";
                texts[7].text = stat.Tier.ToString().Replace("CARD_BALANCE_TIER_", "");
                texts[8].text = $"{stat.DeltaWinRate7D * 100:+0.##;-0.##;0.##}%";
                texts[9].text = $"n={stat.SampleSize:N0}";
            }

            var imgs = row.GetComponentsInChildren<Image>();
            Color tierColor = GetTierColor(stat.Tier);
            if (imgs.Length > 0)
            {
                imgs[0].color = new Color(tierColor.r, tierColor.g, tierColor.b, 0.15f);
            }
        }

        private static Color GetTierColor(CardBalanceTier tier)
        {
            switch (tier)
            {
                case CardBalanceTier.S: return new Color(1f, 0.84f, 0f);
                case CardBalanceTier.A: return new Color(0.2f, 0.8f, 0.4f);
                case CardBalanceTier.B: return new Color(0.2f, 0.6f, 1f);
                case CardBalanceTier.C: return new Color(0.6f, 0.6f, 0.6f);
                case CardBalanceTier.D: return new Color(0.9f, 0.5f, 0.2f);
                case CardBalanceTier.F: return new Color(0.9f, 0.2f, 0.2f);
                default: return Color.white;
            }
        }

        private void ClearStatsRows()
        {
            _rowCache.Clear();
            if (!StatsContainer) return;
            for (int i = StatsContainer.childCount - 1; i >= 0; i--)
            {
                Destroy(StatsContainer.GetChild(i).gameObject);
            }
        }

        public async Task LoadTierDistribution(int timeRangeDays)
        {
            try
            {
                var resp = await _balanceClient.GetTierDistributionAsync(new GetTierDistributionRequest
                {
                    TimeRangeDays = (int)timeRangeDays
                });
                if (resp == null) return;

                foreach (var td in resp.Tiers)
                {
                    string tierName = td.Tier.ToString().Replace("CARD_BALANCE_TIER_", "");
                    switch (td.Tier)
                    {
                        case CardBalanceTier.S: if (TierSCount) TierSCount.text = $"S: {td.Count} ({td.Percentage * 100:F1}%)"; if (TierSBg) TierSBg.color = GetTierColor(td.Tier); break;
                        case CardBalanceTier.A: if (TierACount) TierACount.text = $"A: {td.Count} ({td.Percentage * 100:F1}%)"; if (TierABg) TierABg.color = GetTierColor(td.Tier); break;
                        case CardBalanceTier.B: if (TierBCount) TierBCount.text = $"B: {td.Count} ({td.Percentage * 100:F1}%)"; if (TierBBg) TierBBg.color = GetTierColor(td.Tier); break;
                        case CardBalanceTier.C: if (TierCCount) TierCCount.text = $"C: {td.Count} ({td.Percentage * 100:F1}%)"; if (TierCBg) TierCBg.color = GetTierColor(td.Tier); break;
                        case CardBalanceTier.D: if (TierDCount) TierDCount.text = $"D: {td.Count} ({td.Percentage * 100:F1}%)"; if (TierDBg) TierDBg.color = GetTierColor(td.Tier); break;
                        case CardBalanceTier.F: if (TierFCount) TierFCount.text = $"F: {td.Count} ({td.Percentage * 100:F1}%)"; if (TierFBg) TierFBg.color = GetTierColor(td.Tier); break;
                    }
                }
            }
            catch (Exception e)
            {
                Debug.LogWarning($"LoadTierDistribution failed: {e.Message}");
            }
        }

        public async Task LoadChangeHistory()
        {
            if (!_balanceClient || string.IsNullOrEmpty(CardIdHistoryInput?.text)) return;
            try
            {
                var resp = await _balanceClient.GetBalanceHistoryAsync(new GetBalanceHistoryRequest
                {
                    TemplateId = CardIdHistoryInput.text,
                    Limit = 50
                });
                if (ChangeHistoryContainer)
                {
                    for (int i = ChangeHistoryContainer.childCount - 1; i >= 0; i--)
                        Destroy(ChangeHistoryContainer.GetChild(i).gameObject);
                }
                foreach (var change in resp.Changes)
                {
                    if (ChangeLogRowPrefab && ChangeHistoryContainer)
                    {
                        var row = Instantiate(ChangeLogRowPrefab, ChangeHistoryContainer);
                        var texts = row.GetComponentsInChildren<Text>();
                        if (texts.Length >= 5)
                        {
                            texts[0].text = DateTimeOffset.FromUnixTimeMilliseconds(change.TimestampUnixMs).LocalDateTime.ToString("yyyy-MM-dd HH:mm");
                            texts[1].text = change.ChangedBy;
                            texts[2].text = change.ChangeReason;
                            texts[3].text = $"预测: {change.PredictedImpact:+0.##;-0.##;0}";
                            texts[4].text = $"实际: {change.ActualImpact24H:+0.##;-0.##;0}";
                        }
                    }
                }
            }
            catch (Exception e)
            {
                OnError?.Invoke(e.Message);
            }
        }

        public async Task ApplyHotUpdate()
        {
            if (!_balanceClient || string.IsNullOrEmpty(HotUpdateCardIdInput?.text) ||
                string.IsNullOrEmpty(HotUpdateFieldInput?.text) ||
                string.IsNullOrEmpty(HotUpdateValueInput?.text))
            {
                ShowHotUpdateResult("请完整填写卡牌ID, 字段名, 新值", false);
                return;
            }
            try
            {
                var overrides = new Google.Protobuf.Collections.MapField<string, double>();
                if (double.TryParse(HotUpdateValueInput.text, System.Globalization.NumberStyles.Any, System.Globalization.CultureInfo.InvariantCulture, out double val))
                {
                    overrides[HotUpdateFieldInput.text] = val;
                }

                var resp = await _balanceClient.HotUpdateCardAsync(new HotUpdateRequest
                {
                    TemplateId = HotUpdateCardIdInput.text,
                    NumericOverrides = { overrides },
                    ChangeReason = HotUpdateReasonInput?.text ?? "",
                    ChangedBy = HotUpdateAuthorInput?.text ?? "system",
                    Immediate = HotUpdateImmediateToggle?.isOn ?? false
                });

                ShowHotUpdateResult(resp.Success
                    ? $"热更新成功! change_id={resp.ChangeId}, 生效时间:{DateTimeOffset.FromUnixTimeMilliseconds(resp.EffectiveTimeUnixMs):yyyy-MM-dd HH:mm}"
                    : "热更新失败", resp.Success);

                if (resp.Success)
                {
                    OnHotUpdateApplied?.Invoke(resp.ChangeId);
                    _ = LoadBalanceStats();
                }
            }
            catch (Exception e)
            {
                ShowHotUpdateResult($"错误: {e.Message}", false);
            }
        }

        public async Task RevertHotUpdate()
        {
            if (!_balanceClient || string.IsNullOrEmpty(RevertChangeIdInput?.text)) return;
            try
            {
                var resp = await _balanceClient.RevertHotUpdateAsync(new RevertHotUpdateRequest
                {
                    ChangeId = RevertChangeIdInput.text,
                    RevertedBy = HotUpdateAuthorInput?.text ?? "system"
                });
                ShowHotUpdateResult(resp.Success
                    ? $"回滚成功! 新change_id={resp.RevertChangeId}"
                    : "回滚失败", resp.Success);
                if (resp.Success) _ = LoadBalanceStats();
            }
            catch (Exception e)
            {
                ShowHotUpdateResult($"回滚错误: {e.Message}", false);
            }
        }

        private void ShowHotUpdateResult(string msg, bool success)
        {
            if (!HotUpdateResultText) return;
            HotUpdateResultText.text = msg;
            HotUpdateResultText.color = success ? Color.green : Color.red;
        }

        private void OnDestroy()
        {
            _channel?.ShutdownAsync().Wait(500);
        }
    }
}
