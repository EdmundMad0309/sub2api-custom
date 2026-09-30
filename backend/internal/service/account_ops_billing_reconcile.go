package service

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// BillingReconcileRow 单账号在某个自然日的对账行：
// 平台侧记录的成本/扣费，以及上游余额快照与据此推算的上游扣款。
type BillingReconcileRow struct {
	AccountID        int64      `json:"account_id"`
	AccountName      string     `json:"account_name"`
	Status           string     `json:"status"`
	Requests         int64      `json:"requests"`
	TotalTokens      int64      `json:"total_tokens"`
	PlatformCost     float64    `json:"platform_cost"`
	PlatformCharged  float64    `json:"platform_charged"`
	UpstreamBalance  *float64   `json:"upstream_balance,omitempty"`
	UpstreamCurrency string     `json:"upstream_currency,omitempty"`
	UpstreamProbedAt *time.Time `json:"upstream_probed_at,omitempty"`
	// UpstreamSpent 为相邻两日余额差值（前一日余额 - 当日余额），只有两日快照
	// 都存在时才有值；正数表示上游在该日扣了钱。
	UpstreamSpent *float64 `json:"upstream_spent,omitempty"`
	// Difference 为「上游扣款 - 平台成本」，正数表示上游扣得比平台记录的更多。
	Difference *float64 `json:"difference,omitempty"`
}

// BillingReconcileRepository 账单对账所需的最小仓储能力。
type BillingReconcileRepository interface {
	CollectBillingReconcileRows(ctx context.Context, day time.Time) ([]BillingReconcileRow, error)
	UpsertBillingDailyStats(ctx context.Context, day time.Time, rows []BillingReconcileRow) error
	ListBillingDailyStats(ctx context.Context, day time.Time) ([]BillingReconcileRow, error)
}

// BillingReconcileService 采集与展示「平台记录成本 vs 上游扣款」。
type BillingReconcileService struct {
	repo BillingReconcileRepository

	mu        sync.RWMutex
	lastRunAt time.Time
	lastDay   string
	lastError string

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
}

func NewBillingReconcileService(repo BillingReconcileRepository) *BillingReconcileService {
	return &BillingReconcileService{repo: repo, stopCh: make(chan struct{})}
}

// ProvideBillingReconcileService 创建并启动每日对账快照任务。
func ProvideBillingReconcileService(repo BillingReconcileRepository) *BillingReconcileService {
	svc := NewBillingReconcileService(repo)
	svc.Start()
	return svc
}

// Start 启动每日快照循环（每 30 分钟检查一次，若昨日的快照尚未生成则补齐）。
func (s *BillingReconcileService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() { go s.loop() })
}

func (s *BillingReconcileService) loop() {
	// 启动后先等 1 分钟，再每 30 分钟跑一次。
	select {
	case <-s.stopCh:
		return
	case <-time.After(time.Minute):
	}
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if _, err := s.RunOnce(ctx, time.Time{}); err != nil {
			slog.Warn("billing_reconcile_cycle_failed", "error", err)
		}
		cancel()
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
		}
	}
}

// Stop 停止后台循环。
func (s *BillingReconcileService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// ResolveDay 把入参规范化为目标统计日：空值表示昨天（服务器时区）。
func ResolveBillingReconcileDay(day time.Time) time.Time {
	if day.IsZero() {
		day = time.Now().AddDate(0, 0, -1)
	}
	y, m, d := day.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, day.Location())
}

// RunOnce 采集指定日（默认昨天）的平台成本与上游余额快照并落库。
func (s *BillingReconcileService) RunOnce(ctx context.Context, day time.Time) ([]BillingReconcileRow, error) {
	target := ResolveBillingReconcileDay(day)
	rows, err := s.repo.CollectBillingReconcileRows(ctx, target)
	if err != nil {
		s.setLastError(err)
		return nil, err
	}
	if err := s.repo.UpsertBillingDailyStats(ctx, target, rows); err != nil {
		s.setLastError(err)
		return nil, err
	}
	out, err := s.repo.ListBillingDailyStats(ctx, target)
	if err != nil {
		s.setLastError(err)
		return nil, err
	}
	s.mu.Lock()
	s.lastRunAt = time.Now()
	s.lastDay = target.Format("2006-01-02")
	s.lastError = ""
	s.mu.Unlock()
	return out, nil
}

// Status 读取指定日的对账结果（默认昨天）。
func (s *BillingReconcileService) Status(ctx context.Context, day time.Time) (BillingReconcileStatus, error) {
	target := ResolveBillingReconcileDay(day)
	rows, err := s.repo.ListBillingDailyStats(ctx, target)
	if err != nil {
		return BillingReconcileStatus{}, err
	}
	s.mu.RLock()
	status := BillingReconcileStatus{
		Day:       target.Format("2006-01-02"),
		Rows:      rows,
		LastError: s.lastError,
	}
	if !s.lastRunAt.IsZero() {
		at := s.lastRunAt
		status.LastRunAt = &at
	}
	s.mu.RUnlock()
	var totalPlatform, totalCharged float64
	var totalSpent float64
	var spentKnown int
	for _, row := range rows {
		totalPlatform += row.PlatformCost
		totalCharged += row.PlatformCharged
		if row.UpstreamSpent != nil {
			totalSpent += *row.UpstreamSpent
			spentKnown++
		}
	}
	status.TotalPlatformCost = totalPlatform
	status.TotalPlatformCharged = totalCharged
	status.TotalUpstreamSpent = totalSpent
	status.UpstreamSpentAccounts = spentKnown
	return status, nil
}

// BillingReconcileStatus 页面所需的完整状态。
type BillingReconcileStatus struct {
	Day                   string                `json:"day"`
	Rows                  []BillingReconcileRow `json:"rows"`
	TotalPlatformCost     float64               `json:"total_platform_cost"`
	TotalPlatformCharged  float64               `json:"total_platform_charged"`
	TotalUpstreamSpent    float64               `json:"total_upstream_spent"`
	UpstreamSpentAccounts int                   `json:"upstream_spent_accounts"`
	LastRunAt             *time.Time            `json:"last_run_at,omitempty"`
	LastError             string                `json:"last_error,omitempty"`
}

func (s *BillingReconcileService) setLastError(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.mu.Unlock()
}

// SortBillingReconcileRows 按上游扣款与平台成本差额绝对值降序排列，便于先看异常。
func SortBillingReconcileRows(rows []BillingReconcileRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := 0.0, 0.0
		if rows[i].Difference != nil {
			left = *rows[i].Difference
		}
		if rows[j].Difference != nil {
			right = *rows[j].Difference
		}
		if left == right {
			return rows[i].AccountID < rows[j].AccountID
		}
		return left > right
	})
}
