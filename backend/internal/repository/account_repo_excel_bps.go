package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountExcelBPSRepository = (*accountRepository)(nil)

// DisableExcelBPSOn403 changes only the protocol switch. A stale request cannot
// disable an account whose credentials or opt-in have since been changed.
func (r *accountRepository) DisableExcelBPSOn403(ctx context.Context, account *service.Account) (bool, error) {
	if !account.IsExcelBPSAutoDisableOn403Enabled() {
		return false, nil
	}
	if dbent.TxFromContext(ctx) != nil {
		return r.disableExcelBPSOn403InTx(ctx, account)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.disableExcelBPSOn403InTx(ctx, account)
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	changed, err := r.disableExcelBPSOn403InTx(dbent.NewTxContext(ctx, tx), account)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if changed {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return changed, nil
}

func (r *accountRepository) disableExcelBPSOn403InTx(ctx context.Context, account *service.Account) (bool, error) {
	credentials, err := json.Marshal(account.Credentials)
	if err != nil {
		return false, err
	}
	client := clientFromContext(ctx, r.client)
	// The marker is written with the switch so the account list can flag the
	// suspected Excel ban until an admin turns the protocol back on.
	result, err := client.ExecContext(ctx, `
UPDATE accounts
SET extra = jsonb_set(extra, '{openai_excel_bps}', 'false'::jsonb) || jsonb_build_object('openai_excel_bps_403_disabled_at', $3::text), updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL AND parent_account_id IS NULL
  AND platform = 'openai' AND type = 'oauth'
  AND credentials = $2::jsonb
  AND extra -> 'openai_excel_bps' = 'true'::jsonb
  AND extra -> 'openai_excel_bps_auto_disable_on_403' = 'true'::jsonb`,
		account.ID, string(credentials), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, nil); err != nil {
		return false, err
	}
	return true, nil
}

var _ service.AccountSixSeriesRestrictionRepository = (*accountRepository)(nil)

// DisableSixSeriesOnExcelBPS403 removes 6-series models from every group
// allowlist of the account after the Excel/BPS channel was rejected with
// HTTP 403 (upstream policy block). Non-6-series models keep being served.
func (r *accountRepository) DisableSixSeriesOnExcelBPS403(ctx context.Context, account *service.Account) (bool, error) {
	if account == nil || account.ID <= 0 || r.sql == nil {
		return false, nil
	}
	// 仅当该账号存在「启用中」的 Excel 模式质量规则时才干预；
	// 规则暂停或删除后不再自动调整账号模型白名单。
	var ruleActive bool
	activeRows, activeErr := r.sql.QueryContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM scheduled_test_plans p
		WHERE p.account_id = $1 AND p.enabled = true
		  AND p.pelican_config->'quality' IS NOT NULL
		  AND p.pelican_config->'quality'->>'action' = 'excel_mode')`, account.ID)
	if activeErr != nil {
		return false, activeErr
	}
	if activeRows.Next() {
		if scanErr := activeRows.Scan(&ruleActive); scanErr != nil {
			_ = activeRows.Close()
			return false, scanErr
		}
	}
	_ = activeRows.Close()
	if err := activeRows.Err(); err != nil {
		return false, err
	}
	if !ruleActive {
		return false, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
SELECT ag.group_id,
       COALESCE(ag.allowed_models::text, ''),
       COALESCE(g.model_allowlist->'models'::text, '[]'),
       COALESCE(a.credentials->'model_mapping'::text, '{}')
FROM account_groups ag
JOIN groups g ON g.id = ag.group_id
JOIN accounts a ON a.id = ag.account_id
WHERE ag.account_id = $1 AND a.deleted_at IS NULL AND g.deleted_at IS NULL`, account.ID)
	if err != nil {
		return false, err
	}
	type groupPlan struct {
		groupID   int64
		remaining []string
	}
	plans := make([]groupPlan, 0, 4)
	for rows.Next() {
		var groupID int64
		var accountAllowed, groupAllowed, mapping string
		if err = rows.Scan(&groupID, &accountAllowed, &groupAllowed, &mapping); err != nil {
			_ = rows.Close()
			return false, err
		}
		models := []string{}
		if mapping != "" && mapping != "{}" {
			var decoded map[string]any
			if json.Unmarshal([]byte(mapping), &decoded) == nil {
				for key := range decoded {
					if trimmed := strings.TrimSpace(key); trimmed != "" {
						models = append(models, trimmed)
					}
				}
			}
		}
		if len(models) == 0 && accountAllowed != "" && accountAllowed != "null" {
			var decoded []string
			if json.Unmarshal([]byte(accountAllowed), &decoded) == nil {
				models = decoded
			}
		}
		if len(models) == 0 && groupAllowed != "" {
			var decoded []string
			if json.Unmarshal([]byte(groupAllowed), &decoded) == nil {
				models = decoded
			}
		}
		if len(models) == 0 {
			continue
		}
		remaining := make([]string, 0, len(models))
		sixSeen := false
		seen := map[string]bool{}
		for _, m := range models {
			trimmed := strings.TrimSpace(m)
			if trimmed == "" || seen[strings.ToLower(trimmed)] {
				continue
			}
			seen[strings.ToLower(trimmed)] = true
			if strings.HasPrefix(strings.ToLower(trimmed), "gpt-6") {
				sixSeen = true
				continue
			}
			remaining = append(remaining, trimmed)
		}
		if !sixSeen || len(remaining) == 0 {
			continue
		}
		plans = append(plans, groupPlan{groupID: groupID, remaining: remaining})
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	if len(plans) == 0 {
		return false, nil
	}
	groupIDs := make([]int64, 0, len(plans))
	for _, plan := range plans {
		payload, marshalErr := json.Marshal(plan.remaining)
		if marshalErr != nil {
			return false, marshalErr
		}
		if _, err = r.sql.ExecContext(ctx, `UPDATE account_groups SET allowed_models=$3::jsonb WHERE account_id=$1 AND group_id=$2`, account.ID, plan.groupID, string(payload)); err != nil {
			return false, err
		}
		groupIDs = append(groupIDs, plan.groupID)
	}
	if _, err = r.sql.ExecContext(ctx, `UPDATE accounts SET updated_at=clock_timestamp() WHERE id=$1`, account.ID); err != nil {
		return false, err
	}
	eventPayload := map[string]any{"group_ids": groupIDs}
	_ = enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &account.ID, nil, eventPayload)
	return true, nil
}
