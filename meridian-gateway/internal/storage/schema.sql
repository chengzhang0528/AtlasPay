CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS merchants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    fee_bps INTEGER NOT NULL DEFAULT 0 CHECK (fee_bps >= 0 AND fee_bps <= 10000),
    admin_token_hash TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS applications (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    name TEXT NOT NULL,
    api_key_hash TEXT NOT NULL UNIQUE,
    webhook_url TEXT NOT NULL DEFAULT '',
    webhook_secret_cipher TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_applications_merchant ON applications(merchant_id);

CREATE TABLE IF NOT EXISTS channel_accounts (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    channel TEXT NOT NULL,
    app_id TEXT NOT NULL,
    credentials_cipher TEXT NOT NULL,
    gateway_url TEXT NOT NULL,
    notify_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (merchant_id, channel)
);
CREATE INDEX IF NOT EXISTS idx_channel_accounts_merchant ON channel_accounts(merchant_id);

CREATE TABLE IF NOT EXISTS static_codes (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    name TEXT NOT NULL,
    store_name TEXT NOT NULL DEFAULT '',
    currency TEXT NOT NULL DEFAULT 'CNY',
    mime_type TEXT NOT NULL,
    image BLOB NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_static_codes_merchant ON static_codes(merchant_id);

CREATE TABLE IF NOT EXISTS payments (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    application_id TEXT NOT NULL REFERENCES applications(id),
    merchant_order_id TEXT NOT NULL,
    method TEXT NOT NULL,
    amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
    currency TEXT NOT NULL,
    subject TEXT NOT NULL,
    status TEXT NOT NULL,
    static_code_id TEXT REFERENCES static_codes(id),
    checkout_token TEXT NOT NULL UNIQUE,
    channel_order_id TEXT NOT NULL DEFAULT '',
    channel_status TEXT NOT NULL DEFAULT '',
    qr_content TEXT NOT NULL DEFAULT '',
    redirect_url TEXT NOT NULL DEFAULT '',
    return_url TEXT NOT NULL DEFAULT '',
    expires_at TEXT,
    paid_at TEXT,
    closed_at TEXT,
    refunded_minor INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    close_attempts INTEGER NOT NULL DEFAULT 0,
    close_next_attempt_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (application_id, merchant_order_id)
);
CREATE INDEX IF NOT EXISTS idx_payments_merchant_created ON payments(merchant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payments_pending ON payments(status, expires_at);

CREATE TABLE IF NOT EXISTS payment_attempts (
    id TEXT PRIMARY KEY,
    payment_id TEXT NOT NULL REFERENCES payments(id),
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    channel TEXT NOT NULL,
    channel_order_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS fees (
    id TEXT PRIMARY KEY,
    payment_id TEXT NOT NULL REFERENCES payments(id),
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    kind TEXT NOT NULL,
    amount_minor INTEGER NOT NULL,
    currency TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (payment_id, kind)
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_merchant_created ON audit_logs(merchant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS outbox_deliveries (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    application_id TEXT NOT NULL REFERENCES applications(id),
    event_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    payload BLOB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'skipped', 'failed', 'exhausted')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    last_status_code INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    delivered_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_outbox_due ON outbox_deliveries(status, next_attempt_at);

CREATE TABLE IF NOT EXISTS channel_events (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    channel TEXT NOT NULL,
    event_key TEXT NOT NULL,
    raw_payload TEXT NOT NULL,
    processed_at TEXT NOT NULL,
    UNIQUE (merchant_id, channel, event_key)
);

CREATE TABLE IF NOT EXISTS refunds (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    payment_id TEXT NOT NULL REFERENCES payments(id),
    merchant_refund_id TEXT NOT NULL,
    amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
    channel_refund_id TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (merchant_id, merchant_refund_id)
);
CREATE INDEX IF NOT EXISTS idx_refunds_payment ON refunds(payment_id, created_at DESC);
