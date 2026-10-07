-- 018_sessions.up.sql
CREATE TABLE sessions (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),       -- internal id, never a credential
    token_hash       bytea NOT NULL,                          -- SHA-256 of the opaque cookie token
    user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id  uuid REFERENCES organizations(id) ON DELETE CASCADE,
    created_at       timestamptz NOT NULL DEFAULT now(),
    last_activity_at timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    ip_address       inet,
    user_agent       text,
    auth_level       varchar(30) NOT NULL DEFAULT 'password'
                        CHECK (auth_level IN ('password', 'mfa')),
    revoked_at       timestamptz,

    CONSTRAINT uq_sessions_token_hash UNIQUE (token_hash)
);

CREATE INDEX idx_sessions_user_active ON sessions (user_id) WHERE revoked_at IS NULL;
