package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/domain"
	"github.com/FortyTwoCn/cyber-amber/migrations"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

func Open(ctx context.Context, path string, busyTimeout time.Duration) (*Store, error) {
	if err := ensureParent(path); err != nil {
		return nil, err
	}
	busyMS := busyTimeout.Milliseconds()
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", filepath.ToSlash(path), busyMS)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	s := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func ensureParent(path string) error {
	parent := filepath.Dir(path)
	if parent == "." || parent == "" {
		return nil
	}
	if err := mkdirAllPrivate(parent); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	return nil
}

func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("bootstrap migrations: %w", err)
	}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		versionText, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return fmt.Errorf("invalid migration name %q", entry.Name())
		}
		version, err := strconv.Atoi(versionText)
		if err != nil {
			return fmt.Errorf("invalid migration version %q: %w", versionText, err)
		}
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=?`, version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if exists != 0 {
			continue
		}
		body, err := migrations.FS.ReadFile(entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)`, version, utcText(time.Now()))
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}
	}
	return nil
}

func (s *Store) Enqueue(ctx context.Context, job domain.Job) error {
	requested, err := json.Marshal(job.Requested)
	if err != nil {
		return fmt.Errorf("marshal requested params: %w", err)
	}
	final, err := json.Marshal(job.Final)
	if err != nil {
		return fmt.Errorf("marshal final params: %w", err)
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	if job.AvailableAt.IsZero() {
		job.AvailableAt = now
	}
	if job.Status == "" {
		job.Status = domain.JobQueued
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = 3
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO jobs(
id,source,creator_mid,creator_name,anonymous_id,notification_id,source_comment_rpid,aid,bvid,cid,page,start_ms,end_ms,
requested_json,final_json,dedupe_key,status,progress,attempts,max_attempts,available_at,error_code,user_error,diagnostic,
temp_dir,artifact_id,artifact_path,artifact_sha256,artifact_bytes,bili_image_url,published_rpid,publish_status,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.Source, job.CreatorMID, job.CreatorName, job.AnonymousID, nullableString(job.NotificationID), job.SourceCommentRPID,
		job.Aid, job.BVID, job.CID, job.Page, job.Start.Milliseconds(), job.End.Milliseconds(), string(requested), string(final),
		job.DedupeKey, job.Status, job.Progress, job.Attempts, job.MaxAttempts, utcText(job.AvailableAt), job.ErrorCode, job.UserError,
		job.Diagnostic, job.TempDir, nullableString(job.ArtifactID), job.ArtifactPath, job.ArtifactSHA256, job.ArtifactBytes,
		job.BiliImageURL, job.PublishedRPID, job.PublishStatus, utcText(job.CreatedAt), utcText(job.UpdatedAt))
	if err != nil {
		return fmt.Errorf("enqueue job: %w", err)
	}
	return nil
}

func (s *Store) GetJob(ctx context.Context, id string) (*domain.Job, error) {
	row := s.db.QueryRowContext(ctx, jobSelect+` WHERE id=?`, id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (s *Store) ListJobs(ctx context.Context, limit, offset int) ([]domain.Job, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, jobSelect+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]domain.Job, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list jobs rows: %w", err)
	}
	return jobs, nil
}

type JobAttemptRecord struct {
	Attempt     int        `json:"attempt"`
	WorkerID    string     `json:"worker_id"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Outcome     string     `json:"outcome"`
	ErrorCode   string     `json:"error_code,omitempty"`
	Diagnostic  string     `json:"diagnostic,omitempty"`
}

func (s *Store) ListJobAttempts(ctx context.Context, jobID string) ([]JobAttemptRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT attempt,worker_id,started_at,completed_at,outcome,error_code,diagnostic FROM job_attempts WHERE job_id=? ORDER BY attempt`, jobID)
	if err != nil {
		return nil, fmt.Errorf("list job attempts: %w", err)
	}
	defer rows.Close()
	var result []JobAttemptRecord
	for rows.Next() {
		var item JobAttemptRecord
		var started string
		var completed sql.NullString
		if err := rows.Scan(&item.Attempt, &item.WorkerID, &started, &completed, &item.Outcome, &item.ErrorCode, &item.Diagnostic); err != nil {
			return nil, fmt.Errorf("scan job attempt: %w", err)
		}
		item.StartedAt, err = parseTime(started)
		if err != nil {
			return nil, err
		}
		if completed.Valid {
			value, err := parseTime(completed.String)
			if err != nil {
				return nil, err
			}
			item.CompletedAt = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) JobStatusCounts(ctx context.Context) (map[domain.JobStatus]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status,COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("count jobs by status: %w", err)
	}
	defer rows.Close()
	result := make(map[domain.JobStatus]int)
	for rows.Next() {
		var status domain.JobStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan job status count: %w", err)
		}
		result[status] = count
	}
	return result, rows.Err()
}

func (s *Store) Claim(ctx context.Context, workerID string, lease time.Duration) (*domain.Job, error) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin claim: %w", err)
	}
	defer tx.Rollback()
	// Recovery is part of every claim transaction instead of being a one-shot
	// startup action.  A process can restart before an old lease expires; in
	// that case the next claim after expiry must still make the job runnable.
	if _, err := recoverExpiredTx(ctx, tx, now); err != nil {
		return nil, fmt.Errorf("recover leases before claim: %w", err)
	}
	row := tx.QueryRowContext(ctx, jobSelect+` WHERE status=? AND cancel_requested=0 AND attempts<max_attempts AND available_at<=? AND (lease_until IS NULL OR lease_until<?)
AND (source<>? OR NOT EXISTS (
  SELECT 1 FROM bot_state WHERE id=1 AND paused=1 AND (circuit_until IS NULL OR circuit_until>?)
))
ORDER BY created_at LIMIT 1`, domain.JobQueued, utcText(now), utcText(now), domain.SourceMention, utcText(now))
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		// The transaction may still contain lease-recovery updates (for example
		// an exhausted job that was moved to failed), so do not roll them back
		// merely because no runnable job remains.
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, fmt.Errorf("commit lease recovery: %w", commitErr)
		}
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select claim: %w", err)
	}
	leaseUntil := now.Add(lease)
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET lease_owner=?,lease_until=?,attempts=attempts+1,started_at=COALESCE(started_at,?),updated_at=? WHERE id=? AND status=? AND (lease_until IS NULL OR lease_until<?)`, workerID, utcText(leaseUntil), utcText(now), utcText(now), job.ID, domain.JobQueued, utcText(now))
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return nil, ErrNotFound
	}
	job.LeaseOwner, job.LeaseUntil, job.Attempts = workerID, &leaseUntil, job.Attempts+1
	_, err = tx.ExecContext(ctx, `INSERT INTO job_attempts(job_id,attempt,worker_id,started_at) VALUES(?,?,?,?)`, job.ID, job.Attempts, workerID, utcText(now))
	if err != nil {
		return nil, fmt.Errorf("record attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}
	return job, nil
}

func (s *Store) RenewLease(ctx context.Context, id, workerID string, lease time.Duration) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET lease_until=?,updated_at=? WHERE id=? AND lease_owner=? AND status NOT IN (?,?,?)`, utcText(now.Add(lease)), utcText(now), id, workerID, domain.JobSucceeded, domain.JobFailed, domain.JobCancelled)
	if err != nil {
		return fmt.Errorf("renew lease: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RecoverExpired(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin expired lease recovery: %w", err)
	}
	defer tx.Rollback()
	count, err := recoverExpiredTx(ctx, tx, now)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit expired lease recovery: %w", err)
	}
	return count, nil
}

