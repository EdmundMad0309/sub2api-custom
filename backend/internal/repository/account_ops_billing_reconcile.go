package repository

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.BillingReconcileRepository = (*accountRepository)(nil)

func parseOptionalFloat(raw string) *float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &value
}

func parseOptionalTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999-07"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return &parsed
		}
	}
	return nil
}

// CollectBillingReconcileRows 聚合指定日各账号的平台成本，并读取上游账单探测的余额快照。
func (r *accountRepository) CollectBillingReconcileRows(ctx context.Context, day time.Time) ([]service.BillingReconcileRow, error) {
	if r == nil || r.sql == nil {
		return []service.BillingReconcileRow{}, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
SELECT a.id,
       COALESCE(a.name, ''),
       COALESCE(a.status, ''),
       COALESCE(SUM(COALESCE(u.actual_cost, u.total_cost)), 0)::float8,
       COALESCE(SUM(COALESCE(u.account_stats_cost, u.total_cost) * COALESCE(u.account_rate_multiplier, 1)), 0)::float8,
       COUNT(u.id),
       COALESCE(SUM(u.input_tokens::bigint + u.output_tokens::bigint), 0),
       COALESCE(a.extra->'upstream_billing_probe'->'balance'->'data'->>'remaining', ''),
       COALESCE(a.extra->'upstream_billing_probe'->'balance'->'data'->>'wallet_balance', ''),
       COALESCE(a.extra->'upstream_billing_probe'->'balance'->'data'->>'currency', a.extra->'upstream_billing_probe'->'balance'->'data'->>'wallet_currency', ''),
       COALESCE(a.extra->'upstream_billing_probe'->'balance'->>'received_at', '')
FROM accounts a
LEFT JOIN usage_logs u ON u.account_id = a.id
     AND u.created_at >= $1 AND u.created_at < $1 + INTERVAL '1 day'
WHERE a.deleted_at IS NULL
GROUP BY a.id
ORDER BY a.id`, day)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.BillingReconcileRow, 0, 16)
	for rows.Next() {
		var item service.BillingReconcileRow
		var remaining, wallet, currency, probedAt string
		if err := rows.Scan(&item.AccountID, &item.AccountName, &item.Status,
			&item.PlatformCharged, &item.PlatformCost, &item.Requests, &item.TotalTokens,
			&remaining, &wallet, &currency, &probedAt); err != nil {
			return nil, err
		}
		if balance := parseOptionalFloat(remaining); balance != nil {
			item.UpstreamBalance = balance
		} else if legacy := parseOptionalFloat(wallet); legacy != nil {
			item.UpstreamBalance = legacy
		}
		item.UpstreamCurrency = strings.TrimSpace(currency)
		item.UpstreamProbedAt = parseOptionalTime(probedAt)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertBillingDailyStats 写入指定日的对账快照（重复执行覆盖同一天）。
func (r *accountRepository) UpsertBillingDailyStats(ctx context.Context, day time.Time, rows []service.BillingReconcileRow) error {
	if r == nil || r.sql == nil || len(rows) == 0 {
		return nil
	}
	for _, item := range rows {
		var balance any
		if item.UpstreamBalance != nil {
			balance = *item.UpstreamBalance
		}
		var probedAt any
		if item.UpstreamProbedAt != nil {
			probedAt = *item.UpstreamProbedAt
		}
		var currency any
		if strings.TrimSpace(item.UpstreamCurrency) != "" {
			currency = item.UpstreamCurrency
		}
		if _, err := r.sql.ExecContext(ctx, `
INSERT INTO account_billing_daily_stats
    (account_id, day, platform_cost, platform_charged, requests, total_tokens,
     upstream_balance, upstream_currency, upstream_probed_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
ON CONFLICT (account_id, day) DO UPDATE SET
    platform_cost = EXCLUDED.platform_cost,
    platform_charged = EXCLUDED.platform_charged,
    requests = EXCLUDED.requests,
    total_tokens = EXCLUDED.total_tokens,
    upstream_balance = COALESCE(EXCLUDED.upstream_balance, account_billing_daily_stats.upstream_balance),
    upstream_currency = COALESCE(EXCLUDED.upstream_currency, account_billing_daily_stats.upstream_currency),
    upstream_probed_at = COALESCE(EXCLUDED.upstream_probed_at, account_billing_daily_stats.upstream_probed_at),
    updated_at = NOW()`,
			item.AccountID, day, item.PlatformCost, item.PlatformCharged, item.Requests, item.TotalTokens,
			balance, currency, probedAt); err != nil {
			return err
		}
	}
	return nil
}

// ListBillingDailyStats 读取指定日的对账结果，并用相邻两日余额差值推算上游扣款。
func (r *accountRepository) ListBillingDailyStats(ctx context.Context, day time.Time) ([]service.BillingReconcileRow, error) {
	if r == nil || r.sql == nil {
		return []service.BillingReconcileRow{}, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
SELECT s.account_id,
       COALESCE(a.name, ''),
       COALESCE(a.status, ''),
       s.requests,
       s.total_tokens,
       s.platform_cost::float8,
       s.platform_charged::float8,
       s.upstream_balance::float8,
       COALESCE(s.upstream_currency, ''),
       s.upstream_probed_at,
       prev.upstream_balance::float8
FROM account_billing_daily_stats s
JOIN accounts a ON a.id = s.account_id AND a.deleted_at IS NULL
LEFT JOIN account_billing_daily_stats prev
       ON prev.account_id = s.account_id AND prev.day = s.day - INTERVAL '1 day'
WHERE s.day = $1
ORDER BY s.account_id`, day)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.BillingReconcileRow, 0, 16)
	for rows.Next() {
		var item service.BillingReconcileRow
		var balance, prevBalance sql.NullFloat64
		var probedAt sql.NullTime
		if err := rows.Scan(&item.AccountID, &item.AccountName, &item.Status,
			&item.Requests, &item.TotalTokens, &item.PlatformCost, &item.PlatformCharged,
			&balance, &item.UpstreamCurrency, &probedAt, &prevBalance); err != nil {
			return nil, err
		}
		if balance.Valid {
			value := balance.Float64
			item.UpstreamBalance = &value
		}
		if probedAt.Valid {
			at := probedAt.Time
			item.UpstreamProbedAt = &at
		}
		if balance.Valid && prevBalance.Valid {
			spent := prevBalance.Float64 - balance.Float64
			item.UpstreamSpent = &spent
			diff := spent - item.PlatformCost
			item.Difference = &diff
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
