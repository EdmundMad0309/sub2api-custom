package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

const accountOpsFirstTokenConfigKey = "account_ops_first_token_config_v1"

// FirstTokenMonitorConfig 「首字监控」配置：对指定分组里的每个账号，统计最近
// 窗口内成功请求的平均首字延迟；平均 ≤ OpenThresholdMS 自动打开调度，
// > CloseThresholdMS 自动关闭调度，中间为滞回区不动。
type FirstTokenMonitorConfig struct {
	Enabled          bool    `json:"enabled"`
	GroupIDs         []int64 `json:"group_ids"`
	WindowMinutes    int     `json:"window_minutes"`
	MinSamples       int     `json:"min_samples"`
	OpenThresholdMS  int     `json:"open_threshold_ms"`
	CloseThresholdMS int     `json:"close_threshold_ms"`
	IntervalSeconds  int     `json:"interval_seconds"`
}

func defaultFirstTokenMonitorConfig() FirstTokenMonitorConfig {
	return FirstTokenMonitorConfig{
		Enabled:          false,
		GroupIDs:         []int64{},
		WindowMinutes:    30,
		MinSamples:       3,
		OpenThresholdMS:  7000,
		CloseThresholdMS: 10000,
		IntervalSeconds:  120,
	}
}

// ValidateFirstTokenMonitorConfig 校验并规范化配置。
func ValidateFirstTokenMonitorConfig(cfg *FirstTokenMonitorConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if len(cfg.GroupIDs) > 100 {
		return fmt.Errorf("group_ids supports at most 100 groups")
	}
	seen := map[int64]bool{}
	groups := make([]int64, 0, len(cfg.GroupIDs))
	for _, id := range cfg.GroupIDs {
		if id <= 0 {
			return fmt.Errorf("group_ids must be positive")
		}
		if !seen[id] {
			seen[id] = true
			groups = append(groups, id)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i] < groups[j] })
	cfg.GroupIDs = groups
	if cfg.WindowMinutes < 1 || cfg.WindowMinutes > 24*60 {
		return fmt.Errorf("window_minutes must be within 1..1440")
	}
	if cfg.MinSamples < 1 || cfg.MinSamples > 200 {
		return fmt.Errorf("min_samples must be within 1..200")
	}
	if cfg.OpenThresholdMS < 100 || cfg.OpenThresholdMS > 600000 {
		return fmt.Errorf("open_threshold_ms must be within 100..600000")
	}
	if cfg.CloseThresholdMS <= cfg.OpenThresholdMS || cfg.CloseThresholdMS > 600000 {
		return fmt.Errorf("close_threshold_ms must be greater than open_threshold_ms and within 600000")
	}
	if cfg.IntervalSeconds < 30 || cfg.IntervalSeconds > 3600 {
		return fmt.Errorf("interval_seconds must be within 30..3600")
	}
	return nil
}

// FirstTokenAccountSample 单个账号在窗口内的首字统计。
type FirstTokenAccountSample struct {
	AccountID   int64   `json:"account_id"`
	AccountName string  `json:"account_name"`
	Status      string  `json:"status"`
	Schedulable bool    `json:"schedulable"`
	AverageMS   float64 `json:"average_ms"`
	Samples     int     `json:"samples"`
	GroupIDs    []int64 `json:"group_ids"`
}

// FirstTokenMonitorEvent 一次自动开/关调度的动作记录。
type FirstTokenMonitorEvent struct {
	At        time.Time `json:"at"`
	AccountID int64     `json:"account_id"`
	Name      string    `json:"account_name"`
	Action    string    `json:"action"`
	AverageMS float64   `json:"average_ms"`
	Samples   int       `json:"samples"`
}

// FirstTokenMonitorRepository 首字监控所需的最小仓储能力。
type FirstTokenMonitorRepository interface {
	ListFirstTokenSamples(ctx context.Context, groupIDs []int64, since time.Time) ([]FirstTokenAccountSample, error)
	SetAccountSchedulable(ctx context.Context, accountID int64, schedulable bool) error
}

// FirstTokenMonitorService 定时统计首字延迟并按阈值自动开关账号调度。
type FirstTokenMonitorService struct {
	settings SettingRepository
	repo     FirstTokenMonitorRepository

	mu        sync.RWMutex
	config    FirstTokenMonitorConfig
	lastRunAt time.Time
	lastError string
	events    []FirstTokenMonitorEvent

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
}

func NewFirstTokenMonitorService(settings SettingRepository, repo FirstTokenMonitorRepository) *FirstTokenMonitorService {
	svc := &FirstTokenMonitorService{
		settings: settings,
		repo:     repo,
		config:   defaultFirstTokenMonitorConfig(),
		events:   make([]FirstTokenMonitorEvent, 0, 32),
		stopCh:   make(chan struct{}),
	}
	svc.loadConfig(context.Background())
	return svc
}

func (s *FirstTokenMonitorService) loadConfig(ctx context.Context) {
	if s == nil || s.settings == nil {
		return
	}
	raw, err := s.settings.GetValue(ctx, accountOpsFirstTokenConfigKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return
	}
	cfg := defaultFirstTokenMonitorConfig()
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("first_token_monitor_config_decode_failed", "error", err)
		return
	}
	if err := ValidateFirstTokenMonitorConfig(&cfg); err != nil {
		slog.Warn("first_token_monitor_config_invalid", "error", err)
		return
	}
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
}

