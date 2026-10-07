-- 004_users.up.sql
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    username      varchar(100),
    display_name  varchar(200) NOT NULL,
    first_name    varchar(100),
    last_name     varchar(100),
    email         varchar(320) NOT NULL,
    phone         varchar(50),
    avatar_url    text,
    locale        varchar(20)  NOT NULL DEFAULT 'en-US',
    timezone      varchar(100) NOT NULL DEFAULT 'UTC',
    status        varchar(30)  NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'active', 'suspended', 'locked', 'deactivated', 'deleted')),
    last_login_at timestamptz,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    updated_at    timestamptz  NOT NULL DEFAULT now(),
    deleted_at    timestamptz,

    CONSTRAINT uq_users_email      UNIQUE (email),
    CONSTRAINT uq_users_username   UNIQUE (username),
    CONSTRAINT ck_users_email_lower CHECK (email = lower(email))
);

CREATE INDEX idx_users_status ON users (status) WHERE deleted_at IS NULL;
SELECT attach_updated_at('users');
