-- name: InsertVault :exec
INSERT INTO user_vaults (user_id, kdf_memory_kib, kdf_time, kdf_threads, kdf_salt, wrapped_dek, recovery_wrapped_dek)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: VaultByUser :one
SELECT kdf_memory_kib, kdf_time, kdf_threads, kdf_salt, wrapped_dek, recovery_wrapped_dek
FROM user_vaults WHERE user_id = $1;

-- name: UpdateVault :execrows
UPDATE user_vaults
SET kdf_memory_kib = $2, kdf_time = $3, kdf_threads = $4, kdf_salt = $5, wrapped_dek = $6,
    recovery_wrapped_dek = $7, updated_at = now()
WHERE user_id = $1;
