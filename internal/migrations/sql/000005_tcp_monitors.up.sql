ALTER TABLE monitors
    ADD COLUMN target TEXT NOT NULL DEFAULT '';

ALTER TABLE monitors
    DROP CONSTRAINT monitors_type_supported;
ALTER TABLE monitors
    ADD CONSTRAINT monitors_type_supported CHECK (type IN ('http', 'heartbeat', 'tcp'));

ALTER TABLE monitors
    DROP CONSTRAINT monitors_target_matches_type;
ALTER TABLE monitors
    ADD CONSTRAINT monitors_target_matches_type CHECK (
        (type = 'http' AND btrim(url) <> '' AND target = '' AND heartbeat_token_hash IS NULL)
        OR
        (type = 'tcp' AND url = '' AND btrim(target) <> '' AND heartbeat_token_hash IS NULL)
        OR
        (type = 'heartbeat' AND url = '' AND target = '' AND heartbeat_token_hash IS NOT NULL)
    );

CREATE UNIQUE INDEX monitors_project_tcp_target_unique_active
    ON monitors (project_id, target)
    WHERE deleted_at IS NULL AND type = 'tcp';
