package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type accountTokenGuardRepository struct{ db *sql.DB }

// NewAccountTokenGuardRepository 提供凭证守护的巡检状态与日志存储。
func NewAccountTokenGuardRepository(db *sql.DB) service.AccountTokenGuardRepository {
	return &accountTokenGuardRepository{db: db}
}

func (r *accountTokenGuardRepository) UpsertState(ctx context.Context, state service.AccountTokenGuardState) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO account_token_guard_states(
 account_id,account_name,account_status,schedulable,probe_state,probe_detail,latency_ms,fail_streak,last_probe_at,last_fix_at,last_fix_action,last_fix_result,updated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
 ON CONFLICT(account_id) DO UPDATE SET
 account_name=EXCLUDED.account_name,account_status=EXCLUDED.account_status,schedulable=EXCLUDED.schedulable,
 probe_state=EXCLUDED.probe_state,probe_detail=EXCLUDED.probe_detail,latency_ms=EXCLUDED.latency_ms,
 fail_streak=EXCLUDED.fail_streak,last_probe_at=EXCLUDED.last_probe_at,
 last_fix_at=COALESCE(EXCLUDED.last_fix_at,account_token_guard_states.last_fix_at),
 last_fix_action=CASE WHEN EXCLUDED.last_fix_at IS NULL THEN account_token_guard_states.last_fix_action ELSE EXCLUDED.last_fix_action END,
 last_fix_result=CASE WHEN EXCLUDED.last_fix_at IS NULL THEN account_token_guard_states.last_fix_result ELSE EXCLUDED.last_fix_result END,
 updated_at=NOW()`,
		state.AccountID, state.AccountName, state.AccountStatus, state.Schedulable, state.ProbeState, state.ProbeDetail,
		state.LatencyMS, state.FailStreak, state.LastProbeAt, state.LastFixAt, state.LastFixAction, state.LastFixResult)
	return err
}

func (r *accountTokenGuardRepository) DeleteStatesExcept(ctx context.Context, accountIDs []int64) error {
	if len(accountIDs) == 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM account_token_guard_states WHERE NOT (account_id = ANY($1))`, pq.Array(accountIDs))
	return err
}

func (r *accountTokenGuardRepository) RecordEvent(ctx context.Context, event service.AccountTokenGuardEvent) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO account_token_guard_events(account_id,account_name,kind,detail,latency_ms)
 VALUES($1,$2,$3,$4,$5)`, event.AccountID, event.AccountName, event.Kind, event.Detail, event.LatencyMS)
	if err == nil {
		return nil
	}
	return err
}

func (r *accountTokenGuardRepository) ListEvents(ctx context.Context, offset, limit int) ([]service.AccountTokenGuardEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,account_id,account_name,kind,detail,latency_ms,created_at
 FROM account_token_guard_events ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	events := make([]service.AccountTokenGuardEvent, 0, limit)
	for rows.Next() {
		var event service.AccountTokenGuardEvent
		if err := rows.Scan(&event.ID, &event.AccountID, &event.AccountName, &event.Kind, &event.Detail, &event.LatencyMS, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *accountTokenGuardRepository) ListStates(ctx context.Context) ([]service.AccountTokenGuardState, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT account_id,account_name,account_status,schedulable,probe_state,probe_detail,
 latency_ms,fail_streak,last_probe_at,last_fix_at,last_fix_action,last_fix_result,updated_at
 FROM account_token_guard_states ORDER BY account_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	states := make([]service.AccountTokenGuardState, 0, 32)
	for rows.Next() {
		var state service.AccountTokenGuardState
		if err := rows.Scan(&state.AccountID, &state.AccountName, &state.AccountStatus, &state.Schedulable, &state.ProbeState,
			&state.ProbeDetail, &state.LatencyMS, &state.FailStreak, &state.LastProbeAt, &state.LastFixAt, &state.LastFixAction,
			&state.LastFixResult, &state.UpdatedAt); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (r *accountTokenGuardRepository) PruneEvents(ctx context.Context, before time.Time) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM account_token_guard_events WHERE created_at < $1`, before)
	return err
}


