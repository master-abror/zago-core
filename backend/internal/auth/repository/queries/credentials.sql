-- name: GetActivePasswordCredential :one
-- secret_hash NULL/kosong tidak boleh lolos sebagai hash sah: COALESCE menghasilkan string kosong
-- yang gagal diurai hasher (fail-closed, ErrInvalidHash).
SELECT id, user_id, COALESCE(secret_hash, '')::text AS secret_hash, created_at, updated_at, last_used_at
FROM credentials
WHERE user_id = sqlc.arg('user_id')::uuid
  AND credential_type = 'password'
  AND revoked_at IS NULL;

-- name: InsertPasswordCredential :one
INSERT INTO credentials (user_id, credential_type, secret_hash)
VALUES (sqlc.arg('user_id')::uuid, 'password', sqlc.arg('secret_hash')::text)
RETURNING id;

-- name: SetPasswordHash :execrows
-- Ganti/reset password: tanpa syarat atas hash lama (pemanggil sudah memverifikasi kewenangan).
UPDATE credentials
SET secret_hash = sqlc.arg('secret_hash')::text
WHERE id = sqlc.arg('credential_id')::uuid
  AND credential_type = 'password'
  AND revoked_at IS NULL;

-- name: RehashPasswordIfUnchanged :execrows
-- Rehash-on-login. Hanya menulis bila hash di baris MASIH hash yang tadi diverifikasi: bila
-- password diganti secara bersamaan, rehash dari password lama tidak boleh menimpanya.
UPDATE credentials
SET secret_hash  = sqlc.arg('new_hash')::text,
    last_used_at = now()
WHERE id = sqlc.arg('credential_id')::uuid
  AND secret_hash = sqlc.arg('old_hash')::text
  AND credential_type = 'password'
  AND revoked_at IS NULL;

-- name: TouchCredentialUsed :exec
UPDATE credentials
SET last_used_at = now()
WHERE id = sqlc.arg('credential_id')::uuid;
