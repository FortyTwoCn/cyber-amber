-- A trigger comment is a distinct delivery job even when another user asks
-- for the same clip.  Rendering is shared through persisted artifacts in the
-- worker, while the source-comment unique index continues to prevent duplicate
-- ingestion of the same mention.
DROP INDEX IF EXISTS idx_jobs_active_dedupe;
CREATE INDEX IF NOT EXISTS idx_jobs_dedupe ON jobs(dedupe_key, created_at DESC);
