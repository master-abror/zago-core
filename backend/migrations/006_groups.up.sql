-- 006_groups.up.sql
CREATE TABLE groups (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    parent_group_id uuid REFERENCES groups(id) ON DELETE RESTRICT,
    name            varchar(200) NOT NULL,
    slug            varchar(100) NOT NULL,
    description     text,
    status          varchar(30) NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'archived')),
    created_by      uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz,

    CONSTRAINT uq_groups_org_slug UNIQUE (organization_id, slug),
    CONSTRAINT uq_groups_id_org   UNIQUE (id, organization_id)   -- target of composite FKs
);

CREATE INDEX idx_groups_organization ON groups (organization_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_groups_parent       ON groups (parent_group_id);
CREATE INDEX idx_groups_status       ON groups (status);
SELECT attach_updated_at('groups');

-- Hierarchy rules a plain FK cannot express:
--   * organization_id is immutable
--   * a parent belongs to the same organization
--   * no cycles, bounded depth (8 levels including the group itself)
CREATE OR REPLACE FUNCTION check_group_hierarchy()
RETURNS trigger AS $$
DECLARE
    max_depth constant int := 8;
    ancestors int;
    has_cycle boolean;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.organization_id IS DISTINCT FROM OLD.organization_id THEN
        RAISE EXCEPTION 'groups.organization_id is immutable';
    END IF;

    IF NEW.parent_group_id IS NOT NULL THEN
        IF NEW.parent_group_id = NEW.id THEN
            RAISE EXCEPTION 'a group cannot be its own parent';
        END IF;

        IF NOT EXISTS (
            SELECT 1 FROM groups
            WHERE id = NEW.parent_group_id AND organization_id = NEW.organization_id
        ) THEN
            RAISE EXCEPTION 'parent_group_id must belong to the same organization_id';
        END IF;

        -- walk up the ancestor chain (bounded, so a pre-existing bad chain cannot loop forever)
        WITH RECURSIVE anc(id, parent_group_id, depth) AS (
            SELECT g.id, g.parent_group_id, 1 FROM groups g WHERE g.id = NEW.parent_group_id
            UNION ALL
            SELECT g.id, g.parent_group_id, a.depth + 1
            FROM groups g JOIN anc a ON g.id = a.parent_group_id
            WHERE a.depth < 64
        )
        SELECT count(*), COALESCE(bool_or(id = NEW.id), false)
          INTO ancestors, has_cycle
          FROM anc;

        IF has_cycle THEN
            RAISE EXCEPTION 'group hierarchy cycle detected';
        END IF;

        IF ancestors + 1 > max_depth THEN
            RAISE EXCEPTION 'group hierarchy deeper than % levels', max_depth;
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_groups_hierarchy
    BEFORE INSERT OR UPDATE OF parent_group_id, organization_id ON groups
    FOR EACH ROW EXECUTE FUNCTION check_group_hierarchy();
