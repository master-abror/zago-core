-- 021_password_reset_tokens.up.sql
CREATE TABLE password_reset_tokens (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  bytea NOT NULL,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    ip_address  inet,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_password_reset_token_hash UNIQUE (token_hash)
);

CREATE INDEX idx_password_reset_user_open ON password_reset_tokens (user_id) WHERE used_at IS NULL;
