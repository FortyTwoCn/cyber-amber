package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/domain"
)

type AccountRecord struct {
	MID                                              int64
	Nickname                                         string
	Encrypted, Nonce, RefreshEncrypted, RefreshNonce []byte
	LoggedIn                                         bool
	LastChecked                                      *time.Time
	LastError                                        string
	CreatedAt, UpdatedAt                             time.Time
}

func (s *Store) SaveAccount(ctx context.Context, a AccountRecord) error {
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO bili_accounts(id,mid,nickname,encrypted_credentials,credential_nonce,refresh_token_encrypted,refresh_token_nonce,logged_in,last_checked_at,last_error_code,created_at,updated_at) VALUES(1,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET mid=excluded.mid,nickname=excluded.nickname,encrypted_credentials=excluded.encrypted_credentials,credential_nonce=excluded.credential_nonce,refresh_token_encrypted=excluded.refresh_token_encrypted,refresh_token_nonce=excluded.refresh_token_nonce,logged_in=excluded.logged_in,last_checked_at=excluded.last_checked_at,last_error_code=excluded.last_error_code,updated_at=excluded.updated_at`, a.MID, a.Nickname, a.Encrypted, a.Nonce, a.RefreshEncrypted, a.RefreshNonce, a.LoggedIn, nullableTime(a.LastChecked), a.LastError, utcText(a.CreatedAt), utcText(now))
	if err != nil {
		return fmt.Errorf("save bili account: %w", err)
	}
	return nil
}
func (s *Store) LoadAccount(ctx context.Context) (AccountRecord, error) {
	var a AccountRecord
	var checked sql.NullString
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT mid,nickname,encrypted_credentials,credential_nonce,refresh_token_encrypted,refresh_token_nonce,logged_in,last_checked_at,last_error_code,created_at,updated_at FROM bili_accounts WHERE id=1`).Scan(&a.MID, &a.Nickname, &a.Encrypted, &a.Nonce, &a.RefreshEncrypted, &a.RefreshNonce, &a.LoggedIn, &checked, &a.LastError, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("load bili account: %w", err)
	}
	var parseErr error
	if a.CreatedAt, parseErr = parseTime(created); parseErr != nil {
		return a, parseErr
	}
	if a.UpdatedAt, parseErr = parseTime(updated); parseErr != nil {
		return a, parseErr
	}
	if checked.Valid {
		value, err := parseTime(checked.String)
		if err != nil {
			return a, err
		}
		a.LastChecked = &value
	}
	return a, nil
}

func (s *Store) MarkAccountInvalid(ctx context.Context, code string) error {
	if len(code) > 128 {
		code = code[:128]
	}
	now := utcText(time.Now())
	_, err := s.db.ExecContext(ctx, `UPDATE bili_accounts SET logged_in=0,last_checked_at=?,last_error_code=?,updated_at=? WHERE id=1`, now, code, now)
	if err != nil {
		return fmt.Errorf("mark bili account invalid: %w", err)
	}
	return nil
}

type CursorRecord struct {
	Source      string
	ID, Time    int64
	Initialized bool
	LastSuccess *time.Time
	LastError   string
	UpdatedAt   time.Time
}

func (s *Store) LoadCursor(ctx context.Context, source string) (CursorRecord, error) {
	var c CursorRecord
	var success sql.NullString
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT source,cursor_id,cursor_time,initialized,last_success_at,last_error,updated_at FROM notification_cursors WHERE source=?`, source).Scan(&c.Source, &c.ID, &c.Time, &c.Initialized, &success, &c.LastError, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return CursorRecord{Source: source}, nil
	}
	if err != nil {
		return c, fmt.Errorf("load cursor: %w", err)
	}
	c.UpdatedAt, _ = parseTime(updated)
	if success.Valid {
		value, err := parseTime(success.String)
		if err != nil {
			return c, err
		}
		c.LastSuccess = &value
	}
	return c, nil
}

func (s *Store) RecordCursorError(ctx context.Context, source, message string) error {
	if len(message) > 1024 {
		message = message[:1024]
	}
	now := utcText(time.Now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_cursors(source,cursor_id,cursor_time,initialized,last_error,updated_at) VALUES(?,0,0,0,?,?) ON CONFLICT(source) DO UPDATE SET last_error=excluded.last_error,updated_at=excluded.updated_at`, source, message, now)
	if err != nil {
		return fmt.Errorf("record cursor error: %w", err)
	}
	return nil
}

