-- Pandangan users yang dibutuhkan autentikasi (docs/05). Manajemen user penuh ada di M05.

-- name: GetUserForLoginByEmail :one
-- Email sudah dinormalisasi huruf kecil oleh pemanggil (CHECK ck_users_email_lower di skema).
SELECT id, email, display_name, status
FROM users
WHERE email = sqlc.arg('email')::text
  AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT id, email, display_name, username, locale, timezone, status, last_login_at
FROM users
WHERE id = sqlc.arg('user_id')::uuid
  AND deleted_at IS NULL;

-- name: SetUserLastLogin :exec
UPDATE users
SET last_login_at = now()
WHERE id = sqlc.arg('user_id')::uuid;

-- name: GetDefaultActiveOrganizationID :one
-- Organisasi awal sesi: keanggotaan aktif tertua pada organisasi yang aktif dan tidak dihapus.
-- Tidak ada baris = identitas tingkat platform (organization_id sesi NULL).
SELECT m.organization_id
FROM organization_memberships m
JOIN organizations o ON o.id = m.organization_id
WHERE m.user_id = sqlc.arg('user_id')::uuid
  AND m.status = 'active'
  AND o.status = 'active'
  AND o.deleted_at IS NULL
ORDER BY m.created_at, m.id
LIMIT 1;

-- name: HasActiveMembership :one
-- Dipakai switch-organization: bisa pindah hanya ke organisasi tempat user anggota aktif.
SELECT EXISTS (
    SELECT 1
    FROM organization_memberships m
    JOIN organizations o ON o.id = m.organization_id
    WHERE m.user_id = sqlc.arg('user_id')::uuid
      AND m.organization_id = sqlc.arg('organization_id')::uuid
      AND m.status = 'active'
      AND o.status = 'active'
      AND o.deleted_at IS NULL
) AS is_member;
