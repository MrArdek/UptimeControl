CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT users_email_not_blank CHECK (btrim(email) <> ''),
    CONSTRAINT users_password_hash_not_blank CHECK (btrim(password_hash) <> '')
);

CREATE UNIQUE INDEX users_email_unique_active
    ON users (lower(email))
    WHERE deleted_at IS NULL;

CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT sessions_expiry_after_creation CHECK (expires_at > created_at)
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_active_idx
    ON sessions (expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE sites (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    check_interval_seconds INTEGER NOT NULL DEFAULT 60,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT sites_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT sites_url_not_blank CHECK (btrim(url) <> ''),
    CONSTRAINT sites_check_interval_range CHECK (
        check_interval_seconds BETWEEN 30 AND 86400
    )
);

CREATE INDEX sites_user_id_active_idx
    ON sites (user_id)
    WHERE deleted_at IS NULL;

CREATE TABLE uptime_checks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    site_id UUID NOT NULL REFERENCES sites (id) ON DELETE RESTRICT,
    region TEXT NOT NULL DEFAULT 'default',
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    available BOOLEAN NOT NULL,
    status_code INTEGER,
    response_time_ms BIGINT,
    error_message TEXT,
    CONSTRAINT uptime_checks_region_not_blank CHECK (btrim(region) <> ''),
    CONSTRAINT uptime_checks_status_code_range CHECK (
        status_code IS NULL OR status_code BETWEEN 100 AND 599
    ),
    CONSTRAINT uptime_checks_response_time_non_negative CHECK (
        response_time_ms IS NULL OR response_time_ms >= 0
    )
);

CREATE INDEX uptime_checks_site_time_idx
    ON uptime_checks (site_id, checked_at DESC);

CREATE TABLE incidents (
    id UUID PRIMARY KEY,
    site_id UUID NOT NULL REFERENCES sites (id) ON DELETE RESTRICT,
    started_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    cause TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT incidents_resolution_after_start CHECK (
        resolved_at IS NULL OR resolved_at >= started_at
    )
);

CREATE INDEX incidents_site_started_idx
    ON incidents (site_id, started_at DESC);

CREATE UNIQUE INDEX incidents_one_open_per_site
    ON incidents (site_id)
    WHERE resolved_at IS NULL;
