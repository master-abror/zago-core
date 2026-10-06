-- 013_role_permissions.up.sql
CREATE TABLE role_permissions (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    role_id       uuid NOT NULL REFERENCES roles(id)       ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions(id) ON DELETE RESTRICT,
    created_by    uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_role_permissions UNIQUE (role_id, permission_id)
);
