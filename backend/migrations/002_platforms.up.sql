-- 002_platforms.up.sql
CREATE TABLE platforms (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    name             varchar(200) NOT NULL,
    installation_key varchar(100) NOT NULL,
    version          varchar(50)  NOT NULL,
    status           varchar(30)  NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'maintenance')),
    created_at       timestamptz  NOT NULL DEFAULT now(),
    updated_at       timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT uq_platforms_installation_key UNIQUE (installation_key)
);
SELECT attach_updated_at('platforms');