func recoverExpiredTx(ctx context.Context, tx *sql.Tx, now time.Time) (int64, error) {
	nowText := utcText(now)
	terminal := []any{domain.JobSucceeded, domain.JobFailed, domain.JobCancelled}
	// Empty queues are polled frequently. Avoid taking a SQLite write lock and
	// generating WAL traffic unless a lease really needs recovery.
	var expired int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
SELECT 1 FROM jobs
WHERE lease_owner IS NOT NULL AND lease_until<? AND status NOT IN (?,?,?)
)`, nowText, terminal[0], terminal[1], terminal[2]).Scan(&expired); err != nil {
		return 0, fmt.Errorf("check expired leases: %w", err)
	}
	if expired == 0 {
		return 0, nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE job_attempts
SET completed_at=?,outcome='interrupted',error_code='WORKER_INTERRUPTED',diagnostic='worker lease expired before the attempt completed'
WHERE completed_at IS NULL AND job_id IN (
  SELECT id FROM jobs WHERE lease_owner IS NOT NULL AND lease_until<? AND status NOT IN (?,?,?)
)`, nowText, nowText, terminal[0], terminal[1], terminal[2])
	if err != nil {
		return 0, fmt.Errorf("close interrupted attempts: %w", err)
	}
	failed, err := tx.ExecContext(ctx, `UPDATE jobs
SET status=?,progress=0,lease_owner=NULL,lease_until=NULL,error_code='WORKER_INTERRUPTED',
    user_error='任务因服务中断且已达到最大尝试次数',diagnostic='worker lease expired at maximum attempt count',
    completed_at=?,updated_at=?
WHERE lease_owner IS NOT NULL AND lease_until<? AND attempts>=max_attempts AND status NOT IN (?,?,?)`,
		domain.JobFailed, nowText, nowText, nowText, terminal[0], terminal[1], terminal[2])
	if err != nil {
		return 0, fmt.Errorf("fail exhausted interrupted jobs: %w", err)
	}
	queued, err := tx.ExecContext(ctx, `UPDATE jobs
SET status=?,progress=0,lease_owner=NULL,lease_until=NULL,available_at=?,error_code='WORKER_INTERRUPTED',
    user_error='任务因服务中断，已自动恢复排队',diagnostic='worker lease expired and was recovered',
    completed_at=NULL,updated_at=?
WHERE lease_owner IS NOT NULL AND lease_until<? AND attempts<max_attempts AND status NOT IN (?,?,?)`,
		domain.JobQueued, nowText, nowText, nowText, terminal[0], terminal[1], terminal[2])
	if err != nil {
		return 0, fmt.Errorf("requeue interrupted jobs: %w", err)
	}
	failedCount, _ := failed.RowsAffected()
	queuedCount, _ := queued.RowsAffected()
	return failedCount + queuedCount, nil
}

