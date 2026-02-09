-- Rooms table for optional SFU room persistence.
CREATE TABLE IF NOT EXISTS rooms (
    id VARCHAR(50) PRIMARY KEY,
    max_peers INTEGER NOT NULL DEFAULT 10,
    metadata JSONB DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    tenant_id VARCHAR(50) NOT NULL DEFAULT '',
    partition_id VARCHAR(50) NOT NULL DEFAULT '',
    access_id VARCHAR(50) NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_rooms_active ON rooms (is_active) WHERE deleted_at IS NULL;
CREATE INDEX idx_rooms_tenant ON rooms (tenant_id) WHERE deleted_at IS NULL;
