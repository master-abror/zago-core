-- 005_organization_memberships.up.sql
CREATE TABLE organization_memberships (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    user_id         uuid NOT NULL REFERENCES users(id)         ON DELETE RESTRICT,
    status          varchar(30) NOT NULL DEFAULT 'invited'
                       CHECK (status IN ('invited', 'active', 'suspended', 'removed')),
    joined_at       timestamptz,
    invited_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_org_memberships UNIQUE (organization_id, user_id)
);

CREATE INDEX idx_org_memberships_user       ON organization_memberships (user_id);
CREATE INDEX idx_org_memberships_org_status ON organization_memberships (organization_id, status);
SELECT attach_updated_at('organization_memberships');
