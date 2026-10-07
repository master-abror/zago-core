-- 029_attachments.up.sql
CREATE TABLE attachments (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_user_id    uuid NOT NULL REFERENCES users(id)         ON DELETE RESTRICT,
    message_id       uuid REFERENCES messages(id) ON DELETE SET NULL,
    storage_provider varchar(50) NOT NULL,
    storage_key      text NOT NULL,
    original_name    varchar(255) NOT NULL,
    content_type     varchar(150) NOT NULL,
    size_bytes       bigint NOT NULL CHECK (size_bytes >= 0),
    checksum         varchar(128),
    status           varchar(30) NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'quarantined', 'deleted')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz
);

CREATE INDEX idx_attachments_message ON attachments (message_id);
CREATE INDEX idx_attachments_org     ON attachments (organization_id, created_at DESC);
