-- 030_activities.up.sql
CREATE TABLE activities (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid,   -- soft reference (§2.5)
    actor_user_id   uuid,   -- soft reference
    actor_type      varchar(30) NOT NULL DEFAULT 'user'
                       CHECK (actor_type IN ('user', 'service', 'system')),
    action          varchar(150) NOT NULL,
    resource_type   varchar(150),
    resource_id     uuid,
    scope_type      varchar(30),
    scope_id        uuid,
    result          varchar(30) NOT NULL CHECK (result IN ('success', 'denied', 'failed')),
    ip_address      inet,
    user_agent      text,
    request_id      varchar(100),
    trace_id        varchar(100),
    metadata        jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_activities_org_created   ON activities (organization_id, created_at DESC, id DESC);
CREATE INDEX idx_activities_actor_created ON activities (actor_user_id, created_at DESC, id DESC);
CREATE INDEX idx_activities_scope_created ON activities (scope_type, scope_id, created_at DESC);
CREATE INDEX idx_activities_resource      ON activities (resource_type, resource_id);
CREATE INDEX idx_activities_action        ON activities (action);
