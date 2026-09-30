-- 账号账单对账：每个账号每天的平台成本与上游余额快照。
-- 上游扣款由相邻两天的余额差值推算（balance(前一日) - balance(当日)）。
CREATE TABLE IF NOT EXISTS account_billing_daily_stats (
    account_id         BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    day                DATE NOT NULL,
    platform_cost      NUMERIC(20, 8) NOT NULL DEFAULT 0,
    platform_charged   NUMERIC(20, 8) NOT NULL DEFAULT 0,
    requests           BIGINT NOT NULL DEFAULT 0,
    total_tokens       BIGINT NOT NULL DEFAULT 0,
    upstream_balance   NUMERIC(20, 8),
    upstream_currency  TEXT,
    upstream_probed_at TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, day)
);

CREATE INDEX IF NOT EXISTS idx_account_billing_daily_stats_day
    ON account_billing_daily_stats (day DESC);
