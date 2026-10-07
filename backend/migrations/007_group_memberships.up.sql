-- 007_group_memberships.up.sql
CREATE TABLE group_memberships (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    group_id        uuid NOT NULL,
    organization_id uuid NOT NULL,
    user_id         uuid NOT NULL,
    status          varchar(30) NOT NULL DEFAULT 'invited'
                       CHECK (status IN ('invited', 'active', 'suspended', 'removed')),
    joined_at       timestamptz,
    added_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_group_memberships UNIQUE (group_id, user_id),
    -- the group really belongs to this organization
    CONSTRAINT fk_group_memberships_group
        FOREIGN KEY (group_id, organization_id) REFERENCES groups (id, organization_id) ON DELETE CASCADE,
    -- the user really has a membership in this organization (domain invariant 2)
    CONSTRAINT fk_group_memberships_org_member
        FOREIGN KEY (organization_id, user_id) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

CREATE INDEX idx_group_memberships_user         ON group_memberships (user_id);
CREATE INDEX idx_group_memberships_group_status ON group_memberships (group_id, status);
SELECT attach_updated_at('group_memberships');
