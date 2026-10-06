-- 022_notifications.up.sql
CREATE TABLE notifications (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id   uuid REFERENCES organizations(id) ON DELETE CASCADE,
    recipient_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type              varchar(100) NOT NULL,
    title             varchar(255),   -- English fallback only (08 §8)
    body              text,           -- English fallback only
    data              jsonb NOT NULL DEFAULT '{}',   -- i18n parameters
    priority          varchar(20) NOT NULL DEFAULT 'normal'
                         CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    status            varchar(30) NOT NULL DEFAULT 'unread'
                         CHECK (status IN ('unread', 'read', 'archived', 'expired')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    read_at           timestamptz,
    expires_at        timestamptz
);

CREATE INDEX idx_notifications_recipient_status ON notifications (recipient_user_id, status);
CREATE INDEX idx_notifications_created          ON notifications (created_at DESC);
