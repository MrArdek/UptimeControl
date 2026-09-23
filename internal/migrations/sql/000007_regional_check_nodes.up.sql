CREATE TABLE check_nodes (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    region TEXT NOT NULL,
    secret_hash BYTEA NOT NULL UNIQUE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT check_nodes_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT check_nodes_region_not_blank CHECK (btrim(region) <> '')
);

CREATE INDEX check_nodes_user_active_idx
    ON check_nodes (user_id, created_at, id)
    WHERE revoked_at IS NULL;

CREATE TABLE monitor_assignments (
    id UUID PRIMARY KEY,
    node_id UUID NOT NULL REFERENCES check_nodes (id) ON DELETE RESTRICT,
    monitor_id UUID NOT NULL REFERENCES monitors (id) ON DELETE RESTRICT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    next_check_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    leased_until TIMESTAMPTZ,
    last_result_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX monitor_assignments_node_monitor_active_unique
    ON monitor_assignments (node_id, monitor_id)
    WHERE enabled = true;

CREATE INDEX monitor_assignments_due_idx
    ON monitor_assignments (node_id, next_check_at, id)
    WHERE enabled = true;

CREATE INDEX monitor_assignments_monitor_idx
    ON monitor_assignments (monitor_id, node_id)
    WHERE enabled = true;

ALTER TABLE uptime_checks
    ADD COLUMN node_id UUID REFERENCES check_nodes (id) ON DELETE RESTRICT,
    ADD COLUMN assignment_id UUID REFERENCES monitor_assignments (id) ON DELETE RESTRICT,
    ADD COLUMN result_id UUID,
    ADD COLUMN region TEXT,
    ADD COLUMN started_at TIMESTAMPTZ;

CREATE UNIQUE INDEX uptime_checks_node_result_unique
    ON uptime_checks (node_id, result_id)
    WHERE node_id IS NOT NULL AND result_id IS NOT NULL;

CREATE INDEX uptime_checks_monitor_region_time_idx
    ON uptime_checks (monitor_id, region, checked_at DESC, id DESC)
    WHERE node_id IS NOT NULL;
