-- 009_module_dependencies.up.sql
CREATE TABLE module_dependencies (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    module_id            uuid NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    dependency_module_id uuid NOT NULL REFERENCES modules(id) ON DELETE RESTRICT,
    version_constraint   varchar(100) NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_module_dependencies      UNIQUE (module_id, dependency_module_id),
    CONSTRAINT ck_module_dependencies_self CHECK (module_id <> dependency_module_id)
);
