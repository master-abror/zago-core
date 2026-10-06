-- 025_conversation_members.up.sql
CREATE TABLE conversation_members (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id      uuid NOT NULL,
    organization_id      uuid NOT NULL,
    user_id              uuid NOT NULL,
    member_role          varchar(30) NOT NULL DEFAULT 'member'
                            CHECK (member_role IN ('member', 'moderator', 'owner')),
    status               varchar(30) NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'left')),
    joined_at            timestamptz NOT NULL DEFAULT now(),
    left_at              timestamptz,
    last_read_message_id uuid, -- FK added in 026, after `messages` exists
    muted_until          timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_conversation_members UNIQUE (conversation_id, user_id),
    -- domain invariant 5: a member belongs to the conversation's organization
    CONSTRAINT fk_conversation_members_conversation
        FOREIGN KEY (conversation_id, organization_id) REFERENCES conversations (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_conversation_members_org_member
        FOREIGN KEY (organization_id, user_id) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

CREATE INDEX idx_conversation_members_user ON conversation_members (user_id, status);
SELECT attach_updated_at('conversation_members');
