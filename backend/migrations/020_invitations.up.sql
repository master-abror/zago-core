-- 020_invitations.up.sql
CREATE TABLE invitations (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email           varchar(320) NOT NULL,
    invited_by      uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    group_id        uuid,
    role_id         uuid REFERENCES roles(id) ON DELETE RESTRICT,
    token_hash      bytea NOT NULL,
    status          varchar(30) NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'accepted', 'revoked', 'expired')),
    expires_at      timestamptz NOT NULL,
    accepted_at     timestamptz,
    revoked_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_invitations_token_hash UNIQUE (token_hash),
    CONSTRAINT ck_invitations_email_lower CHECK (email = lower(email)),
    CONSTRAINT fk_invitations_group
        FOREIGN KEY (group_id, organization_id) REFERENCES groups (id, organization_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX uq_invitations_pending ON invitations (organization_id, email)
    WHERE status = 'pending';
CREATE INDEX idx_invitations_org_status ON invitations (organization_id, status);
SELECT attach_updated_at('invitations');
