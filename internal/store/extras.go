package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func (r *SQLiteRepository) snapshotPostContext(ctx context.Context, tx *sql.Tx, post Post) error {
	var title, excerpt, content, rawCustomFields string
	err := tx.QueryRowContext(ctx, `SELECT title, excerpt, content, custom_fields FROM posts WHERE id = ?`, post.ID).Scan(&title, &excerpt, &content, &rawCustomFields)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read post revision source: %w", err)
	}
	currentCustomFields, err := decodeCustomFields(rawCustomFields)
	if err != nil {
		return err
	}
	if title == post.Title && excerpt == post.Excerpt && content == post.Content && equalCustomFields(currentCustomFields, post.CustomFields) {
		return nil
	}
	customFields, err := encodeCustomFields(currentCustomFields)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO post_revisions(id, post_id, title, excerpt, content, custom_fields, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, newID(), post.ID, title, excerpt, content, customFields, formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("save post revision: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) ListPostRevisions(postID string) ([]PostRevision, error) {
	return r.ListPostRevisionsContext(context.Background(), postID)
}

func (r *SQLiteRepository) ListPostRevisionsContext(ctx context.Context, postID string) ([]PostRevision, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, post_id, title, excerpt, content, custom_fields, created_at FROM post_revisions WHERE post_id = ? ORDER BY created_at DESC, id DESC`, postID)
	if err != nil {
		return nil, fmt.Errorf("list post revisions: %w", err)
	}
	defer rows.Close()
	items := make([]PostRevision, 0)
	for rows.Next() {
		var item PostRevision
		var createdAt, rawCustomFields string
		if err := rows.Scan(&item.ID, &item.PostID, &item.Title, &item.Excerpt, &item.Content, &rawCustomFields, &createdAt); err != nil {
			return nil, fmt.Errorf("scan post revision: %w", err)
		}
		item.CustomFields, err = decodeCustomFields(rawCustomFields)
		if err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SQLiteRepository) ListPostRevisionPageContext(ctx context.Context, postID string, page, pageSize int) (PostRevisionPage, error) {
	page, pageSize = normalizePage(page, pageSize)
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM post_revisions WHERE post_id = ?`, postID).Scan(&total); err != nil {
		return PostRevisionPage{}, fmt.Errorf("count post revisions: %w", err)
	}
	offset := pageOffset(page, pageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT id, post_id, title, created_at
		FROM post_revisions WHERE post_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, postID, pageSize, offset)
	if err != nil {
		return PostRevisionPage{}, fmt.Errorf("list post revisions: %w", err)
	}
	defer rows.Close()
	items := make([]PostRevisionSummary, 0, pageSize)
	for rows.Next() {
		var item PostRevisionSummary
		var createdAt string
		if err := rows.Scan(&item.ID, &item.PostID, &item.Title, &createdAt); err != nil {
			return PostRevisionPage{}, fmt.Errorf("scan post revision: %w", err)
		}
		item.CreatedAt = parseTime(createdAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return PostRevisionPage{}, fmt.Errorf("iterate post revisions: %w", err)
	}
	return PostRevisionPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *SQLiteRepository) PostRevision(id string) (PostRevision, error) {
	return r.PostRevisionContext(context.Background(), id)
}

func (r *SQLiteRepository) PostRevisionContext(ctx context.Context, id string) (PostRevision, error) {
	var item PostRevision
	var createdAt, rawCustomFields string
	err := r.db.QueryRowContext(ctx, `SELECT id, post_id, title, excerpt, content, custom_fields, created_at FROM post_revisions WHERE id = ?`, id).Scan(&item.ID, &item.PostID, &item.Title, &item.Excerpt, &item.Content, &rawCustomFields, &createdAt)
	if err == sql.ErrNoRows {
		return PostRevision{}, ErrNotFound
	}
	if err != nil {
		return PostRevision{}, fmt.Errorf("read post revision: %w", err)
	}
	item.CustomFields, err = decodeCustomFields(rawCustomFields)
	if err != nil {
		return PostRevision{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func (r *SQLiteRepository) SaveOutboundMail(recipient, subject, body string) error {
	return r.SaveOutboundMailContext(context.Background(), recipient, subject, body)
}

func (r *SQLiteRepository) SaveOutboundMailContext(ctx context.Context, recipient, subject, body string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin outbound mail save: %w", err)
	}
	defer tx.Rollback()
	createdAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbound_mail(id, recipient, subject, body, created_at) VALUES (?, ?, ?, ?, ?)`, newID(), recipient, subject, body, formatTime(createdAt)); err != nil {
		return fmt.Errorf("save outbound mail: %w", err)
	}
	cutoff := createdAt.Add(-outboundMailRetention)
	if err := cleanupOutboundMailTx(ctx, tx, cutoff); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit outbound mail save: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) ListOutboundMail(page, pageSize int) (OutboundMailPage, error) {
	return r.ListOutboundMailContext(context.Background(), page, pageSize)
}

func (r *SQLiteRepository) ListOutboundMailContext(ctx context.Context, page, pageSize int) (OutboundMailPage, error) {
	page, pageSize = normalizePage(page, pageSize)
	cutoff := formatTime(time.Now().UTC().Add(-outboundMailRetention))
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbound_mail WHERE created_at >= ?`, cutoff).Scan(&total); err != nil {
		return OutboundMailPage{}, fmt.Errorf("count outbound mail: %w", err)
	}
	offset := pageOffset(page, pageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT id, recipient, subject, body, created_at FROM outbound_mail WHERE created_at >= ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, cutoff, pageSize, offset)
	if err != nil {
		return OutboundMailPage{}, fmt.Errorf("list outbound mail: %w", err)
	}
	defer rows.Close()
	items := make([]OutboundMail, 0)
	for rows.Next() {
		var item OutboundMail
		var createdAt string
		if err := rows.Scan(&item.ID, &item.Recipient, &item.Subject, &item.Body, &createdAt); err != nil {
			return OutboundMailPage{}, fmt.Errorf("scan outbound mail: %w", err)
		}
		item.CreatedAt = parseTime(createdAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return OutboundMailPage{}, err
	}
	return OutboundMailPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

const (
	outboundMailRetention  = 90 * 24 * time.Hour
	outboundMailMaxEntries = 1000
)

func (r *SQLiteRepository) CleanupOutboundMail(ctx context.Context, before time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin outbound mail cleanup: %w", err)
	}
	defer tx.Rollback()
	if err := cleanupOutboundMailTx(ctx, tx, before); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit outbound mail cleanup: %w", err)
	}
	return nil
}

func cleanupOutboundMailTx(ctx context.Context, tx *sql.Tx, before time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbound_mail WHERE created_at < ?`, formatTime(before)); err != nil {
		return fmt.Errorf("expire outbound mail: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbound_mail WHERE id IN (
		SELECT id FROM outbound_mail ORDER BY created_at DESC, id DESC LIMIT -1 OFFSET ?
	)`, outboundMailMaxEntries); err != nil {
		return fmt.Errorf("bound outbound mail history: %w", err)
	}
	return nil
}

func HashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (r *SQLiteRepository) SavePasswordReset(username, tokenHash string, expires time.Time) error {
	return r.SavePasswordResetContext(context.Background(), username, tokenHash, expires)
}

func (r *SQLiteRepository) SavePasswordResetContext(ctx context.Context, username, tokenHash string, expires time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password reset save: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE username = ? OR expires_at <= ?`, username, formatTime(now)); err != nil {
		return fmt.Errorf("replace old password resets: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO password_resets(token_hash, username, expires_at, created_at) VALUES (?, ?, ?, ?)`, tokenHash, username, formatTime(expires), formatTime(now)); err != nil {
		return fmt.Errorf("save password reset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password reset save: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) CreatePasswordResetForActiveUserContext(ctx context.Context, username, tokenHash string, expires time.Time) (User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("begin active password reset save: %w", err)
	}
	defer tx.Rollback()

	user, err := scanUser(tx.QueryRowContext(ctx, `SELECT id, username, password_hash, email, role, active, created_at, updated_at FROM users WHERE username = ? AND active = 1`, username))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("read active user for password reset: %w", err)
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE username = ? OR expires_at <= ?`, username, formatTime(now)); err != nil {
		return User{}, fmt.Errorf("replace old password resets: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO password_resets(token_hash, username, expires_at, created_at) VALUES (?, ?, ?, ?)`, tokenHash, username, formatTime(expires), formatTime(now)); err != nil {
		return User{}, fmt.Errorf("save password reset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit password reset save: %w", err)
	}
	return user, nil
}

