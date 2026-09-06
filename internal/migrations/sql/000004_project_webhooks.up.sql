CREATE TABLE project_webhooks (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE RESTRICT,
    url TEXT NOT NULL,
    secret BYTEA NOT NULL,
    events TEXT NOT NULL DEFAULT 'down,recovered',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT project_webhooks_url_not_blank CHECK (btrim(url) <> ''),
    CONSTRAINT project_webhooks_events_not_blank CHECK (btrim(events) <> '')
);

CREATE INDEX project_webhooks_project_id_active_idx
    ON project_webhooks (project_id, created_at)
    WHERE deleted_at IS NULL;

CREATE TABLE webhook_deliveries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    webhook_id UUID NOT NULL REFERENCES project_webhooks (id) ON DELETE CASCADE,
    event TEXT NOT NULL,
    status_code INTEGER,
    attempts INTEGER NOT NULL DEFAULT 1,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT webhook_deliveries_event_not_blank CHECK (btrim(event) <> ''),
    CONSTRAINT webhook_deliveries_attempts_positive CHECK (attempts >= 1)
);

CREATE INDEX webhook_deliveries_webhook_id_idx
    ON webhook_deliveries (webhook_id, id DESC);
