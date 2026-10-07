-- 011_permissions.up.sql
CREATE TABLE permissions (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    module_id   uuid REFERENCES modules(id) ON DELETE RESTRICT,
    resource    varchar(150) NOT NULL,
    action      varchar(100) NOT NULL,
    code        varchar(255) NOT NULL,
    description text,
    is_system   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_permissions_code UNIQUE (code)
);

CREATE INDEX idx_permissions_module ON permissions (module_id);
