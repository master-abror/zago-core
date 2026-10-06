-- 023_inbox_items.up.sql
CREATE TABLE inbox_items (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id   uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    recipient_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type              varchar(100) NOT NULL,
    title             varchar(255),
    description       text,
    data              jsonb NOT NULL DEFAULT '{}',
    resource_type     varchar(150),
    resource_id       uuid,
    action_url        text,
    status            varchar(30) NOT NULL DEFAULT 'pending'
                         CHECK (status IN ('pending', 'in_progress', 'completed', 'dismissed', 'expired')),
    priority          varchar(20) NOT NULL DEFAULT 'normal'
                         CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz,
    expires_at        timestamptz
);

CREATE INDEX idx_inbox_items_recipient_status ON inbox_items (recipient_user_id, status);
CREATE INDEX idx_inbox_items_resource         ON inbox_items (resource_type, resource_id);
