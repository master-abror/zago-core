-- 019_security_events.up.sql
CREATE TABLE security_events (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid,   -- soft reference (§2.5)
    user_id         uuid,   -- soft reference
    session_id      uuid,   -- soft reference
    event_type      varchar(100) NOT NULL,
    result          varchar(30) NOT NULL CHECK (result IN ('success', 'failure')),
    ip_address      inet,
    user_agent      text,
    request_id      varchar(100),
    metadata        jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_security_events_user_created ON security_events (user_id, created_at DESC);
CREATE INDEX idx_security_events_org_created  ON security_events (organization_id, created_at DESC);
