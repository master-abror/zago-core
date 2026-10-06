-- 015_policies.up.sql
CREATE TABLE policies (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    name            varchar(150) NOT NULL,
    code            varchar(150) NOT NULL,
    description     text,
    effect          varchar(20) NOT NULL CHECK (effect IN ('allow', 'deny')),
    expression      text NOT NULL,
    status          varchar(30) NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'disabled')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_policies_org_code UNIQUE (organization_id, code)
);

CREATE UNIQUE INDEX uq_policies_global_code ON policies (code) WHERE organization_id IS NULL;
SELECT attach_updated_at('policies');
