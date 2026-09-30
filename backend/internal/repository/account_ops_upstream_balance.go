package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type accountUpstreamPanelRepository struct {
	db *sql.DB
}

// NewAccountUpstreamPanelRepository 创建上游面板凭据仓储（智能运维 · 上游余额）。
func NewAccountUpstreamPanelRepository(db *sql.DB) service.UpstreamPanelCredentialRepository {
	return &accountUpstreamPanelRepository{db: db}
}

const upstreamPanelCredentialColumns = `
	c.account_id,
	COALESCE(a.name, '') AS account_name,
	COALESCE(a.status, '') AS account_status,
	c.site_type,
	c.base_url,
	c.username,
	c.password_encrypted,
	c.enabled,
	c.last_probe_at,
	COALESCE(c.last_status, ''),
	COALESCE(c.last_error, ''),
	COALESCE(c.last_http_status, 0),
	c.balance,
	COALESCE(c.balance_unit, ''),
	c.balance_detail,
	c.updated_at`

func (r *accountUpstreamPanelRepository) ListUpstreamPanelCredentials(ctx context.Context) ([]service.UpstreamPanelCredential, error) {
	if r == nil || r.db == nil {
		return []service.UpstreamPanelCredential{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT`+upstreamPanelCredentialColumns+`
FROM account_upstream_panel_credentials c
JOIN accounts a ON a.id = c.account_id AND a.deleted_at IS NULL
ORDER BY c.account_id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.UpstreamPanelCredential, 0, 16)
	for rows.Next() {
		item, scanErr := scanUpstreamPanelCredential(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *accountUpstreamPanelRepository) GetUpstreamPanelCredential(ctx context.Context, accountID int64) (*service.UpstreamPanelCredential, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	row := r.db.QueryRowContext(ctx, `
SELECT`+upstreamPanelCredentialColumns+`
FROM account_upstream_panel_credentials c
JOIN accounts a ON a.id = c.account_id AND a.deleted_at IS NULL
WHERE c.account_id = $1`, accountID)
	item, err := scanUpstreamPanelCredential(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (r *accountUpstreamPanelRepository) UpsertUpstreamPanelCredential(
	ctx context.Context,
	input service.UpstreamPanelCredentialInput,
	passwordCiphertext string,
) error {
	if r == nil || r.db == nil {
		return errors.New("upstream panel repository is unavailable")
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO account_upstream_panel_credentials
	(account_id, site_type, base_url, username, password_encrypted, enabled, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (account_id) DO UPDATE SET
	site_type = EXCLUDED.site_type,
	base_url = EXCLUDED.base_url,
	username = EXCLUDED.username,
	password_encrypted = CASE
		WHEN EXCLUDED.password_encrypted = '' THEN account_upstream_panel_credentials.password_encrypted
		ELSE EXCLUDED.password_encrypted
	END,
	enabled = EXCLUDED.enabled,
	updated_at = NOW()`,
		input.AccountID, input.SiteType, input.BaseURL, input.Username,
		strings.TrimSpace(passwordCiphertext), input.Enabled)
	return err
}

func (r *accountUpstreamPanelRepository) DeleteUpstreamPanelCredential(ctx context.Context, accountID int64) error {
	if r == nil || r.db == nil {
		return errors.New("upstream panel repository is unavailable")
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM account_upstream_panel_credentials WHERE account_id = $1`, accountID)
	return err
}

func (r *accountUpstreamPanelRepository) SaveUpstreamPanelProbeOutcome(
	ctx context.Context,
	accountID int64,
	outcome service.UpstreamPanelProbeOutcome,
) error {
	if r == nil || r.db == nil {
		return errors.New("upstream panel repository is unavailable")
	}
	var detail any
	if len(outcome.Detail) > 0 {
		payload, err := json.Marshal(outcome.Detail)
		if err != nil {
			return err
		}
		detail = string(payload)
	}
	probedAt := outcome.At
	if probedAt.IsZero() {
		probedAt = time.Now()
	}
	_, err := r.db.ExecContext(ctx, `
UPDATE account_upstream_panel_credentials
SET last_probe_at = $2,
	last_status = $3,
	last_error = $4,
	last_http_status = $5,
	balance = $6,
	balance_unit = $7,
	balance_detail = $8,
	updated_at = NOW()
WHERE account_id = $1`,
		accountID, probedAt, outcome.Status, outcome.Error, outcome.HTTPStatus,
		outcome.Balance, outcome.Unit, detail)
	return err
}

type upstreamPanelCredentialScanner interface {
	Scan(dest ...any) error
}

func scanUpstreamPanelCredential(scanner upstreamPanelCredentialScanner) (*service.UpstreamPanelCredential, error) {
	var (
		item               service.UpstreamPanelCredential
		passwordCiphertext string
		lastProbeAt        sql.NullTime
		lastHTTPStatus     sql.NullInt64
		balance            sql.NullFloat64
		detailRaw          []byte
		updatedAt          sql.NullTime
	)
	if err := scanner.Scan(
		&item.AccountID,
		&item.AccountName,
		&item.AccountStatus,
		&item.SiteType,
		&item.BaseURL,
		&item.Username,
		&passwordCiphertext,
		&item.Enabled,
		&lastProbeAt,
		&item.LastStatus,
		&item.LastError,
		&lastHTTPStatus,
		&balance,
		&item.BalanceUnit,
		&detailRaw,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	item.PasswordCiphertext = passwordCiphertext
	item.PasswordConfigured = strings.TrimSpace(passwordCiphertext) != ""
	if lastProbeAt.Valid {
		at := lastProbeAt.Time
		item.LastProbeAt = &at
	}
	if lastHTTPStatus.Valid {
		item.LastHTTPStatus = int(lastHTTPStatus.Int64)
	}
	if balance.Valid {
		value := balance.Float64
		item.Balance = &value
	}
	if len(detailRaw) > 0 {
		detail := map[string]any{}
		if err := json.Unmarshal(detailRaw, &detail); err == nil {
			item.BalanceDetail = detail
		}
	}
	if updatedAt.Valid {
		at := updatedAt.Time
		item.UpdatedAt = &at
	}
	return &item, nil
}
