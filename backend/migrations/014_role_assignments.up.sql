-- 014_role_assignments.up.sql
CREATE TABLE role_assignments (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    role_id         uuid NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    subject_type    varchar(30) NOT NULL CHECK (subject_type IN ('user', 'group', 'organization', 'service')),
    subject_id      uuid NOT NULL, -- polymorphic; not an FK, see note below
    scope_type      varchar(30) NOT NULL CHECK (scope_type IN ('platform', 'organization', 'group', 'resource', 'own')),
    scope_id        uuid,
    assigned_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    expires_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    revoked_at      timestamptz,

    -- domain invariant 3: an assignment cannot reference an invalid scope
    CONSTRAINT ck_role_assignments_scope CHECK (
           (scope_type IN ('platform', 'own') AND scope_id IS NULL)
        OR  scope_type = 'organization'
        OR (scope_type IN ('group', 'resource') AND scope_id IS NOT NULL)
    ),
    -- platform-scoped assignments have no organization; every other scope has one
    CONSTRAINT ck_role_assignments_org CHECK ((scope_type = 'platform') = (organization_id IS NULL))
);

CREATE INDEX idx_role_assignments_subject ON role_assignments (subject_type, subject_id);
CREATE INDEX idx_role_assignments_org     ON role_assignments (organization_id);
CREATE INDEX idx_role_assignments_scope   ON role_assignments (scope_type, scope_id);
CREATE INDEX idx_role_assignments_active  ON role_assignments (subject_id) WHERE revoked_at IS NULL;

-- at most one active assignment per (role, subject, scope)
CREATE UNIQUE INDEX uq_role_assignments_active ON role_assignments
    (role_id, subject_type, subject_id, scope_type,
     COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE revoked_at IS NULL;
