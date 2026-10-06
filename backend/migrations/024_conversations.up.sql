-- 024_conversations.up.sql
CREATE TABLE conversations (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    type            varchar(20) NOT NULL CHECK (type IN ('direct', 'group')),
    title           varchar(255),
    direct_key      text,   -- least(user_a,user_b) || ':' || greatest(...), only for direct
    created_by      uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status          varchar(30) NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'archived')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz,

    CONSTRAINT uq_conversations_id_org UNIQUE (id, organization_id),
    CONSTRAINT ck_conversations_direct_key CHECK ((type = 'direct') = (direct_key IS NOT NULL))
);

-- one direct conversation per pair, even under concurrent creation
CREATE UNIQUE INDEX uq_conversations_direct ON conversations (organization_id, direct_key)
    WHERE type = 'direct';
CREATE INDEX idx_conversations_org ON conversations (organization_id, status);
SELECT attach_updated_at('conversations');
