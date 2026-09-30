-- 上游面板余额：按账号保存上游面板的登录凭据（NewAPI / Sub2API），
-- 供智能运维登录上游站点读取该账号对应的余额。
CREATE TABLE IF NOT EXISTS account_upstream_panel_credentials (
    account_id         BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    site_type          VARCHAR(16) NOT NULL DEFAULT 'sub2api',
    base_url           TEXT NOT NULL DEFAULT '',
    username           TEXT NOT NULL DEFAULT '',
    password_encrypted TEXT NOT NULL DEFAULT '',
    enabled            BOOLEAN NOT NULL DEFAULT TRUE,
    last_probe_at      TIMESTAMPTZ,
    last_status        VARCHAR(16) NOT NULL DEFAULT '',
    last_error         TEXT NOT NULL DEFAULT '',
    last_http_status   INTEGER NOT NULL DEFAULT 0,
    balance            NUMERIC(20, 8),
    balance_unit       VARCHAR(16) NOT NULL DEFAULT '',
    balance_detail     JSONB,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_upstream_panel_enabled
    ON account_upstream_panel_credentials (enabled);