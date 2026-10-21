package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (r *SQLiteRepository) ListComments(query CommentQuery) (CommentPage, error) {
	return r.ListCommentsContext(context.Background(), query)
}

func (r *SQLiteRepository) ListCommentsContext(ctx context.Context, query CommentQuery) (CommentPage, error) {
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize)
	where := "1 = 1"
	args := make([]any, 0, 3)
	if query.PostID != "" {
		where += " AND c.post_id = ?"
		args = append(args, query.PostID)
	}
	if query.Status != "" {
		where += " AND c.status = ?"
		args = append(args, query.Status)
	} else if !query.IncludePending {
		where += " AND c.status = 'approved'"
	}
	if query.PublishedPostsOnly {
		where += " AND p.status = 'published' AND p.published_at IS NOT NULL AND p.published_at != '' AND p.published_at <= ?"
		args = append(args, formatTime(time.Now().UTC()))
	}
	from := "comments c JOIN posts p ON p.id = c.post_id"
	snapshotAt := time.Now().UTC()
	if query.Cursor != nil {
		snapshotAt = query.Cursor.SnapshotAt
	}
	snapshotTimestamp := formatTime(snapshotAt)
	countWhere := where
	countArgs := append([]any(nil), args...)
	countWhere += " AND c.created_at <= ? AND c.updated_at <= ?"
	countArgs = append(countArgs, snapshotTimestamp, snapshotTimestamp)
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+from+` WHERE `+countWhere, countArgs...).Scan(&total); err != nil {
		return CommentPage{}, fmt.Errorf("count comments: %w", err)
	}
	listWhere := countWhere
	listArgs := append([]any(nil), countArgs...)
	offset := pageOffset(query.Page, query.PageSize)
	if query.Cursor != nil {
		beforeAt := formatTime(query.Cursor.BeforeAt)
		listWhere += " AND (c.created_at < ? OR (c.created_at = ? AND c.id < ?))"
		listArgs = append(listArgs, beforeAt, beforeAt, query.Cursor.BeforeID)
		offset = 0
	}
	rows, err := r.db.QueryContext(ctx, `SELECT c.id, c.post_id, p.title, p.slug, c.author_name, c.author_email, c.author_url, c.content, c.status, c.parent_id, c.created_at, c.updated_at
		FROM `+from+` WHERE `+listWhere+` ORDER BY c.created_at DESC, c.id DESC LIMIT ? OFFSET ?`, append(listArgs, query.PageSize+1, offset)...)
	if err != nil {
		return CommentPage{}, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()
	items := make([]Comment, 0, query.PageSize+1)
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return CommentPage{}, fmt.Errorf("scan comment: %w", err)
		}
		items = append(items, comment)
	}
	if err := rows.Err(); err != nil {
		return CommentPage{}, fmt.Errorf("iterate comments: %w", err)
	}
	hasMore := len(items) > query.PageSize
	if hasMore {
		items = items[:query.PageSize]
	}
	return CommentPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize, HasMore: hasMore, SnapshotAt: snapshotAt}, nil
}

func (r *SQLiteRepository) Comment(id string) (Comment, error) {
	return r.CommentContext(context.Background(), id)
}

func (r *SQLiteRepository) CommentContext(ctx context.Context, id string) (Comment, error) {
	row := r.db.QueryRowContext(ctx, `SELECT c.id, c.post_id, p.title, p.slug, c.author_name, c.author_email, c.author_url, c.content, c.status, c.parent_id, c.created_at, c.updated_at
		FROM comments c JOIN posts p ON p.id = c.post_id WHERE c.id = ?`, id)
	comment, err := scanComment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("read comment: %w", err)
	}
	return comment, nil
}

func (r *SQLiteRepository) SaveComment(comment Comment) (Comment, error) {
	return r.SaveCommentContext(context.Background(), comment)
}

