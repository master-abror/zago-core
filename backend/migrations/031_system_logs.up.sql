-- 031_system_logs.up.sql
CREATE TABLE system_logs (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    service     varchar(100) NOT NULL,
    environment varchar(50)  NOT NULL,
    level       varchar(20)  NOT NULL CHECK (level IN ('debug', 'info', 'warn', 'error', 'fatal')),
    event_code  varchar(150) NOT NULL,
    message     text NOT NULL,
    request_id  varchar(100),
    trace_id    varchar(100),
    metadata    jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_system_logs_created         ON system_logs (created_at DESC);
CREATE INDEX idx_system_logs_level_created   ON system_logs (level, created_at DESC);
CREATE INDEX idx_system_logs_service_created ON system_logs (service, created_at DESC);
