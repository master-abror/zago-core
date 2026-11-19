-- 035_role_assignments_unique_org.up.sql
-- ADR-0022 butir 65. uq_role_assignments_active (014) tidak memuat organization_id, padahal bentuk
-- kanonik scope organisasi menulis scope_id NULL (butir 56): role sistem yang sama untuk user yang
-- sama di dua organisasi berbeda bertabrakan di indeks lama dan salah dilaporkan sebagai
-- assignment ganda. Kunci baru menambahkan organization_id. Nama indeks tetap supaya pemetaan
-- 23505 di aplikasi tidak berubah. Bentuk NULL dan bentuk scope_id = organisasi tetap dua baris
-- berbeda di indeks; keduanya dipagari pemeriksaan ActiveDuplicate di aplikasi (butir 56).
--
-- Aman terhadap data yang ada: kunci baru lebih halus dari kunci lama, sehingga setiap isi tabel
-- yang lolos indeks lama pasti lolos indeks baru (CREATE tidak dapat gagal karena data).
DROP INDEX uq_role_assignments_active;

CREATE UNIQUE INDEX uq_role_assignments_active ON role_assignments
    (role_id, subject_type, subject_id, scope_type,
     COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid),
     COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE revoked_at IS NULL;