func (r *SQLiteRepository) DeletePasswordReset(tokenHash string) error {
	return r.DeletePasswordResetContext(context.Background(), tokenHash)
}

func (r *SQLiteRepository) DeletePasswordResetContext(ctx context.Context, tokenHash string) (err error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("delete password reset: %w", err)
	}
	// SQLite's busy handler may keep sleeping after context cancellation, so
	// bound this cleanup's lock wait separately and restore the default below.
	defer conn.Close()
	defer func() {
		if _, restoreErr := conn.ExecContext(context.Background(), fmt.Sprintf(`PRAGMA busy_timeout = %d`, sqliteBusyTimeoutMilliseconds)); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore sqlite busy timeout: %w", restoreErr))
		}
	}()
	if _, err = conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA busy_timeout = %d`, passwordResetCleanupBusyTimeoutMilliseconds)); err != nil {
		return fmt.Errorf("set password reset cleanup busy timeout: %w", err)
	}
	if _, err = conn.ExecContext(ctx, `DELETE FROM password_resets WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete password reset: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) ConsumePasswordReset(tokenHash string, now time.Time) (string, error) {
	var username, expiresAt string
	err := r.db.QueryRow(`SELECT username, expires_at FROM password_resets WHERE token_hash = ?`, tokenHash).Scan(&username, &expiresAt)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read password reset: %w", err)
	}
	if parseTime(expiresAt).Before(now) {
		_, _ = r.db.Exec(`DELETE FROM password_resets WHERE token_hash = ?`, tokenHash)
		return "", ErrNotFound
	}
	if _, err := r.db.Exec(`DELETE FROM password_resets WHERE token_hash = ?`, tokenHash); err != nil {
		return "", fmt.Errorf("consume password reset: %w", err)
	}
	return username, nil
}

