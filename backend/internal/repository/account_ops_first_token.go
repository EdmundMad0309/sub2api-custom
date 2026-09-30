package repository

import (
	"context"
	"database/sql"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.FirstTokenMonitorRepository = (*accountRepository)(nil)

// ListFirstTokenSamples 统计指定分组内每个账号在窗口内的平均首字延迟。
// 只统计 first_token_ms > 0 的成功样本；没有任何样本时平均值按 0 返回。
func (r *accountRepository) ListFirstTokenSamples(ctx context.Context, groupIDs []int64, since time.Time) ([]service.FirstTokenAccountSample, error) {
	if r == nil || r.sql == nil || len(groupIDs) == 0 {
		return []service.FirstTokenAccountSample{}, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
SELECT a.id, COALESCE(a.name, ''), COALESCE(a.status, ''), a.schedulable,
       COALESCE(stats.avg_ms, 0)::float8, COALESCE(stats.samples, 0),
       COALESCE(groups.group_ids, ARRAY[]::bigint[])
FROM accounts a
JOIN (
    SELECT DISTINCT account_id FROM account_groups WHERE group_id = ANY($1::bigint[])
) sel ON sel.account_id = a.id
LEFT JOIN LATERAL (
    SELECT AVG(u.first_token_ms) AS avg_ms, COUNT(*) AS samples
    FROM usage_logs u
    WHERE u.account_id = a.id AND u.first_token_ms > 0 AND u.created_at >= $2
) stats ON true
LEFT JOIN LATERAL (
    SELECT array_agg(ag.group_id ORDER BY ag.group_id) AS group_ids
    FROM account_groups ag WHERE ag.account_id = a.id
) groups ON true
WHERE a.deleted_at IS NULL
ORDER BY a.id`, pq.Array(groupIDs), since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	samples := make([]service.FirstTokenAccountSample, 0, 16)
	for rows.Next() {
		var sample service.FirstTokenAccountSample
		var groups []int64
		if err := rows.Scan(&sample.AccountID, &sample.AccountName, &sample.Status, &sample.Schedulable,
			&sample.AverageMS, &sample.Samples, pq.Array(&groups)); err != nil {
			return nil, err
		}
		sample.GroupIDs = groups
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return samples, nil
}

// SetAccountSchedulable 实现 service.FirstTokenMonitorRepository。
func (r *accountRepository) SetAccountSchedulable(ctx context.Context, accountID int64, schedulable bool) error {
	return r.SetSchedulable(ctx, accountID, schedulable)
}

// ProvideAccountOpsExtraRepository 暴露 accountRepository 的具体类型，供智能运维
// 扩展功能（首字监控 / 账单对账）注入；实现与主账号仓储完全一致（共享同一套
// 调度快照同步与 outbox 逻辑）。
func ProvideAccountOpsExtraRepository(client *dbent.Client, sqlDB *sql.DB, schedulerCache service.SchedulerCache) *accountRepository {
	return newAccountRepositoryWithSQL(client, sqlDB, schedulerCache)
}
