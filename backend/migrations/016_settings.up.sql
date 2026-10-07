-- 016_settings.up.sql
CREATE TABLE settings (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    scope_type  varchar(30) NOT NULL CHECK (scope_type IN ('platform', 'organization', 'group', 'user', 'module')),
    scope_id    uuid NOT NULL,
    module_id   uuid REFERENCES modules(id) ON DELETE CASCADE,
    key         varchar(200) NOT NULL,
    value       jsonb NOT NULL,
    value_type  varchar(30) NOT NULL,
    is_secret   boolean NOT NULL DEFAULT false,
    row_version bigint NOT NULL DEFAULT 1,    -- optimistic locking (03 §48)
    created_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- A single UNIQUE(scope_type, scope_id, module_id, key) would NOT catch two
-- non-module settings with the same key, because module_id IS NULL and
-- NULL <> NULL. Two partial indexes cover both cases:
CREATE UNIQUE INDEX uq_settings_key_with_module ON settings (scope_type, scope_id, module_id, key)
    WHERE module_id IS NOT NULL;
CREATE UNIQUE INDEX uq_settings_key_no_module ON settings (scope_type, scope_id, key)
    WHERE module_id IS NULL;
SELECT attach_updated_at('settings');
