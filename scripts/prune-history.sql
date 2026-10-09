-- Operator-controlled retention. Preview is the default; never remove resumable uploads or unresolved copies.
-- psql -v ON_ERROR_STOP=1 -v apply=false -v transfer_days=90 -v audit_days=365 -f scripts/prune-history.sql
\if :{?apply}
\else
  \set apply false
\endif
\if :{?transfer_days}
\else
  \set transfer_days 90
\endif
\if :{?audit_days}
\else
  \set audit_days 365
\endif
BEGIN;
SET LOCAL statement_timeout = '30s';
CREATE TEMP TABLE retention_policy ON COMMIT DROP AS
SELECT GREATEST(30, :'transfer_days'::integer) AS transfer_days,
       GREATEST(90, :'audit_days'::integer) AS audit_days;
CREATE TEMP TABLE expired_transfers ON COMMIT DROP AS
SELECT id FROM transfers
WHERE finished_at < now() - make_interval(days => (SELECT transfer_days FROM retention_policy))
  AND (status = 'done' OR (direction = 'download' AND status IN ('failed', 'canceled', 'interrupted')))
ORDER BY finished_at, id LIMIT 1000;
CREATE TEMP TABLE expired_audit ON COMMIT DROP AS
SELECT id FROM audit_events
WHERE at < now() - make_interval(days => (SELECT audit_days FROM retention_policy))
ORDER BY at, id LIMIT 1000;
SELECT (SELECT count(*) FROM expired_transfers) AS eligible_transfers,
       (SELECT count(*) FROM expired_audit) AS eligible_audit_events;
\if :apply
  DELETE FROM transfers WHERE id IN (SELECT id FROM expired_transfers)
    AND (status = 'done' OR (direction = 'download' AND status IN ('failed', 'canceled', 'interrupted')));
  DELETE FROM audit_events WHERE id IN (SELECT id FROM expired_audit);
\endif
COMMIT;
