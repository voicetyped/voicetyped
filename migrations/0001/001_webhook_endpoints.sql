CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id VARCHAR(50) PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    modified_at TIMESTAMP NOT NULL DEFAULT NOW(),
    version INTEGER DEFAULT 0,
    tenant_id VARCHAR(50),
    partition_id VARCHAR(50),
    access_id VARCHAR(50),
    deleted_at TIMESTAMP,
    name VARCHAR(255) NOT NULL,
    url VARCHAR(2048) NOT NULL,
    secret VARCHAR(512) NOT NULL,
    event_types JSONB DEFAULT '[]',
    is_active BOOLEAN DEFAULT TRUE,
    description TEXT,
    failure_count INTEGER DEFAULT 0,
    last_failure_at TIMESTAMP,
    circuit_state VARCHAR(20) DEFAULT 'closed',
    max_rps INTEGER DEFAULT 10
);

CREATE INDEX IF NOT EXISTS idx_wh_tenant ON webhook_endpoints(tenant_id);
CREATE INDEX IF NOT EXISTS idx_wh_active ON webhook_endpoints(is_active);
CREATE INDEX IF NOT EXISTS idx_wh_events ON webhook_endpoints USING GIN(event_types);
