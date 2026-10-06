-- 032_outbox_events.up.sql   (transactional outbox, 10 §6)
CREATE TABLE outbox_events (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid,   -- soft reference
    event_name      varchar(200) NOT NULL,
    aggregate_type  varchar(100),
    aggregate_id    uuid,
    payload         jsonb NOT NULL DEFAULT '{}',
    request_id      varchar(100),
    trace_id        varchar(100),
    status          varchar(20) NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'processed', 'dead')),
    attempts        integer NOT NULL DEFAULT 0,
    available_at    timestamptz NOT NULL DEFAULT now(),
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    processed_at    timestamptz
);

-- the relay reads: WHERE status = 'pending' AND available_at <= now()
--                  ORDER BY available_at, created_at LIMIT n FOR UPDATE SKIP LOCKED
CREATE INDEX idx_outbox_pending ON outbox_events (available_at, created_at) WHERE status = 'pending';
CREATE INDEX idx_outbox_processed ON outbox_events (processed_at) WHERE status <> 'pending';