func (s *Store) Transition(ctx context.Context, id, workerID string, to domain.JobStatus, progress int) error {
	if progress < 0 || progress > 100 {
		return errors.New("progress must be between 0 and 100")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transition: %w", err)
	}
	defer tx.Rollback()
	var from domain.JobStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id=?`, id).Scan(&from); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read status: %w", err)
	}
	if err := domain.ValidateTransition(from, to); err != nil {
		return err
	}
	now := time.Now().UTC()
	completed := any(nil)
	if to.Terminal() {
		completed = utcText(now)
	}
	query := `UPDATE jobs SET status=?,progress=?,updated_at=?,completed_at=COALESCE(?,completed_at)`
	if to.Terminal() {
		query += `,lease_owner=NULL,lease_until=NULL`
	}
	query += ` WHERE id=?`
	args := []any{to, progress, utcText(now), completed, id}
	if workerID != "" {
		query += ` AND lease_owner=?`
		args = append(args, workerID)
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}
	return nil
}

func (s *Store) UpdateProgress(ctx context.Context, id, workerID string, progress int) error {
	if progress < 0 || progress > 100 {
		return errors.New("progress must be between 0 and 100")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET progress=?,updated_at=? WHERE id=? AND lease_owner=? AND cancel_requested=0`, progress, utcText(time.Now()), id, workerID)
	if err != nil {
		return fmt.Errorf("update progress: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CompleteAttempt(ctx context.Context, id string, attempt int, outcome, errorCode, diagnostic string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE job_attempts SET completed_at=?,outcome=?,error_code=?,diagnostic=? WHERE job_id=? AND attempt=?`, utcText(time.Now()), outcome, errorCode, tail(diagnostic, 4096), id, attempt)
	if err != nil {
		return fmt.Errorf("complete attempt: %w", err)
	}
	return nil
}

func (s *Store) Fail(ctx context.Context, id, workerID, errorCode, userError, diagnostic string, retryable bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin fail: %w", err)
	}
	defer tx.Rollback()
	var attempts, maxAttempts int
	if err := tx.QueryRowContext(ctx, `SELECT attempts,max_attempts FROM jobs WHERE id=? AND lease_owner=?`, id, workerID).Scan(&attempts, &maxAttempts); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read attempt count: %w", err)
	}
	status := domain.JobFailed
	available := time.Now().UTC()
	completed := any(utcText(available))
	if retryable && attempts < maxAttempts {
		status = domain.JobQueued
		delay := time.Second * time.Duration(1<<min(attempts, 8))
		delay += time.Duration(rand.Int64N(max(int64(delay/5), 1)))
		available = available.Add(delay)
		completed = nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET status=?,progress=0,available_at=?,lease_owner=NULL,lease_until=NULL,error_code=?,user_error=?,diagnostic=?,completed_at=?,updated_at=? WHERE id=?`, status, utcText(available), errorCode, userError, tail(diagnostic, 8192), completed, utcText(time.Now()), id)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	outcome := "failed"
	if status == domain.JobQueued {
		outcome = "retrying"
	}
	_, err = tx.ExecContext(ctx, `UPDATE job_attempts SET completed_at=?,outcome=?,error_code=?,diagnostic=? WHERE job_id=? AND attempt=?`, utcText(time.Now()), outcome, errorCode, tail(diagnostic, 4096), id, attempts)
	if err != nil {
		return fmt.Errorf("complete failed attempt: %w", err)
	}
	return tx.Commit()
}

func (s *Store) ParkClaim(ctx context.Context, id, workerID, errorCode, userError string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin park job: %w", err)
	}
	defer tx.Rollback()
	var attempt int
	if err := tx.QueryRowContext(ctx, `SELECT attempts FROM jobs WHERE id=? AND lease_owner=?`, id, workerID).Scan(&attempt); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read parked attempt: %w", err)
	}
	now := utcText(time.Now())
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET status=?,available_at=?,lease_owner=NULL,lease_until=NULL,
error_code=?,user_error=?,diagnostic='',completed_at=NULL,max_attempts=MAX(max_attempts,attempts+1),updated_at=?
WHERE id=? AND lease_owner=?`, domain.JobQueued, now, errorCode, userError, now, id, workerID)
	if err != nil {
		return fmt.Errorf("park job: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE job_attempts SET completed_at=?,outcome='deferred',error_code=?,diagnostic=? WHERE job_id=? AND attempt=?`, now, errorCode, userError, id, attempt)
	if err != nil {
		return fmt.Errorf("complete parked attempt: %w", err)
	}
	return tx.Commit()
}

func (s *Store) Cancel(ctx context.Context, id string) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET cancel_requested=1,status=CASE WHEN status=? THEN ? ELSE status END,completed_at=CASE WHEN status=? THEN ? ELSE completed_at END,updated_at=? WHERE id=? AND status NOT IN (?,?,?)`, domain.JobQueued, domain.JobCancelled, domain.JobQueued, utcText(now), utcText(now), id, domain.JobSucceeded, domain.JobFailed, domain.JobCancelled)
	if err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) IsCancelRequested(ctx context.Context, id string) (bool, error) {
	var value bool
	if err := s.db.QueryRowContext(ctx, `SELECT cancel_requested FROM jobs WHERE id=?`, id).Scan(&value); errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	} else if err != nil {
		return false, fmt.Errorf("read cancel flag: %w", err)
	}
	return value, nil
}

func (s *Store) Retry(ctx context.Context, id string) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?,progress=0,cancel_requested=0,available_at=?,lease_owner=NULL,lease_until=NULL,error_code='',user_error='',diagnostic='',completed_at=NULL,updated_at=? WHERE id=? AND status IN (?,?) AND attempts<max_attempts`, domain.JobQueued, utcText(now), utcText(now), id, domain.JobFailed, domain.JobInterrupted)
	if err != nil {
		return fmt.Errorf("retry job: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SaveArtifact(ctx context.Context, artifact domain.Artifact) error {
	requested, _ := json.Marshal(artifact.Requested)
	final, _ := json.Marshal(artifact.Final)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin artifact: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,job_id,path,sha256,size_bytes,width,height,frame_count,duration_ms,requested_json,final_json,original_size_bytes,compression_attempts,degraded,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, artifact.ID, artifact.JobID, artifact.Path, artifact.SHA256, artifact.SizeBytes, artifact.Width, artifact.Height, artifact.FrameCount, artifact.Duration.Milliseconds(), string(requested), string(final), artifact.OriginalSizeBytes, artifact.CompressionAttempts, artifact.Degraded, utcText(artifact.ExpiresAt), utcText(artifact.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert artifact: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET artifact_id=?,artifact_path=?,artifact_sha256=?,artifact_bytes=?,final_json=?,updated_at=? WHERE id=?`, artifact.ID, artifact.Path, artifact.SHA256, artifact.SizeBytes, string(final), utcText(time.Now()), artifact.JobID)
	if err != nil {
		return fmt.Errorf("attach artifact: %w", err)
	}
	return tx.Commit()
}

func (s *Store) FindReusableArtifact(ctx context.Context, dedupeKey string) (*domain.Artifact, error) {
	var artifact domain.Artifact
	var requested, final, expires, created string
	var durationMS int64
	err := s.db.QueryRowContext(ctx, `SELECT
  a.id,a.job_id,a.path,a.sha256,a.size_bytes,a.width,a.height,a.frame_count,a.duration_ms,
  a.requested_json,a.final_json,a.original_size_bytes,a.compression_attempts,a.degraded,a.expires_at,a.created_at
FROM artifacts a
JOIN jobs j ON j.artifact_id=a.id
WHERE j.dedupe_key=? AND a.expires_at>?
ORDER BY a.created_at DESC LIMIT 1`, dedupeKey, utcText(time.Now())).Scan(
		&artifact.ID, &artifact.JobID, &artifact.Path, &artifact.SHA256, &artifact.SizeBytes,
		&artifact.Width, &artifact.Height, &artifact.FrameCount, &durationMS, &requested, &final,
		&artifact.OriginalSizeBytes, &artifact.CompressionAttempts, &artifact.Degraded, &expires, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find reusable artifact: %w", err)
	}
	if err := json.Unmarshal([]byte(requested), &artifact.Requested); err != nil {
		return nil, fmt.Errorf("decode reusable artifact request: %w", err)
	}
	if err := json.Unmarshal([]byte(final), &artifact.Final); err != nil {
		return nil, fmt.Errorf("decode reusable artifact result: %w", err)
	}
	artifact.Duration = time.Duration(durationMS) * time.Millisecond
	if artifact.ExpiresAt, err = parseTime(expires); err != nil {
		return nil, err
	}
	if artifact.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	return &artifact, nil
}

func (s *Store) AttachReusableArtifact(ctx context.Context, jobID, workerID string, artifact domain.Artifact) error {
	final, err := json.Marshal(artifact.Final)
	if err != nil {
		return fmt.Errorf("marshal reusable artifact params: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE jobs
SET artifact_id=?,artifact_path=?,artifact_sha256=?,artifact_bytes=?,final_json=?,updated_at=?
WHERE id=? AND lease_owner=?`, artifact.ID, artifact.Path, artifact.SHA256, artifact.SizeBytes, string(final), utcText(time.Now()), jobID, workerID)
	if err != nil {
		return fmt.Errorf("attach reusable artifact: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) QueueDepth(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status=?`, domain.JobQueued).Scan(&count); err != nil {
		return 0, fmt.Errorf("queue depth: %w", err)
	}
	return count, nil
}

func (s *Store) ActiveJobs(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status NOT IN (?,?,?,?)`, domain.JobQueued, domain.JobSucceeded, domain.JobFailed, domain.JobCancelled).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("active jobs: %w", err)
	}
	return count, nil
}

func (s *Store) FindJobByDedupe(ctx context.Context, source domain.JobSource, key string) (*domain.Job, error) {
	row := s.db.QueryRowContext(ctx, jobSelect+` WHERE source=? AND dedupe_key=? AND (
status NOT IN (?,?,?) OR (status=? AND artifact_id IN (SELECT id FROM artifacts WHERE expires_at>?))
) ORDER BY created_at DESC LIMIT 1`, source, key, domain.JobSucceeded, domain.JobFailed, domain.JobCancelled, domain.JobSucceeded, utcText(time.Now()))
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find deduplicated job: %w", err)
	}
	return job, nil
}

type ExpiredArtifact struct {
	ID, Path  string
	SizeBytes int64
}

func (s *Store) ExpiredArtifacts(ctx context.Context, before time.Time, limit int) ([]ExpiredArtifact, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,path,size_bytes FROM artifacts WHERE expires_at<? ORDER BY expires_at LIMIT ?`, utcText(before), limit)
	if err != nil {
		return nil, fmt.Errorf("list expired artifacts: %w", err)
	}
	defer rows.Close()
	var result []ExpiredArtifact
	for rows.Next() {
		var item ExpiredArtifact
		if err := rows.Scan(&item.ID, &item.Path, &item.SizeBytes); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Store) OldestArtifacts(ctx context.Context, limit int) ([]ExpiredArtifact, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,path,size_bytes FROM artifacts ORDER BY created_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list oldest artifacts: %w", err)
	}
	defer rows.Close()
	var result []ExpiredArtifact
	for rows.Next() {
		var item ExpiredArtifact
		if err := rows.Scan(&item.ID, &item.Path, &item.SizeBytes); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Store) ArtifactStorageBytes(ctx context.Context) (uint64, error) {
	var value int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes),0) FROM artifacts`).Scan(&value); err != nil {
		return 0, fmt.Errorf("sum artifact bytes: %w", err)
	}
	if value < 0 {
		return 0, errors.New("artifact byte total is negative")
	}
	return uint64(value), nil
}
func (s *Store) HasArtifactPath(ctx context.Context, path string) (bool, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts WHERE path=?)`, path).Scan(&exists); err != nil {
		return false, fmt.Errorf("check artifact path: %w", err)
	}
	return exists != 0, nil
}
func (s *Store) DeleteArtifact(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET artifact_id=NULL,artifact_path='',artifact_sha256='',artifact_bytes=0 WHERE artifact_id=?`, id); err != nil {
		return fmt.Errorf("detach artifact: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM artifacts WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return tx.Commit()
}
func (s *Store) CleanupExpiredCaches(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM video_cache WHERE expires_at<?`, utcText(now)); err != nil {
		return fmt.Errorf("clean video cache: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM danmaku_cache WHERE expires_at<?`, utcText(now)); err != nil {
		return fmt.Errorf("clean danmaku cache: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM rate_limit_usage WHERE window_start<?`, utcText(now.Add(-48*time.Hour))); err != nil {
		return fmt.Errorf("clean rate limits: %w", err)
	}
	return nil
}

func (s *Store) ClearCaches(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clear caches: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM video_cache`); err != nil {
		return fmt.Errorf("clear video cache: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM danmaku_cache`); err != nil {
		return fmt.Errorf("clear danmaku cache: %w", err)
	}
	return tx.Commit()
}

