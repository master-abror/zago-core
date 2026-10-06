-- 001_common_functions.up.sql
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Attaches the updated_at trigger to a table. Called explicitly by every migration
-- that creates a table with an updated_at column (see §4).
CREATE OR REPLACE FUNCTION attach_updated_at(target regclass)
RETURNS void AS $$
DECLARE
    tname text := (SELECT relname FROM pg_class WHERE oid = target);
BEGIN
    EXECUTE format(
        'CREATE TRIGGER %I BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION set_updated_at()',
        'trg_' || tname || '_updated_at', target);
END;
$$ LANGUAGE plpgsql;