type MentionRecord struct {
	ID, NotificationID                    string
	OccurredAt                            time.Time
	SenderMID                             int64
	SenderName, SenderAvatar, Message     string
	SubjectID, RootID, SourceID, TargetID int64
	BusinessType                          int
	URI                                   string
	AID                                   int64
	BVID                                  string
	RPID, RootRPID                        int64
	RawJSON, Status, ErrorCode            string
}

func (s *Store) PendingMentions(ctx context.Context, limit int) ([]MentionRecord, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,notification_id,occurred_at,sender_mid,sender_name,sender_avatar,message,subject_id,root_id,source_id,target_id,business_type,uri,aid,bvid,rpid,root_rpid,raw_json,status,error_code FROM mention_events WHERE status='received' ORDER BY occurred_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending mentions: %w", err)
	}
	defer rows.Close()
	result := make([]MentionRecord, 0, limit)
	for rows.Next() {
		var m MentionRecord
		var occurred string
		if err := rows.Scan(&m.ID, &m.NotificationID, &occurred, &m.SenderMID, &m.SenderName, &m.SenderAvatar, &m.Message, &m.SubjectID, &m.RootID, &m.SourceID, &m.TargetID, &m.BusinessType, &m.URI, &m.AID, &m.BVID, &m.RPID, &m.RootRPID, &m.RawJSON, &m.Status, &m.ErrorCode); err != nil {
			return nil, fmt.Errorf("scan pending mention: %w", err)
		}
		m.OccurredAt, err = parseTime(occurred)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending mention rows: %w", err)
	}
	return result, nil
}

// ListMentions returns recent mention deliveries for administrator diagnostics.
// Raw notification JSON is deliberately kept in the store and is not exposed by
// the HTTP handler.
func (s *Store) ListMentions(ctx context.Context, limit int) ([]MentionRecord, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,notification_id,occurred_at,sender_mid,sender_name,sender_avatar,message,subject_id,root_id,source_id,target_id,business_type,uri,aid,bvid,rpid,root_rpid,raw_json,status,error_code FROM mention_events ORDER BY occurred_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list mentions: %w", err)
	}
	defer rows.Close()
	result := make([]MentionRecord, 0, limit)
	for rows.Next() {
		var m MentionRecord
		var occurred string
		if err := rows.Scan(&m.ID, &m.NotificationID, &occurred, &m.SenderMID, &m.SenderName, &m.SenderAvatar, &m.Message, &m.SubjectID, &m.RootID, &m.SourceID, &m.TargetID, &m.BusinessType, &m.URI, &m.AID, &m.BVID, &m.RPID, &m.RootRPID, &m.RawJSON, &m.Status, &m.ErrorCode); err != nil {
			return nil, fmt.Errorf("scan mention: %w", err)
		}
		m.OccurredAt, err = parseTime(occurred)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mention rows: %w", err)
	}
	return result, nil
}

func (s *Store) GetMention(ctx context.Context, notificationID string) (MentionRecord, error) {
	var m MentionRecord
	var occurred string
	err := s.db.QueryRowContext(ctx, `SELECT id,notification_id,occurred_at,sender_mid,sender_name,sender_avatar,message,subject_id,root_id,source_id,target_id,business_type,uri,aid,bvid,rpid,root_rpid,raw_json,status,error_code FROM mention_events WHERE notification_id=?`, notificationID).Scan(&m.ID, &m.NotificationID, &occurred, &m.SenderMID, &m.SenderName, &m.SenderAvatar, &m.Message, &m.SubjectID, &m.RootID, &m.SourceID, &m.TargetID, &m.BusinessType, &m.URI, &m.AID, &m.BVID, &m.RPID, &m.RootRPID, &m.RawJSON, &m.Status, &m.ErrorCode)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, fmt.Errorf("get mention: %w", err)
	}
	m.OccurredAt, err = parseTime(occurred)
	return m, err
}

func (s *Store) SetJobPublishStatus(ctx context.Context, id, status string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET publish_status=?,updated_at=? WHERE id=?`, status, utcText(time.Now()), id)
	if err != nil {
		return fmt.Errorf("set job publish status: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SaveMentionsAndCursor(ctx context.Context, mentions []MentionRecord, cursor CursorRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mention transaction: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	for _, m := range mentions {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO mention_events(id,notification_id,occurred_at,sender_mid,sender_name,sender_avatar,message,subject_id,root_id,source_id,target_id,business_type,uri,aid,bvid,rpid,root_rpid,raw_json,status,error_code,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.NotificationID, utcText(m.OccurredAt), m.SenderMID, m.SenderName, m.SenderAvatar, m.Message, m.SubjectID, m.RootID, m.SourceID, m.TargetID, m.BusinessType, m.URI, m.AID, m.BVID, m.RPID, m.RootRPID, m.RawJSON, m.Status, m.ErrorCode, utcText(now))
		if err != nil {
			return fmt.Errorf("save mention %s: %w", m.NotificationID, err)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO notification_cursors(source,cursor_id,cursor_time,initialized,last_success_at,last_error,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(source) DO UPDATE SET cursor_id=excluded.cursor_id,cursor_time=excluded.cursor_time,initialized=excluded.initialized,last_success_at=excluded.last_success_at,last_error=excluded.last_error,updated_at=excluded.updated_at`, cursor.Source, cursor.ID, cursor.Time, cursor.Initialized, utcText(now), cursor.LastError, utcText(now))
	if err != nil {
		return fmt.Errorf("save mention cursor: %w", err)
	}
	return tx.Commit()
}

