-- 010_module_installations.up.sql
CREATE TABLE module_installations (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    module_id       uuid NOT NULL REFERENCES modules(id)       ON DELETE RESTRICT,
    version         varchar(50) NOT NULL,
    status          varchar(30) NOT NULL DEFAULT 'installed'
                       CHECK (status IN ('installed', 'enabled', 'disabled', 'failed')),
    configuration   jsonb NOT NULL DEFAULT '{}',
    row_version     bigint NOT NULL DEFAULT 1,    -- optimistic locking (03 §48)
    installed_by    uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    installed_at    timestamptz NOT NULL DEFAULT now(),
    enabled_at      timestamptz,
    disabled_at     timestamptz,
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_module_installations UNIQUE (organization_id, module_id)
);
SELECT attach_updated_at('module_installations');
