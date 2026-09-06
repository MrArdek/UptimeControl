CREATE TABLE projects (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT projects_name_not_blank CHECK (btrim(name) <> '')
);

CREATE INDEX projects_user_id_active_idx
    ON projects (user_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE monitors (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE RESTRICT,
    type TEXT NOT NULL DEFAULT 'http',
    name TEXT NOT NULL,
    url TEXT NOT NULL DEFAULT '',
    heartbeat_token_hash BYTEA UNIQUE,
    check_interval_seconds INTEGER NOT NULL DEFAULT 60,
    timeout_seconds INTEGER NOT NULL DEFAULT 10,
    enabled BOOLEAN NOT NULL DEFAULT true,
    next_check_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_checked_at TIMESTAMPTZ,
    last_available BOOLEAN,
    last_status_code INTEGER,
    last_response_time_ms BIGINT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT monitors_type_supported CHECK (type IN ('http', 'heartbeat')),
    CONSTRAINT monitors_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT monitors_target_matches_type CHECK (
        (type = 'http' AND btrim(url) <> '' AND heartbeat_token_hash IS NULL)
        OR
        (type = 'heartbeat' AND url = '' AND heartbeat_token_hash IS NOT NULL)
    ),
    CONSTRAINT monitors_check_interval_range CHECK (
        check_interval_seconds BETWEEN 30 AND 86400
    ),
    CONSTRAINT monitors_timeout_range CHECK (timeout_seconds BETWEEN 1 AND 30),
    CONSTRAINT monitors_status_code_range CHECK (
        last_status_code IS NULL OR last_status_code BETWEEN 100 AND 599
    ),
    CONSTRAINT monitors_response_time_non_negative CHECK (
        last_response_time_ms IS NULL OR last_response_time_ms >= 0
    )
);

CREATE INDEX monitors_due_idx
    ON monitors (next_check_at, id)
    WHERE enabled = true AND deleted_at IS NULL;

CREATE INDEX monitors_project_id_active_idx
    ON monitors (project_id, created_at, id)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX monitors_project_url_unique_active
    ON monitors (project_id, url)
    WHERE deleted_at IS NULL AND type = 'http';

-- Preserve installations created before projects existed. Each former site becomes
-- one project with one HTTP monitor. Reusing the UUID in different tables keeps all
-- existing check and incident references valid during the migration.
INSERT INTO projects (id, user_id, name, created_at, updated_at, deleted_at)
SELECT id, user_id, name, created_at, updated_at, deleted_at
FROM sites;

INSERT INTO monitors (
    id,
    project_id,
    name,
    url,
    check_interval_seconds,
    enabled,
    next_check_at,
    created_at,
    updated_at,
    deleted_at
)
SELECT
    id,
    id,
    name,
    url,
    check_interval_seconds,
    enabled,
    now(),
    created_at,
    updated_at,
    deleted_at
FROM sites;

ALTER TABLE uptime_checks
    DROP CONSTRAINT uptime_checks_site_id_fkey;
ALTER TABLE uptime_checks
    RENAME COLUMN site_id TO monitor_id;
ALTER TABLE uptime_checks
    ADD CONSTRAINT uptime_checks_monitor_id_fkey
    FOREIGN KEY (monitor_id) REFERENCES monitors (id) ON DELETE RESTRICT;
ALTER INDEX uptime_checks_site_time_idx
    RENAME TO uptime_checks_monitor_time_idx;

ALTER TABLE incidents
    DROP CONSTRAINT incidents_site_id_fkey;
ALTER TABLE incidents
    RENAME COLUMN site_id TO monitor_id;
ALTER TABLE incidents
    ADD CONSTRAINT incidents_monitor_id_fkey
    FOREIGN KEY (monitor_id) REFERENCES monitors (id) ON DELETE RESTRICT;
ALTER INDEX incidents_site_started_idx
    RENAME TO incidents_monitor_started_idx;
ALTER INDEX incidents_one_open_per_site
    RENAME TO incidents_one_open_per_monitor;
