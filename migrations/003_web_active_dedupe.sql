-- Public web requests share one active job. Mention delivery jobs intentionally
-- remain distinct so every triggering user can receive their own root comment.
-- A database created by the short-lived 002 schema may already contain races;
-- keep the oldest job and close later identical web jobs before adding the
-- invariant, so upgrade cannot fail halfway through startup.
UPDATE job_attempts
SET completed_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'), outcome='deduplicated', error_code='DEDUPLICATED'
WHERE completed_at IS NULL AND job_id IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY dedupe_key ORDER BY created_at,id) AS duplicate_number
    FROM jobs
    WHERE source='web' AND status NOT IN ('succeeded','failed','cancelled')
  ) WHERE duplicate_number>1
);
UPDATE jobs
SET status='cancelled', cancel_requested=1, error_code='DEDUPLICATED',
    user_error='已合并到更早创建的相同任务', lease_owner=NULL, lease_until=NULL,
    completed_at=COALESCE(completed_at, updated_at), updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE id IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY dedupe_key ORDER BY created_at,id) AS duplicate_number
    FROM jobs
    WHERE source='web' AND status NOT IN ('succeeded','failed','cancelled')
  ) WHERE duplicate_number>1
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_active_web_dedupe
ON jobs(dedupe_key)
WHERE source='web' AND status NOT IN ('succeeded','failed','cancelled');
