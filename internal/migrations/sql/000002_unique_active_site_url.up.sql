CREATE UNIQUE INDEX sites_user_url_unique_active
    ON sites (user_id, url)
    WHERE deleted_at IS NULL;
