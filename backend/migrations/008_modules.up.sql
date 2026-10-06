-- 008_modules.up.sql
CREATE TABLE modules (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    code         varchar(100) NOT NULL,
    name         varchar(200) NOT NULL,
    version      varchar(50)  NOT NULL,
    description  text,
    module_type  varchar(50)  NOT NULL DEFAULT 'business'
                    CHECK (module_type IN ('core', 'business')),
    status       varchar(30)  NOT NULL DEFAULT 'registered'
                    CHECK (status IN ('registered', 'installed', 'enabled', 'disabled', 'failed', 'uninstalled')),
    manifest     jsonb NOT NULL,
    installed_at timestamptz,
    enabled_at   timestamptz,
    disabled_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_modules_code UNIQUE (code),
    CONSTRAINT ck_modules_code_format CHECK (code ~ '^[a-z][a-z0-9_]{1,49}$')
);
SELECT attach_updated_at('modules');