func (r *SQLiteRepository) CheckPasswordResetContext(ctx context.Context, tokenHash string, now time.Time) error {
	var expiresAt string
	err := r.db.QueryRowContext(ctx, `SELECT expires_at FROM password_resets WHERE token_hash = ?`, tokenHash).Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check password reset: %w", err)
	}
	if !parseTime(expiresAt).After(now) {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) CompletePasswordReset(ctx context.Context, tokenHash string, now time.Time, passwordHash string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password reset completion: %w", err)
	}
	defer tx.Rollback()

	var username, expiresAt string
	err = tx.QueryRowContext(ctx, `SELECT username, expires_at FROM password_resets WHERE token_hash = ?`, tokenHash).Scan(&username, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read password reset: %w", err)
	}
	if !parseTime(expiresAt).After(now) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE token_hash = ?`, tokenHash); err != nil {
			return fmt.Errorf("remove expired password reset: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit expired password reset removal: %w", err)
		}
		return ErrNotFound
	}

	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE username = ?`, passwordHash, formatTime(now), username)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check password update: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions
		WHERE username = ? OR user_id = (SELECT id FROM users WHERE username = ?)`, username, username); err != nil {
		return fmt.Errorf("revoke sessions after password reset: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("consume password reset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password reset: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) BackupDatabase(dest string) error {
	return r.BackupDatabaseContext(context.Background(), dest)
}

func (r *SQLiteRepository) BackupDatabaseContext(ctx context.Context, dest string) error {
	dest = filepath.ToSlash(dest)
	escaped := strings.ReplaceAll(dest, "'", "''")
	if _, err := r.db.ExecContext(ctx, `VACUUM INTO '`+escaped+`'`); err != nil {
		return fmt.Errorf("backup database: %w", err)
	}
	return nil
}
