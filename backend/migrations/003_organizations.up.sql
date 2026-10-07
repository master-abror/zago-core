-- 003_organizations.up.sql
CREATE TABLE organizations (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    name              varchar(200) NOT NULL,
    slug              varchar(100) NOT NULL,
    legal_name        varchar(255),
    status            varchar(30)  NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active', 'suspended', 'archived')),
    default_locale    varchar(20)  NOT NULL DEFAULT 'en-US',
    default_timezone  varchar(100) NOT NULL DEFAULT 'UTC',
    created_at        timestamptz  NOT NULL DEFAULT now(),
    updated_at        timestamptz  NOT NULL DEFAULT now(),
    deleted_at        timestamptz,

    CONSTRAINT uq_organizations_slug UNIQUE (slug)
);

CREATE INDEX idx_organizations_status     ON organizations (status) WHERE deleted_at IS NULL;
CREATE INDEX idx_organizations_deleted_at ON organizations (deleted_at);
SELECT attach_updated_at('organizations');