func (s *Store) CacheGet(ctx context.Context, table, key string) ([]byte, bool, error) {
	if table != "video_cache" && table != "danmaku_cache" {
		return nil, false, errors.New("invalid cache table")
	}
	column := "value_json"
	if table == "danmaku_cache" {
		column = "value"
	}
	query := `SELECT ` + column + ` FROM ` + table + ` WHERE cache_key=? AND expires_at>?`
	var value []byte
	if err := s.db.QueryRowContext(ctx, query, key, utcText(time.Now())).Scan(&value); errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("read cache: %w", err)
	}
	return value, true, nil
}
func (s *Store) CacheSet(ctx context.Context, table, key string, value []byte, ttl time.Duration) error {
	if table != "video_cache" && table != "danmaku_cache" {
		return errors.New("invalid cache table")
	}
	column := "value_json"
	if table == "danmaku_cache" {
		column = "value"
	}
	query := `INSERT INTO ` + table + `(cache_key,` + column + `,expires_at,created_at) VALUES(?,?,?,?) ON CONFLICT(cache_key) DO UPDATE SET ` + column + `=excluded.` + column + `,expires_at=excluded.expires_at,created_at=excluded.created_at`
	_, err := s.db.ExecContext(ctx, query, key, value, utcText(time.Now().Add(ttl)), utcText(time.Now()))
	if err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}

