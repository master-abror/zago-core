-- HANYA untuk pengembangan lokal dan CI (ADR-0003). Dijalankan setelah 00-roles.sql oleh superuser.
-- `cmd/migrate roundtrip` membuat database sementara agar down(semua) tidak pernah menyentuh data
-- pengembangan; itu membutuhkan CREATEDB pada role migrator. Produksi: role dibuat operator
-- tanpa hak ini, dan roundtrip tidak dijalankan di produksi.
ALTER ROLE app_migrator CREATEDB;
