CREATE TABLE IF NOT EXISTS delivery_attempts (
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
    request_body TEXT,
    response_code INTEGER DEFAULT 0,
    response_body TEXT,
    attempt_number INTEGER DEFAULT 1,
    status VARCHAR(20) NOT NULL,
    error TEXT,
    duration_ms BIGINT DEFAULT 0,
    next_retry_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_da_webhook ON delivery_attempts(webhook_id);
CREATE INDEX IF NOT EXISTS idx_da_status ON delivery_attempts(status);
