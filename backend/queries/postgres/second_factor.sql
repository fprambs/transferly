-- name: TotpFactor :one
SELECT sealed, confirmed, last_step FROM user_totp WHERE user_id = $1;

-- name: UpsertTotp :exec
INSERT INTO user_totp (user_id, sealed) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET sealed = EXCLUDED.sealed, confirmed = false, last_step = 0, created_at = now();

-- name: ConfirmTotp :exec
UPDATE user_totp SET confirmed = true, last_step = $2 WHERE user_id = $1;

-- name: AdvanceTotpStep :execrows
UPDATE user_totp SET last_step = $2 WHERE user_id = $1 AND last_step < $2;

-- name: DeleteTotp :exec
DELETE FROM user_totp WHERE user_id = $1;

-- name: DeleteRecoveryCodes :exec
DELETE FROM user_recovery_codes WHERE user_id = $1;

-- name: InsertRecoveryCode :exec
INSERT INTO user_recovery_codes (user_id, code_hash) VALUES ($1, $2);

-- name: UseRecoveryCode :execrows
UPDATE user_recovery_codes SET used_at = now() WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL;

-- name: CountRecoveryCodes :one
SELECT count(*) FROM user_recovery_codes WHERE user_id = $1 AND used_at IS NULL;
