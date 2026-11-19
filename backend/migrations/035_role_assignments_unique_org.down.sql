-- 035_role_assignments_unique_org.down.sql
-- Mengembalikan kunci lama (tanpa organization_id). Gagal dengan pesan terbaca bila sudah ada
-- assignment aktif yang sama di organisasi berbeda, karena kunci lama tidak dapat menampungnya.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM role_assignments
         WHERE revoked_at IS NULL
         GROUP BY role_id, subject_type, subject_id, scope_type,
                  COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid)
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'tidak dapat membatalkan migrasi 035: ada assignment aktif yang sama di organisasi berbeda; cabut salah satunya dulu';
    END IF;
END
$$;

DROP INDEX uq_role_assignments_active;

CREATE UNIQUE INDEX uq_role_assignments_active ON role_assignments
    (role_id, subject_type, subject_id, scope_type,
     COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE revoked_at IS NULL;