func (s *Store) TakeRateLimit(ctx context.Context, scope, subject string, window time.Duration, limit int) (bool, int, error) {
	if limit < 1 || window < time.Second || scope == "" || subject == "" {
		return false, 0, errors.New("rate-limit scope, subject, window and limit must be valid")
	}
	now := time.Now().UTC()
	unix := now.Unix()
	start := time.Unix(unix-(unix%int64(window.Seconds())), 0).UTC()
	startText := utcText(start)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, fmt.Errorf("begin rate limit: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO rate_limit_usage(scope,subject,window_start,count,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(scope,subject,window_start) DO UPDATE SET count=count+1,updated_at=excluded.updated_at`, scope, subject, startText, 1, utcText(now))
	if err != nil {
		return false, 0, fmt.Errorf("increment rate limit: %w", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count FROM rate_limit_usage WHERE scope=? AND subject=? AND window_start=?`, scope, subject, startText).Scan(&count); err != nil {
		return false, 0, fmt.Errorf("read rate limit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, 0, fmt.Errorf("commit rate limit: %w", err)
	}
	return count <= limit, count, nil
}

func (s *Store) LoadSetting(ctx context.Context, key string) ([]byte, bool, error) {
	if strings.TrimSpace(key) == "" {
		return nil, false, errors.New("setting key is required")
	}
	var value []byte
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=? AND sensitive=0`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load setting: %w", err)
	}
	return value, true, nil
}

func (s *Store) SaveSetting(ctx context.Context, key string, value []byte) error {
	if strings.TrimSpace(key) == "" || len(value) == 0 || !json.Valid(value) {
		return errors.New("setting key and valid JSON value are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value_json,sensitive,updated_at) VALUES(?,?,0,?)
ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,sensitive=0,updated_at=excluded.updated_at`, key, value, utcText(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("save setting: %w", err)
	}
	return nil
}

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("setting key is required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=? AND sensitive=0`, key)
	if err != nil {
		return fmt.Errorf("delete setting: %w", err)
	}
	return nil
}

const jobSelect = `SELECT id,source,creator_mid,creator_name,anonymous_id,COALESCE(notification_id,''),source_comment_rpid,aid,bvid,cid,page,start_ms,end_ms,requested_json,final_json,dedupe_key,status,progress,attempts,max_attempts,available_at,COALESCE(lease_owner,''),lease_until,cancel_requested,error_code,user_error,diagnostic,temp_dir,COALESCE(artifact_id,''),artifact_path,artifact_sha256,artifact_bytes,bili_image_url,published_rpid,publish_status,created_at,started_at,completed_at,updated_at FROM jobs`

type scanner interface{ Scan(dest ...any) error }

func scanJob(row scanner) (*domain.Job, error) {
	var j domain.Job
	var startMS, endMS int64
	var requested, final string
	var available, created, updated string
	var lease, started, completed sql.NullString
	err := row.Scan(&j.ID, &j.Source, &j.CreatorMID, &j.CreatorName, &j.AnonymousID, &j.NotificationID, &j.SourceCommentRPID, &j.Aid, &j.BVID, &j.CID, &j.Page, &startMS, &endMS, &requested, &final, &j.DedupeKey, &j.Status, &j.Progress, &j.Attempts, &j.MaxAttempts, &available, &j.LeaseOwner, &lease, &j.CancelRequested, &j.ErrorCode, &j.UserError, &j.Diagnostic, &j.TempDir, &j.ArtifactID, &j.ArtifactPath, &j.ArtifactSHA256, &j.ArtifactBytes, &j.BiliImageURL, &j.PublishedRPID, &j.PublishStatus, &created, &started, &completed, &updated)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(requested), &j.Requested); err != nil {
		return nil, fmt.Errorf("decode requested params: %w", err)
	}
	if err := json.Unmarshal([]byte(final), &j.Final); err != nil {
		return nil, fmt.Errorf("decode final params: %w", err)
	}
	j.Start, j.End = time.Duration(startMS)*time.Millisecond, time.Duration(endMS)*time.Millisecond
	if j.AvailableAt, err = parseTime(available); err != nil {
		return nil, err
	}
	if j.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if j.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	if lease.Valid {
		value, err := parseTime(lease.String)
		if err != nil {
			return nil, err
		}
		j.LeaseUntil = &value
	}
	if started.Valid {
		value, err := parseTime(started.String)
		if err != nil {
			return nil, err
		}
		j.StartedAt = &value
	}
	if completed.Valid {
		value, err := parseTime(completed.String)
		if err != nil {
			return nil, err
		}
		j.CompletedAt = &value
	}
	return &j, nil
}

func utcText(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse database timestamp: %w", err)
	}
	return t, nil
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func tail(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	return value[len(value)-maxLen:]
}
