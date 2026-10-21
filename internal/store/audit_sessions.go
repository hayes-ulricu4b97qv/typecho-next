package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (r *SQLiteRepository) RecordAudit(event AuditEvent) error {
	if event.ID == "" {
		event.ID = newAuditID()
	}
	if event.ActorUsername == "" {
		event.ActorUsername = "system"
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	details := ""
	if len(event.Details) > 0 {
		encoded, err := json.Marshal(event.Details)
		if err != nil {
			return fmt.Errorf("encode audit details: %w", err)
		}
		details = string(encoded)
	}
	_, err := r.db.Exec(`INSERT INTO audit_events(id, actor_user_id, actor_username, action, resource_type, resource_id, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, event.ID, event.ActorUserID, event.ActorUsername, event.Action, event.ResourceType, event.ResourceID, details, formatTime(event.CreatedAt))
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) ListAudit(query AuditQuery) (AuditPage, error) {
	return r.ListAuditContext(context.Background(), query)
}

func (r *SQLiteRepository) ListAuditContext(ctx context.Context, query AuditQuery) (AuditPage, error) {
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize)
	where := "1 = 1"
	args := make([]any, 0, 2)
	if strings.TrimSpace(query.Action) != "" {
		where += " AND action = ?"
		args = append(args, strings.TrimSpace(query.Action))
	}
	if strings.TrimSpace(query.ResourceType) != "" {
		where += " AND resource_type = ?"
		args = append(args, strings.TrimSpace(query.ResourceType))
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE `+where, args...).Scan(&total); err != nil {
		return AuditPage{}, fmt.Errorf("count audit events: %w", err)
	}
	offset := pageOffset(query.Page, query.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT id, actor_user_id, actor_username, action, resource_type, resource_id, details, created_at
		FROM audit_events WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, append(args, query.PageSize, offset)...)
	if err != nil {
		return AuditPage{}, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	items := make([]AuditEvent, 0, query.PageSize)
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return AuditPage{}, fmt.Errorf("scan audit event: %w", err)
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return AuditPage{}, fmt.Errorf("iterate audit events: %w", err)
	}
	return AuditPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (r *SQLiteRepository) Stats() (PostStats, error) {
	return r.StatsContext(context.Background())
}

func (r *SQLiteRepository) StatsContext(ctx context.Context) (PostStats, error) {
	var stats PostStats
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status = 'published' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'draft' THEN 1 ELSE 0 END), 0),
		(SELECT COUNT(*) FROM comments),
		(SELECT COUNT(*) FROM comments WHERE status = 'pending')
		FROM posts`).Scan(&stats.Posts, &stats.PublishedPosts, &stats.Drafts, &stats.Comments, &stats.PendingComments)
	if err != nil {
		return PostStats{}, fmt.Errorf("read post stats: %w", err)
	}
	return stats, nil
}

func (r *SQLiteRepository) CreateSession(username, tokenHash string, expiresAt time.Time) error {
	return r.CreateSessionContext(context.Background(), username, tokenHash, expiresAt)
}

func (r *SQLiteRepository) CreateSessionContext(ctx context.Context, username, tokenHash string, expiresAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `INSERT INTO sessions(token_hash, user_id, username, expires_at, created_at)
		SELECT ?, id, username, ?, ? FROM users WHERE username = ? AND active = 1`, tokenHash, formatTime(expiresAt), formatTime(time.Now().UTC()), username)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read created session count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) CreateAuthenticatedSessionContext(ctx context.Context, username, passwordHash, tokenHash string, expiresAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `INSERT INTO sessions(token_hash, user_id, username, expires_at, created_at)
		SELECT ?, id, username, ?, ? FROM users WHERE username = ? AND active = 1 AND password_hash = ?`,
		tokenHash, formatTime(expiresAt), formatTime(time.Now().UTC()), username, passwordHash)
	if err != nil {
		return fmt.Errorf("create authenticated session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read authenticated session count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) ResolveSession(tokenHash string, now time.Time) (string, error) {
	return r.ResolveSessionContext(context.Background(), tokenHash, now)
}

func (r *SQLiteRepository) ResolveSessionContext(ctx context.Context, tokenHash string, now time.Time) (string, error) {
	var username, expiresAt string
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(u.username, s.username), s.expires_at
		FROM sessions s LEFT JOIN users u ON u.id = NULLIF(s.user_id, '')
		WHERE s.token_hash = ?`, tokenHash).Scan(&username, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read session: %w", err)
	}
	if !parseTime(expiresAt).After(now) {
		return "", ErrNotFound
	}
	return username, nil
}

func (r *SQLiteRepository) DeleteSession(tokenHash string) error {
	if _, err := r.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) DeleteUserSessions(username string) error {
	if _, err := r.db.Exec(`DELETE FROM sessions
		WHERE username = ? OR user_id = (SELECT id FROM users WHERE username = ?)`, username, username); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) DeleteOtherUserSessions(username, tokenHash string) error {
	if _, err := r.db.Exec(`DELETE FROM sessions
		WHERE (username = ? OR user_id = (SELECT id FROM users WHERE username = ?))
		AND token_hash <> ?`, username, username, tokenHash); err != nil {
		return fmt.Errorf("delete other user sessions: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) PurgeSessions(now time.Time) error {
	return r.PurgeSessionsContext(context.Background(), now)
}

func (r *SQLiteRepository) PurgeSessionsContext(ctx context.Context, now time.Time) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, formatTime(now)); err != nil {
		return fmt.Errorf("purge sessions: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}