// GetConfig 返回当前配置副本。
func (s *FirstTokenMonitorService) GetConfig() FirstTokenMonitorConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// SaveConfig 校验并持久化配置。
func (s *FirstTokenMonitorService) SaveConfig(ctx context.Context, cfg FirstTokenMonitorConfig) error {
	if err := ValidateFirstTokenMonitorConfig(&cfg); err != nil {
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if s.settings == nil {
		return fmt.Errorf("settings repository unavailable")
	}
	if err := s.settings.Set(ctx, accountOpsFirstTokenConfigKey, string(raw)); err != nil {
		return err
	}
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
	return nil
}

// FirstTokenMonitorStatus 页面所需的运行状态。
type FirstTokenMonitorStatus struct {
	Config    FirstTokenMonitorConfig   `json:"config"`
	Running   bool                      `json:"running"`
	LastRunAt *time.Time                `json:"last_run_at,omitempty"`
	LastError string                    `json:"last_error,omitempty"`
	Events    []FirstTokenMonitorEvent  `json:"events"`
	Samples   []FirstTokenAccountSample `json:"samples"`
}

// Status 返回当前状态；samples 仅在需要时实时查询。
func (s *FirstTokenMonitorService) Status(ctx context.Context, includeSamples bool) (FirstTokenMonitorStatus, error) {
	cfg := s.GetConfig()
	s.mu.RLock()
	status := FirstTokenMonitorStatus{
		Config:    cfg,
		Running:   cfg.Enabled,
		LastError: s.lastError,
		Events:    append([]FirstTokenMonitorEvent{}, s.events...),
	}
	if !s.lastRunAt.IsZero() {
		at := s.lastRunAt
		status.LastRunAt = &at
	}
	s.mu.RUnlock()
	if includeSamples && len(cfg.GroupIDs) > 0 {
		since := time.Now().Add(-time.Duration(cfg.WindowMinutes) * time.Minute)
		samples, err := s.repo.ListFirstTokenSamples(ctx, cfg.GroupIDs, since)
		if err != nil {
			return status, err
		}
		status.Samples = samples
	}
	return status, nil
}

// RunOnce 执行一轮统计与自动开关调度，返回本轮动作。
func (s *FirstTokenMonitorService) RunOnce(ctx context.Context) ([]FirstTokenMonitorEvent, []FirstTokenAccountSample, error) {
	cfg := s.GetConfig()
	if len(cfg.GroupIDs) == 0 {
		return nil, nil, nil
	}
	since := time.Now().Add(-time.Duration(cfg.WindowMinutes) * time.Minute)
	samples, err := s.repo.ListFirstTokenSamples(ctx, cfg.GroupIDs, since)
	if err != nil {
		s.setLastError(err)
		return nil, nil, err
	}
	open := float64(cfg.OpenThresholdMS)
	closeMS := float64(cfg.CloseThresholdMS)
	events := make([]FirstTokenMonitorEvent, 0, 4)
	for _, sample := range samples {
		if sample.Samples < cfg.MinSamples || sample.AverageMS <= 0 {
			continue
		}
		switch {
		case sample.AverageMS <= open && !sample.Schedulable:
			if err := s.repo.SetAccountSchedulable(ctx, sample.AccountID, true); err != nil {
				slog.Warn("first_token_monitor_enable_failed", "account_id", sample.AccountID, "error", err)
				continue
			}
			events = append(events, FirstTokenMonitorEvent{
				At: time.Now(), AccountID: sample.AccountID, Name: sample.AccountName,
				Action: "scheduling_enabled", AverageMS: sample.AverageMS, Samples: sample.Samples,
			})
		case sample.AverageMS > closeMS && sample.Schedulable:
			if err := s.repo.SetAccountSchedulable(ctx, sample.AccountID, false); err != nil {
				slog.Warn("first_token_monitor_disable_failed", "account_id", sample.AccountID, "error", err)
				continue
			}
			events = append(events, FirstTokenMonitorEvent{
				At: time.Now(), AccountID: sample.AccountID, Name: sample.AccountName,
				Action: "scheduling_disabled", AverageMS: sample.AverageMS, Samples: sample.Samples,
			})
		}
	}
	s.mu.Lock()
	s.lastRunAt = time.Now()
	s.lastError = ""
	if len(events) > 0 {
		merged := append(events, s.events...)
		if len(merged) > 50 {
			merged = merged[:50]
		}
		s.events = merged
	}
	s.mu.Unlock()
	return events, samples, nil
}

func (s *FirstTokenMonitorService) setLastError(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.lastRunAt = time.Now()
	s.mu.Unlock()
}

// Start 启动后台扫描循环（按配置间隔）。
func (s *FirstTokenMonitorService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.loop()
	})
}

func (s *FirstTokenMonitorService) loop() {
	for {
		cfg := s.GetConfig()
		interval := time.Duration(cfg.IntervalSeconds) * time.Second
		if interval < 30*time.Second {
			interval = 30 * time.Second
		}
		select {
		case <-s.stopCh:
			return
		case <-time.After(interval):
		}
		if !s.GetConfig().Enabled {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if _, _, err := s.RunOnce(ctx); err != nil {
			slog.Warn("first_token_monitor_cycle_failed", "error", err)
		}
		cancel()
	}
}

// Stop 停止后台循环。
func (s *FirstTokenMonitorService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// ProvideFirstTokenMonitorService 创建并启动首字监控后台服务。
func ProvideFirstTokenMonitorService(settings SettingRepository, repo FirstTokenMonitorRepository) *FirstTokenMonitorService {
	svc := NewFirstTokenMonitorService(settings, repo)
	svc.Start()
	return svc
}
