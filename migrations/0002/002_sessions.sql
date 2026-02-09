-- Dialog sessions table for recovery and audit.
CREATE TABLE IF NOT EXISTS dialog_sessions (
    id VARCHAR(50) PRIMARY KEY,
    dialog_name VARCHAR(255) NOT NULL,
    current_state VARCHAR(255) NOT NULL,
    variables JSONB DEFAULT '{}',
    history JSONB DEFAULT '[]',
    room_id VARCHAR(50),
    peer_id VARCHAR(50),
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    tenant_id VARCHAR(50) NOT NULL DEFAULT '',
    partition_id VARCHAR(50) NOT NULL DEFAULT '',
    access_id VARCHAR(50) NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_sessions_active ON dialog_sessions (is_active) WHERE deleted_at IS NULL;
CREATE INDEX idx_sessions_dialog ON dialog_sessions (dialog_name) WHERE deleted_at IS NULL;
CREATE INDEX idx_sessions_room ON dialog_sessions (room_id) WHERE room_id IS NOT NULL AND deleted_at IS NULL;
