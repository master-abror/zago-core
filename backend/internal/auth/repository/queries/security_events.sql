-- security_events bersifat append-only bagi app_user (INSERT/SELECT saja, docs/04 §13).

-- name: InsertSecurityEvent :exec
INSERT INTO security_events (organization_id, user_id, session_id, event_type, result, ip_address, user_agent, request_id, metadata)
VALUES (
    sqlc.narg('organization_id')::uuid,
    sqlc.narg('user_id')::uuid,
    sqlc.narg('session_id')::uuid,
    sqlc.arg('event_type')::text,
    sqlc.arg('result')::text,
    sqlc.narg('ip_address')::inet,
    sqlc.narg('user_agent')::text,
    sqlc.narg('request_id')::text,
    sqlc.arg('metadata')::jsonb
);

-- name: ListSecurityEventsByUser :many
-- Riwayat login milik user sendiri (docs/05 §14). Keyset: kirim (before_created_at, before_id)
-- dari baris terakhir halaman sebelumnya; keduanya NULL = halaman pertama.
SELECT *
FROM security_events
WHERE user_id = sqlc.arg('user_id')::uuid
  AND (
      sqlc.narg('before_created_at')::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size')::int;
