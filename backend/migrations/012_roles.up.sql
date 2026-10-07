-- 012_roles.up.sql
CREATE TABLE roles (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    group_id        uuid,
    name            varchar(150) NOT NULL,
    slug            varchar(100) NOT NULL,
    description     text,
    role_type       varchar(30) NOT NULL
                       CHECK (role_type IN ('system', 'organization', 'group', 'custom')),
    status          varchar(30) NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'archived')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    -- a group-owned role must belong to a group of the same organization
    CONSTRAINT fk_roles_group
        FOREIGN KEY (group_id, organization_id) REFERENCES groups (id, organization_id) ON DELETE RESTRICT,

    -- the boundary matches the columns (also blocks the half-NULL case that MATCH SIMPLE would skip)
    CONSTRAINT ck_roles_boundary CHECK (
           (role_type = 'system'       AND organization_id IS NULL     AND group_id IS NULL)
        OR (role_type = 'organization' AND organization_id IS NOT NULL AND group_id IS NULL)
        OR (role_type = 'group'        AND organization_id IS NOT NULL AND group_id IS NOT NULL)
        OR (role_type = 'custom'       AND organization_id IS NOT NULL)
    )
);

-- Slug uniqueness per boundary. A composite UNIQUE would not work: NULL <> NULL.
CREATE UNIQUE INDEX uq_roles_global_slug ON roles (slug)
    WHERE organization_id IS NULL;
CREATE UNIQUE INDEX uq_roles_org_slug    ON roles (organization_id, slug)
    WHERE organization_id IS NOT NULL AND group_id IS NULL;
CREATE UNIQUE INDEX uq_roles_group_slug  ON roles (group_id, slug)
    WHERE group_id IS NOT NULL;

CREATE INDEX idx_roles_organization ON roles (organization_id);
SELECT attach_updated_at('roles');
