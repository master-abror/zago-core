-- Hanya SHA-256 token (token_hash) yang pernah masuk ke tabel ini (docs/05 §7.1).

-- name: InsertSession :one
INSERT INTO sessions (token_hash, user_id, organization_id, expires_at, ip_address, user_agent, auth_level)
VALUES (
    sqlc.arg('token_hash')::bytea,
    sqlc.arg('user_id')::uuid,
    sqlc.narg('organization_id')::uuid,
    sqlc.arg('expires_at')::timestamptz,
    sqlc.narg('ip_address')::inet,
    sqlc.narg('user_agent')::text,
    sqlc.arg('auth_level')::text
)
RETURNING *;

-- name: GetSessionByTokenHash :one
-- Mengembalikan baris APA ADANYA (termasuk yang dicabut/kedaluwarsa); keputusan valid-tidaknya
-- ada di service sesi (revoked_at, expires_at, idle timeout).
SELECT *
FROM sessions
WHERE token_hash = sqlc.arg('token_hash')::bytea;

-- name: GetSessionByIDForUser :one
SELECT *
FROM sessions
WHERE id = sqlc.arg('session_id')::uuid
  AND user_id = sqlc.arg('user_id')::uuid;

-- name: ListActiveSessionsByUser :many
SELECT *
FROM sessions
WHERE user_id = sqlc.arg('user_id')::uuid
  AND revoked_at IS NULL
  AND expires_at > now()
ORDER BY created_at DESC, id DESC;

-- name: RevokeSession :one
-- Milik user itu saja (id sesi bukan kredensial, tapi tetap tak boleh mencabut sesi orang lain).
-- RETURNING token_hash agar pemanggil bisa menghapus entri Redis. Tak ada baris = tak ada sesi
-- aktif milik user itu (pgx.ErrNoRows).
UPDATE sessions
SET revoked_at = now()
WHERE id = sqlc.arg('session_id')::uuid
  AND user_id = sqlc.arg('user_id')::uuid
  AND revoked_at IS NULL
RETURNING id, token_hash;

-- name: RevokeOtherSessions :many
UPDATE sessions
SET revoked_at = now()
WHERE user_id = sqlc.arg('user_id')::uuid
  AND id <> sqlc.arg('keep_session_id')::uuid
  AND revoked_at IS NULL
RETURNING id, token_hash;

-- name: RevokeAllSessions :many
UPDATE sessions
SET revoked_at = now()
WHERE user_id = sqlc.arg('user_id')::uuid
  AND revoked_at IS NULL
RETURNING id, token_hash;

-- name: TouchSessionActivity :execrows
-- Maju saja, tidak pernah mundur (GREATEST). Hasil 1 baris = sesi belum dicabut; 0 baris = sesi
-- sudah dicabut/tidak ada. Pemanggil memakainya juga sebagai pemeriksaan "masih hidup" terhadap
-- sumber kebenaran (PostgreSQL), sehingga entri Redis basi paling lama bertahan satu interval
-- aktivitas (docs/05 §12.1).
UPDATE sessions
SET last_activity_at = GREATEST(last_activity_at, sqlc.arg('at')::timestamptz)
WHERE id = sqlc.arg('session_id')::uuid
  AND revoked_at IS NULL;