func (s *Store) MarkMention(ctx context.Context, notificationID, status, errorCode string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE mention_events SET status=?,error_code=? WHERE notification_id=?`, status, errorCode, notificationID)
	if err != nil {
		return fmt.Errorf("mark mention: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateJobVideo(ctx context.Context, id, workerID string, aid int64, bvid string, cid int64, page int) error {
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET aid=?,bvid=?,cid=?,page=?,updated_at=? WHERE id=? AND lease_owner=?`, aid, bvid, cid, page, utcText(time.Now()), id, workerID)
	if err != nil {
		return fmt.Errorf("update job video: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateJobDedupeKey(ctx context.Context, id, workerID, dedupeKey string) error {
	if dedupeKey == "" {
		return errors.New("dedupe key is required")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET dedupe_key=?,updated_at=? WHERE id=? AND lease_owner=?`, dedupeKey, utcText(time.Now()), id, workerID)
	if err != nil {
		return fmt.Errorf("update job dedupe key: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateJobTempDir(ctx context.Context, id, workerID, tempDir string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET temp_dir=?,updated_at=? WHERE id=? AND lease_owner=?`, tempDir, utcText(time.Now()), id, workerID)
	if err != nil {
		return fmt.Errorf("update job temp directory: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

type PublishedRecord struct {
	JobID                                      string
	AID, RPID                                  int64
	ImageURL, TaskMarker, Status, ResponseJSON string
	VerifiedAt                                 *time.Time
}

func (s *Store) SavePublished(ctx context.Context, p PublishedRecord) error {
	allowedStatus := map[string]bool{"published": true, "published_unverified": true, "pending_review": true, "self_only_suspected": true, "deleted": true, "failed": true}
	if p.JobID == "" || p.AID <= 0 || p.RPID <= 0 || p.TaskMarker == "" || !allowedStatus[p.Status] {
		return errors.New("invalid published comment record")
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin published comment: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO published_comments(job_id,aid,rpid,image_url,task_marker,status,verified_at,response_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(job_id) DO UPDATE SET rpid=excluded.rpid,image_url=excluded.image_url,status=excluded.status,verified_at=excluded.verified_at,response_json=excluded.response_json,updated_at=excluded.updated_at`, p.JobID, p.AID, p.RPID, p.ImageURL, p.TaskMarker, p.Status, nullableTime(p.VerifiedAt), p.ResponseJSON, utcText(now), utcText(now))
	if err != nil {
		return fmt.Errorf("save published comment: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET bili_image_url=?,published_rpid=?,publish_status=?,updated_at=? WHERE id=?`, p.ImageURL, p.RPID, p.Status, utcText(now), p.JobID)
	if err != nil {
		return fmt.Errorf("attach published comment: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return tx.Commit()
}
func (s *Store) LoadPublished(ctx context.Context, jobID string) (PublishedRecord, error) {
	var p PublishedRecord
	var verified sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT job_id,aid,rpid,image_url,task_marker,status,response_json,verified_at FROM published_comments WHERE job_id=?`, jobID).Scan(&p.JobID, &p.AID, &p.RPID, &p.ImageURL, &p.TaskMarker, &p.Status, &p.ResponseJSON, &verified)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("load published: %w", err)
	}
	if verified.Valid {
		value, err := parseTime(verified.String)
		if err != nil {
			return p, err
		}
		p.VerifiedAt = &value
	}
	return p, nil
}

type BotState struct {
	Paused        bool
	CircuitReason string
	CircuitUntil  *time.Time
	UpdatedAt     time.Time
}

func (s *Store) BotState(ctx context.Context) (BotState, error) {
	var b BotState
	var until sql.NullString
	var updated string
	if err := s.db.QueryRowContext(ctx, `SELECT paused,circuit_reason,circuit_until,updated_at FROM bot_state WHERE id=1`).Scan(&b.Paused, &b.CircuitReason, &until, &updated); err != nil {
		return b, fmt.Errorf("load bot state: %w", err)
	}
	b.UpdatedAt, _ = parseTime(updated)
	if until.Valid {
		value, err := parseTime(until.String)
		if err != nil {
			return b, err
		}
		b.CircuitUntil = &value
	}
	return b, nil
}
func (s *Store) SetBotPaused(ctx context.Context, paused bool, reason string, until *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE bot_state SET paused=?,circuit_reason=?,circuit_until=?,updated_at=? WHERE id=1`, paused, reason, nullableTime(until), utcText(time.Now()))
	if err != nil {
		return fmt.Errorf("update bot state: %w", err)
	}
	return nil
}

func (s *Store) IsBlocked(ctx context.Context, subjectType, subjectID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blocked_users WHERE subject_type=? AND subject_id=? AND (expires_at IS NULL OR expires_at>?)`, subjectType, subjectID, utcText(time.Now())).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check blocklist: %w", err)
	}
	return count > 0, nil
}

func (s *Store) SetBlocked(ctx context.Context, subjectType, subjectID, reason string, expiresAt *time.Time) error {
	if subjectType != "bili_mid" && subjectType != "ip" {
		return errors.New("invalid block subject type")
	}
	if subjectID == "" || reason == "" {
		return errors.New("block subject and reason are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO blocked_users(subject_type,subject_id,reason,expires_at,created_at) VALUES(?,?,?,?,?) ON CONFLICT(subject_type,subject_id) DO UPDATE SET reason=excluded.reason,expires_at=excluded.expires_at,created_at=excluded.created_at`, subjectType, subjectID, reason, nullableTime(expiresAt), utcText(time.Now()))
	if err != nil {
		return fmt.Errorf("set blocked user: %w", err)
	}
	return nil
}

type BlockedRecord struct {
	SubjectType string     `json:"subject_type"`
	SubjectID   string     `json:"subject_id"`
	Reason      string     `json:"reason"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (s *Store) ListBlocked(ctx context.Context, limit int) ([]BlockedRecord, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT subject_type,subject_id,reason,expires_at,created_at FROM blocked_users ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list blocked users: %w", err)
	}
	defer rows.Close()
	result := make([]BlockedRecord, 0, limit)
	for rows.Next() {
		var item BlockedRecord
		var expires sql.NullString
		var created string
		if err := rows.Scan(&item.SubjectType, &item.SubjectID, &item.Reason, &expires, &created); err != nil {
			return nil, fmt.Errorf("scan blocked user: %w", err)
		}
		item.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		if expires.Valid {
			value, err := parseTime(expires.String)
			if err != nil {
				return nil, err
			}
			item.ExpiresAt = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) DeleteBlocked(ctx context.Context, subjectType, subjectID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM blocked_users WHERE subject_type=? AND subject_id=?`, subjectType, subjectID)
	if err != nil {
		return fmt.Errorf("delete blocked user: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ActiveJobsForMID(ctx context.Context, mid int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE creator_mid=? AND status NOT IN (?,?,?)`, mid, domain.JobSucceeded, domain.JobFailed, domain.JobCancelled).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count user active jobs: %w", err)
	}
	return count, nil
}

func (s *Store) ActiveJobsForAnonymous(ctx context.Context, anonymousID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE anonymous_id=? AND status NOT IN (?,?,?)`, anonymousID, domain.JobSucceeded, domain.JobFailed, domain.JobCancelled).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count anonymous active jobs: %w", err)
	}
	return count, nil
}

func (s *Store) Audit(ctx context.Context, actor, action, target, requestID string, detailsJSON []byte) error {
	if len(detailsJSON) == 0 {
		detailsJSON = []byte("{}")
	} else if !json.Valid(detailsJSON) {
		return errors.New("invalid audit JSON")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_logs(actor,action,target,request_id,details_json,created_at) VALUES(?,?,?,?,?,?)`, actor, action, target, requestID, string(detailsJSON), utcText(time.Now()))
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

type AuditRecord struct {
	ID        int64           `json:"id"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	RequestID string          `json:"request_id"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Store) ListAudits(ctx context.Context, limit int) ([]AuditRecord, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,actor,action,target,request_id,details_json,created_at FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()
	result := make([]AuditRecord, 0, limit)
	for rows.Next() {
		var item AuditRecord
		var details, created string
		if err := rows.Scan(&item.ID, &item.Actor, &item.Action, &item.Target, &item.RequestID, &details, &created); err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		item.Details = json.RawMessage(details)
		item.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return utcText(*value)
}
