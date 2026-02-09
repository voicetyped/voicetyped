CREATE TABLE IF NOT EXISTS dead_letters (
    id VARCHAR(50) PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    modified_at TIMESTAMP NOT NULL DEFAULT NOW(),
    version INTEGER DEFAULT 0,
    tenant_id VARCHAR(50),
    partition_id VARCHAR(50),
    access_id VARCHAR(50),
    deleted_at TIMESTAMP,
    webhook_id VARCHAR(50) NOT NULL REFERENCES webhook_endpoints(id),
    event_id VARCHAR(50) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload TEXT NOT NULL,
    last_error TEXT,
    attempts INTEGER DEFAULT 0,
    replayable BOOLEAN DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_dl_webhook ON dead_letters(webhook_id, replayable);