func (r *SQLiteRepository) SaveCommentContext(ctx context.Context, comment Comment) (Comment, error) {
	if comment.ID == "" {
		comment.ID = newCommentID()
	}
	if comment.Status == "" {
		comment.Status = "pending"
	}
	if comment.CreatedAt.IsZero() {
		comment.CreatedAt = time.Now().UTC()
	}
	comment.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Comment{}, fmt.Errorf("begin comment save: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO comments(id, post_id, parent_id, author_name, author_email, author_url, content, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET post_id = excluded.post_id, parent_id = excluded.parent_id, author_name = excluded.author_name, author_email = excluded.author_email,
		author_url = excluded.author_url, content = excluded.content, status = excluded.status, updated_at = excluded.updated_at`,
		comment.ID, comment.PostID, comment.ParentID, comment.AuthorName, comment.AuthorEmail, comment.AuthorURL, comment.Content, comment.Status, formatTime(comment.CreatedAt), formatTime(comment.UpdatedAt))
	if err != nil {
		return Comment{}, fmt.Errorf("save comment: %w", err)
	}
	row := tx.QueryRowContext(ctx, `SELECT c.id, c.post_id, p.title, p.slug, c.author_name, c.author_email, c.author_url, c.content, c.status, c.parent_id, c.created_at, c.updated_at
		FROM comments c JOIN posts p ON p.id = c.post_id WHERE c.id = ?`, comment.ID)
	saved, err := scanComment(row)
	if err != nil {
		return Comment{}, fmt.Errorf("read saved comment: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Comment{}, fmt.Errorf("commit comment save: %w", err)
	}
	return saved, nil
}

func (r *SQLiteRepository) UpdateCommentStatus(id, status string) (Comment, bool, error) {
	return r.UpdateCommentStatusContext(context.Background(), id, status)
}

func (r *SQLiteRepository) UpdateCommentStatusContext(ctx context.Context, id, status string) (Comment, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Comment{}, false, fmt.Errorf("begin comment status update: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE comments SET status = ?, updated_at = ? WHERE id = ? AND status <> ?`, status, formatTime(time.Now().UTC()), id, status)
	if err != nil {
		return Comment{}, false, fmt.Errorf("update comment status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Comment{}, false, fmt.Errorf("read updated comment count: %w", err)
	}
	comment, err := scanComment(tx.QueryRowContext(ctx, `SELECT c.id, c.post_id, p.title, p.slug, c.author_name, c.author_email, c.author_url, c.content, c.status, c.parent_id, c.created_at, c.updated_at
		FROM comments c JOIN posts p ON p.id = c.post_id WHERE c.id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Comment{}, false, ErrNotFound
		}
		return Comment{}, false, fmt.Errorf("read updated comment: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Comment{}, false, fmt.Errorf("commit comment status update: %w", err)
	}
	return comment, affected > 0, nil
}

func (r *SQLiteRepository) DeleteComment(id string) error {
	return r.DeleteCommentContext(context.Background(), id)
}

func (r *SQLiteRepository) DeleteCommentContext(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted comment count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) ListMedia(query MediaQuery) (MediaPage, error) {
	return r.ListMediaContext(context.Background(), query)
}

func (r *SQLiteRepository) ListMediaContext(ctx context.Context, query MediaQuery) (MediaPage, error) {
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize)
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media`).Scan(&total); err != nil {
		return MediaPage{}, fmt.Errorf("count media: %w", err)
	}
	offset := pageOffset(query.Page, query.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT id, filename, content_type, size, storage_key, created_at FROM media ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, query.PageSize, offset)
	if err != nil {
		return MediaPage{}, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()
	items := make([]Media, 0, query.PageSize)
	for rows.Next() {
		media, err := scanMedia(rows)
		if err != nil {
			return MediaPage{}, fmt.Errorf("scan media: %w", err)
		}
		items = append(items, media)
	}
	if err := rows.Err(); err != nil {
		return MediaPage{}, fmt.Errorf("iterate media: %w", err)
	}
	return MediaPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (r *SQLiteRepository) Media(id string) (Media, error) {
	return r.MediaContext(context.Background(), id)
}

func (r *SQLiteRepository) MediaContext(ctx context.Context, id string) (Media, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, filename, content_type, size, storage_key, created_at FROM media WHERE id = ?`, id)
	media, err := scanMedia(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Media{}, ErrNotFound
	}
	if err != nil {
		return Media{}, fmt.Errorf("read media: %w", err)
	}
	return media, nil
}

func (r *SQLiteRepository) SaveMedia(media Media) (Media, error) {
	return r.SaveMediaContext(context.Background(), media)
}

func (r *SQLiteRepository) SaveMediaContext(ctx context.Context, media Media) (Media, error) {
	if media.ID == "" {
		media.ID = newMediaID()
	}
	if media.CreatedAt.IsZero() {
		media.CreatedAt = time.Now().UTC()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Media{}, fmt.Errorf("begin media save: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO media(id, filename, content_type, size, storage_key, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET filename = excluded.filename, content_type = excluded.content_type, size = excluded.size, storage_key = excluded.storage_key`,
		media.ID, media.Filename, media.ContentType, media.Size, media.StorageKey, formatTime(media.CreatedAt))
	if err != nil {
		return Media{}, fmt.Errorf("save media: %w", err)
	}
	row := tx.QueryRowContext(ctx, `SELECT id, filename, content_type, size, storage_key, created_at FROM media WHERE id = ?`, media.ID)
	saved, err := scanMedia(row)
	if err != nil {
		return Media{}, fmt.Errorf("read saved media: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Media{}, fmt.Errorf("commit media save: %w", err)
	}
	return saved, nil
}

func (r *SQLiteRepository) DeleteMedia(id string) error {
	result, err := r.db.Exec(`DELETE FROM media WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete media: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted media count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
