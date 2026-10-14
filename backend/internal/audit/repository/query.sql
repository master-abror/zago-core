-- name: ListActivitiesByRequestID :many
-- Jejak audit (`activities`, migrasi 030) untuk satu request. Dipakai tes ujung-ke-ujung (T5f)
-- untuk membuktikan `request_id` mengalir dari HTTP ke activity.
-- Platform: organization_id NULL = semua organisasi (diagnostik/tes). Pemanggil bertenant WAJIB
-- mengisi organization_id (docs/10 §13: query bertenant selalu memfilter organisasi).
SELECT *
FROM activities
WHERE request_id = sqlc.arg('request_id')::text
  AND (sqlc.narg('organization_id')::uuid IS NULL OR organization_id = sqlc.narg('organization_id')::uuid)
ORDER BY created_at, id;
