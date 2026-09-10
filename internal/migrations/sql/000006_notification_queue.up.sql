CREATE TABLE notification_events (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE RESTRICT,
    monitor_id UUID NOT NULL REFERENCES monitors (id) ON DELETE RESTRICT,
    kind TEXT NOT NULL,
    project_name TEXT NOT NULL,
    monitor_name TEXT NOT NULL,
    url TEXT NOT NULL DEFAULT '',
    target TEXT NOT NULL DEFAULT '',
    cause TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT notification_events_kind_supported CHECK (kind IN ('down', 'recovered', 'test')),
    CONSTRAINT notification_events_status_supported CHECK (status IN ('pending', 'processing', 'delivered', 'failed')),
    CONSTRAINT notification_events_attempts_nonnegative CHECK (attempts >= 0)
);

CREATE INDEX notification_events_ready_idx
    ON notification_events (available_at, created_at, id)
    WHERE status IN ('pending', 'processing');

CREATE INDEX notification_events_project_idx
    ON notification_events (project_id, created_at DESC, id DESC);

CREATE TABLE notification_attempts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    notification_id UUID NOT NULL REFERENCES notification_events (id) ON DELETE CASCADE,
    attempt INTEGER NOT NULL,
    success BOOLEAN NOT NULL,
    error_code TEXT,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT notification_attempts_attempt_positive CHECK (attempt >= 1),
    CONSTRAINT notification_attempts_error_safe CHECK (error_code IS NULL OR char_length(error_code) <= 100)
);

CREATE INDEX notification_attempts_notification_idx
    ON notification_attempts (notification_id, attempt DESC);
