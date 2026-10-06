-- 017_credentials.up.sql
CREATE TABLE credentials (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_type varchar(30) NOT NULL CHECK (credential_type IN ('password', 'totp', 'recovery_code', 'webauthn')),
    secret_hash     text,  -- password / recovery code: PHC-format hash. TOTP: encrypted secret (not a hash)
    metadata        jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz,
    revoked_at      timestamptz
);

CREATE INDEX idx_credentials_user_type ON credentials (user_id, credential_type) WHERE revoked_at IS NULL;
-- one active password per user
CREATE UNIQUE INDEX uq_credentials_active_password ON credentials (user_id)
    WHERE credential_type = 'password' AND revoked_at IS NULL;
SELECT attach_updated_at('credentials');
