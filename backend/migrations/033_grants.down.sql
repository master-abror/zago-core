-- 033_grants.down.sql
REVOKE UPDATE (user_id, ip_address, user_agent, metadata) ON security_events FROM app_maintenance;
REVOKE UPDATE (actor_user_id, metadata)                   ON activities      FROM app_maintenance;
REVOKE SELECT, DELETE ON activities, security_events, system_logs, outbox_events FROM app_maintenance;

ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM app_user;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM app_user;