// ListSeriesAccess 返回目标分组内账号的「自身模型清单 + 各分组白名单」，供 6 系兜底判定。
func (r *accountTokenGuardRepository) ListSeriesAccess(ctx context.Context, groupIDs []int64) ([]service.AccountTokenGuardSeriesAccount, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT a.id,
       COALESCE(a.name,'') ,
       COALESCE(a.status,''),
       COALESCE(a.schedulable,false),
       COALESCE(a.credentials->'model_mapping','{}'::jsonb)::text,
       COALESCE(jsonb_agg(jsonb_build_object('group_id', ag.group_id, 'allowed', to_jsonb(ag.allowed_models)))::text,'[]')
FROM accounts a
JOIN account_groups ag ON ag.account_id = a.id AND ag.group_id = ANY($1)
WHERE a.deleted_at IS NULL
GROUP BY a.id, a.name, a.status, a.schedulable, a.credentials`, pq.Array(groupIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.AccountTokenGuardSeriesAccount, 0, 16)
	for rows.Next() {
		var item service.AccountTokenGuardSeriesAccount
		var mappingRaw, groupsRaw string
		if err := rows.Scan(&item.AccountID, &item.AccountName, &item.Status, &item.Schedulable, &mappingRaw, &groupsRaw); err != nil {
			return nil, err
		}
		models := map[string]any{}
		if strings.TrimSpace(mappingRaw) != "" {
			_ = json.Unmarshal([]byte(mappingRaw), &models)
		}
		for key := range models {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				item.OwnModels = append(item.OwnModels, trimmed)
			}
		}
		var bindings []struct {
			GroupID int64           `json:"group_id"`
			Allowed json.RawMessage `json:"allowed"`
		}
		if strings.TrimSpace(groupsRaw) != "" {
			_ = json.Unmarshal([]byte(groupsRaw), &bindings)
		}
		item.Allowed = map[int64][]string{}
		for _, binding := range bindings {
			item.GroupIDs = append(item.GroupIDs, binding.GroupID)
			raw := strings.TrimSpace(string(binding.Allowed))
			if raw == "" || raw == "null" {
				continue
			}
			var list []string
			if json.Unmarshal(binding.Allowed, &list) == nil && len(list) > 0 {
				item.Allowed[binding.GroupID] = list
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// RestoreSeriesAccess 覆盖账号在目标分组的白名单；models 为空时清空限制（回到不限制），
// ensureSchedulable 为真时顺带把处于 active 的账号重新打开调度。
func (r *accountTokenGuardRepository) RestoreSeriesAccess(ctx context.Context, accountID int64, groupIDs []int64, models []string, ensureSchedulable bool) (int, error) {
	if accountID <= 0 || len(groupIDs) == 0 {
		return 0, nil
	}
	var affected int64
	if len(models) == 0 {
		res, err := r.db.ExecContext(ctx, `UPDATE account_groups SET allowed_models=NULL WHERE account_id=$1 AND group_id=ANY($2)`, accountID, pq.Array(groupIDs))
		if err != nil {
			return 0, err
		}
		affected, _ = res.RowsAffected()
	} else {
		payload, err := json.Marshal(models)
		if err != nil {
			return 0, err
		}
		res, err := r.db.ExecContext(ctx, `UPDATE account_groups SET allowed_models=$3::jsonb WHERE account_id=$1 AND group_id=ANY($2)`, accountID, pq.Array(groupIDs), string(payload))
		if err != nil {
			return 0, err
		}
		affected, _ = res.RowsAffected()
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE accounts SET updated_at=clock_timestamp() WHERE id=$1`, accountID); err != nil {
		return int(affected), err
	}
	if ensureSchedulable {
		if _, err := r.db.ExecContext(ctx, `UPDATE accounts SET schedulable=true, updated_at=clock_timestamp() WHERE id=$1 AND status=$2`, accountID, service.StatusActive); err != nil {
			return int(affected), err
		}
	}
	return int(affected), nil
}
