-- Dijalankan sekali oleh infrastruktur (superuser). Password di bawah HANYA untuk pengembangan lokal.
-- Produksi: role dibuat operator dengan password dari secret manager (docs/04 §13).
CREATE ROLE app_migrator    WITH LOGIN PASSWORD 'migrator_dev_pw';
CREATE ROLE app_user        WITH LOGIN PASSWORD 'app_dev_pw';
CREATE ROLE app_maintenance WITH LOGIN PASSWORD 'maintenance_dev_pw';

ALTER DATABASE platform OWNER TO app_migrator;
ALTER SCHEMA public OWNER TO app_migrator;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO app_user, app_maintenance;
