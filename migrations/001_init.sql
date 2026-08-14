CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS bili_accounts (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    mid INTEGER NOT NULL DEFAULT 0,
    nickname TEXT NOT NULL DEFAULT '',
    encrypted_credentials BLOB,
    credential_nonce BLOB,
    refresh_token_encrypted BLOB,
    refresh_token_nonce BLOB,
    logged_in INTEGER NOT NULL DEFAULT 0 CHECK (logged_in IN (0,1)),
    last_checked_at TEXT,
    last_error_code TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_cursors (
    source TEXT PRIMARY KEY,
    cursor_id INTEGER NOT NULL DEFAULT 0,
    cursor_time INTEGER NOT NULL DEFAULT 0,
    initialized INTEGER NOT NULL DEFAULT 0 CHECK (initialized IN (0,1)),
    last_success_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mention_events (
    id TEXT PRIMARY KEY,
    notification_id TEXT NOT NULL UNIQUE,
    occurred_at TEXT NOT NULL,
    sender_mid INTEGER NOT NULL,
    sender_name TEXT NOT NULL,
    sender_avatar TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL,
    subject_id INTEGER NOT NULL DEFAULT 0,
    root_id INTEGER NOT NULL DEFAULT 0,
    source_id INTEGER NOT NULL DEFAULT 0,
    target_id INTEGER NOT NULL DEFAULT 0,
    business_type INTEGER NOT NULL DEFAULT 0,
    uri TEXT NOT NULL DEFAULT '',
    aid INTEGER NOT NULL DEFAULT 0,
    bvid TEXT NOT NULL DEFAULT '',
    rpid INTEGER NOT NULL DEFAULT 0,
    root_rpid INTEGER NOT NULL DEFAULT 0,
    raw_json TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'received',
    error_code TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE(rpid)
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    creator_mid INTEGER NOT NULL DEFAULT 0,
    creator_name TEXT NOT NULL DEFAULT '',
    anonymous_id TEXT NOT NULL DEFAULT '',
    notification_id TEXT,
    source_comment_rpid INTEGER NOT NULL DEFAULT 0,
    aid INTEGER NOT NULL DEFAULT 0,
    bvid TEXT NOT NULL DEFAULT '',
    cid INTEGER NOT NULL DEFAULT 0,
    page INTEGER NOT NULL DEFAULT 1,
    start_ms INTEGER NOT NULL,
    end_ms INTEGER NOT NULL,
    requested_json TEXT NOT NULL,
    final_json TEXT NOT NULL DEFAULT '{}',
    dedupe_key TEXT NOT NULL,
    status TEXT NOT NULL,
    progress INTEGER NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    available_at TEXT NOT NULL,
    lease_owner TEXT,
    lease_until TEXT,
    cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0,1)),
    error_code TEXT NOT NULL DEFAULT '',
    user_error TEXT NOT NULL DEFAULT '',
    diagnostic TEXT NOT NULL DEFAULT '',
    temp_dir TEXT NOT NULL DEFAULT '',
    artifact_id TEXT,
    artifact_path TEXT NOT NULL DEFAULT '',
    artifact_sha256 TEXT NOT NULL DEFAULT '',
    artifact_bytes INTEGER NOT NULL DEFAULT 0,
    bili_image_url TEXT NOT NULL DEFAULT '',
    published_rpid INTEGER NOT NULL DEFAULT 0,
    publish_status TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(notification_id) REFERENCES mention_events(notification_id)
);
CREATE INDEX IF NOT EXISTS idx_jobs_claim ON jobs(status, available_at, lease_until, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_created ON jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_bvid ON jobs(bvid, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_mention_comment_unique ON jobs(source, source_comment_rpid) WHERE source = 'bili_mention' AND source_comment_rpid > 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_active_dedupe ON jobs(dedupe_key, source) WHERE status NOT IN ('succeeded','failed','cancelled');

CREATE TABLE IF NOT EXISTS job_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    attempt INTEGER NOT NULL,
    worker_id TEXT NOT NULL,
    started_at TEXT NOT NULL,
    completed_at TEXT,
    outcome TEXT NOT NULL DEFAULT 'running',
    error_code TEXT NOT NULL DEFAULT '',
    diagnostic TEXT NOT NULL DEFAULT '',
    UNIQUE(job_id, attempt)
);

CREATE TABLE IF NOT EXISTS artifacts (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    path TEXT NOT NULL UNIQUE,
    sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    frame_count INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    mime_type TEXT NOT NULL DEFAULT 'image/gif',
    requested_json TEXT NOT NULL,
    final_json TEXT NOT NULL,
    original_size_bytes INTEGER NOT NULL,
    compression_attempts INTEGER NOT NULL DEFAULT 0,
    degraded INTEGER NOT NULL DEFAULT 0 CHECK (degraded IN (0,1)),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_artifacts_expiry ON artifacts(expires_at);
CREATE INDEX IF NOT EXISTS idx_artifacts_sha ON artifacts(sha256);

CREATE TABLE IF NOT EXISTS published_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,
    aid INTEGER NOT NULL,
    rpid INTEGER NOT NULL DEFAULT 0,
    image_url TEXT NOT NULL,
    task_marker TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL,
    verified_at TEXT,
    response_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rate_limit_usage (
    scope TEXT NOT NULL,
    subject TEXT NOT NULL,
    window_start TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(scope, subject, window_start)
);

CREATE TABLE IF NOT EXISTS blocked_users (
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    expires_at TEXT,
    created_at TEXT NOT NULL,
    PRIMARY KEY(subject_type, subject_id)
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    sensitive INTEGER NOT NULL DEFAULT 0 CHECK (sensitive IN (0,1)),
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    details_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC);

CREATE TABLE IF NOT EXISTS video_cache (
    cache_key TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS danmaku_cache (
    cache_key TEXT PRIMARY KEY,
    value BLOB NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS bot_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    paused INTEGER NOT NULL DEFAULT 0 CHECK (paused IN (0,1)),
    circuit_reason TEXT NOT NULL DEFAULT '',
    circuit_until TEXT,
    updated_at TEXT NOT NULL
);
INSERT OR IGNORE INTO bot_state(id, paused, updated_at) VALUES(1, 0, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
