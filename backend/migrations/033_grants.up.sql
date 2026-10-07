-- 033_grants.up.sql
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO app_user;

-- tables created later by app_migrator (module tables) get the same grants automatically
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_user;

-- append-only: the runtime role can create and read, never modify or erase
REVOKE UPDATE, DELETE ON activities, security_events, system_logs FROM app_user;

-- maintenance role: the minimum needed for retention and anonymization
GRANT SELECT, DELETE ON activities, security_events, system_logs, outbox_events TO app_maintenance;
GRANT UPDATE (actor_user_id, metadata)                            ON activities      TO app_maintenance;
GRANT UPDATE (user_id, ip_address, user_agent, metadata)          ON security_events TO app_maintenance;

-- ADR-0006: golang-migrate's version table is not application data. The runtime role may read it
-- (schema version check) but never write it. Guarded: the table exists once golang-migrate has opened it.
DO $$
BEGIN
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        REVOKE INSERT, UPDATE, DELETE ON public.schema_migrations FROM app_user;
    END IF;
END
$$;
