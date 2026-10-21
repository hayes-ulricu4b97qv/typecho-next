package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (r *SQLiteRepository) Posts() ([]Post, error) {
	rows, err := r.db.Query(`SELECT id, title, slug, excerpt, content, status, allow_comment, published_at, updated_at, passphrase_hash, custom_fields FROM posts ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()
	posts := make([]Post, 0)
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, fmt.Errorf("scan post: %w", err)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate posts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close posts: %w", err)
	}
	if err := r.populatePostTermsForPosts(posts); err != nil {
		return nil, err
	}
	return posts, nil
}

func (r *SQLiteRepository) SearchPosts(query PostQuery) (PostPage, error) {
	return r.SearchPostsContext(context.Background(), query)
}

func (r *SQLiteRepository) SearchPostsContext(ctx context.Context, query PostQuery) (PostPage, error) {
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize)
	search := strings.TrimSpace(query.Search)
	where := "1 = 1"
	args := make([]any, 0, 5)
	publicationWindow := !query.PublishedFrom.IsZero() || !query.PublishedBefore.IsZero()
	if !query.IncludeDrafts || publicationWindow {
		now := formatTime(time.Now().UTC())
		where += " AND status = 'published' AND published_at IS NOT NULL AND published_at != '' AND published_at <= ?"
		args = append(args, now)
	}
	if query.Status != "" {
		where += " AND status = ?"
		args = append(args, query.Status)
	}
	if !query.PublishedFrom.IsZero() {
		where += " AND published_at >= ?"
		args = append(args, formatTime(query.PublishedFrom))
	}
	if !query.PublishedBefore.IsZero() {
		where += " AND published_at < ?"
		args = append(args, formatTime(query.PublishedBefore))
	}
	if query.TermKind != "" && query.TermSlug != "" {
		where += ` AND EXISTS (
			SELECT 1 FROM post_terms filter_pt JOIN terms filter_t ON filter_t.id = filter_pt.term_id
			WHERE filter_pt.post_id = posts.id AND filter_t.kind = ? AND filter_t.slug = ?
		)`
		args = append(args, query.TermKind, query.TermSlug)
	}
	if query.CategorySlug != "" {
		where += ` AND EXISTS (
			SELECT 1 FROM post_terms category_pt JOIN terms category_t ON category_t.id = category_pt.term_id
			WHERE category_pt.post_id = posts.id AND category_t.kind = 'category' AND category_t.slug = ?
		)`
		args = append(args, query.CategorySlug)
	}
	if query.TagSlug != "" {
		where += ` AND EXISTS (
			SELECT 1 FROM post_terms tag_pt JOIN terms tag_t ON tag_t.id = tag_pt.term_id
			WHERE tag_pt.post_id = posts.id AND tag_t.kind = 'tag' AND tag_t.slug = ?
		)`
		args = append(args, query.TagSlug)
	}
	if search != "" {
		clause, searchArgs := searchTokenFilter("posts.id", "post", search)
		where += " AND " + clause
		args = append(args, searchArgs...)
	}
	orderBy := "CASE WHEN published_at IS NULL OR published_at = '' THEN 1 ELSE 0 END, published_at DESC, id DESC"
	if query.Order == "updated" {
		orderBy = "updated_at DESC, id DESC"
	}
	contentPreview := query.IncludeContent && query.ContentPreview
	selectColumns := "id, title, slug, excerpt, status, allow_comment, published_at, updated_at, passphrase_hash"
	if query.IncludeContent {
		selectColumns = "id, title, slug, excerpt, content, status, allow_comment, published_at, updated_at, passphrase_hash, custom_fields"
		if contentPreview {
			// RSS uses at most 280 runes and needs one extra rune to detect
			// truncation. SQLite substr counts Unicode characters for TEXT.
			selectColumns = "id, title, slug, excerpt, substr(content, 1, 281), status, allow_comment, published_at, updated_at, passphrase_hash, custom_fields"
		}
	}
	if search != "" {
		return r.searchPostsContext(ctx, query, where, args, search, orderBy, selectColumns, contentPreview)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE `+where, args...).Scan(&total); err != nil {
		return PostPage{}, fmt.Errorf("count posts: %w", err)
	}
	offset := pageOffset(query.Page, query.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT `+selectColumns+` FROM posts WHERE `+where+` ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, append(args, query.PageSize, offset)...)
	if err != nil {
		return PostPage{}, fmt.Errorf("search posts: %w", err)
	}
	defer rows.Close()
	items := make([]Post, 0, query.PageSize)
	for rows.Next() {
		var post Post
		if query.IncludeContent {
			post, err = scanPost(rows)
		} else {
			post, err = scanPostSummary(rows)
		}
		if err != nil {
			return PostPage{}, fmt.Errorf("scan searched post: %w", err)
		}
		items = append(items, post)
	}
	if err := rows.Err(); err != nil {
		return PostPage{}, fmt.Errorf("iterate searched posts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return PostPage{}, fmt.Errorf("close searched posts: %w", err)
	}
	if !contentPreview {
		if err := r.populatePostTermsForPostsContext(ctx, items); err != nil {
			return PostPage{}, err
		}
	}
	return PostPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (r *SQLiteRepository) searchPostsContext(ctx context.Context, query PostQuery, where string, args []any, search, orderBy, selectColumns string, contentPreview bool) (PostPage, error) {
	ids, total, err := r.matchingSearchPageIDsContext(ctx, "posts", where, orderBy, args, search, query.Page, query.PageSize)
	if err != nil {
		return PostPage{}, err
	}
	items := make([]Post, 0, len(ids))
	if len(ids) > 0 {
		clause, idArgs := idInClause(ids)
		pageArgs := append(append([]any(nil), args...), idArgs...)
		rows, err := r.db.QueryContext(ctx, `SELECT `+selectColumns+` FROM posts WHERE `+where+` AND `+clause+` ORDER BY `+orderBy, pageArgs...)
		if err != nil {
			return PostPage{}, fmt.Errorf("read searched posts: %w", err)
		}
		for rows.Next() {
			var post Post
			if query.IncludeContent {
				post, err = scanPost(rows)
			} else {
				post, err = scanPostSummary(rows)
			}
			if err != nil {
				rows.Close()
				return PostPage{}, fmt.Errorf("scan searched post: %w", err)
			}
			items = append(items, post)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return PostPage{}, fmt.Errorf("iterate searched posts: %w", err)
		}
		if err := rows.Close(); err != nil {
			return PostPage{}, fmt.Errorf("close searched posts: %w", err)
		}
	}
	if !contentPreview {
		if err := r.populatePostTermsForPostsContext(ctx, items); err != nil {
			return PostPage{}, err
		}
	}
	return PostPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (r *SQLiteRepository) ListPublishedMeta() ([]Post, error) {
	return r.ListPublishedMetaContext(context.Background())
}

func (r *SQLiteRepository) ListPublishedMetaContext(ctx context.Context) ([]Post, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, title, slug, published_at FROM posts
		WHERE status = 'published' AND published_at IS NOT NULL AND published_at != '' AND published_at <= ?
		ORDER BY published_at DESC, id DESC`, formatTime(time.Now().UTC()))
	if err != nil {
		return nil, fmt.Errorf("list published posts: %w", err)
	}
	defer rows.Close()
	posts := make([]Post, 0)
	for rows.Next() {
		var post Post
		var publishedAt string
		if err := rows.Scan(&post.ID, &post.Title, &post.Slug, &publishedAt); err != nil {
			return nil, fmt.Errorf("scan published post: %w", err)
		}
		post.Status = "published"
		post.PublishedAt = parseTime(publishedAt)
		if post.PublishedAt.IsZero() {
			continue
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published posts: %w", err)
	}
	return posts, nil
}

func (r *SQLiteRepository) PublishedMonthCountsContext(ctx context.Context, location *time.Location) ([]MonthCount, error) {
	if location == nil {
		location = time.UTC
	}
	rows, err := r.db.QueryContext(ctx, `SELECT published_at FROM posts
		WHERE status = 'published' AND published_at IS NOT NULL AND published_at != '' AND published_at <= ?`, formatTime(time.Now().UTC()))
	if err != nil {
		return nil, fmt.Errorf("list published month counts: %w", err)
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var publishedAt string
		if err := rows.Scan(&publishedAt); err != nil {
			return nil, fmt.Errorf("scan published month timestamp: %w", err)
		}
		if parsed := parseTime(publishedAt); !parsed.IsZero() {
			counts[parsed.In(location).Format("2006-01")]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published month timestamps: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close published month timestamps: %w", err)
	}
	items := make([]MonthCount, 0, len(counts))
	for month, count := range counts {
		items = append(items, MonthCount{Month: month, Count: count})
	}
	return items, nil
}

func (r *SQLiteRepository) PublishedNeighbors(post Post) (*Post, *Post, error) {
	return r.PublishedNeighborsContext(context.Background(), post)
}

func (r *SQLiteRepository) PublishedNeighborsContext(ctx context.Context, post Post) (*Post, *Post, error) {
	if post.Status != "published" || post.ID == "" || post.PublishedAt.IsZero() {
		return nil, nil, nil
	}
	publishedAt := formatTime(post.PublishedAt)
	now := formatTime(time.Now().UTC())
	previous, err := r.publishedNeighborContext(ctx, "<", "DESC", publishedAt, post.ID, now)
	if err != nil {
		return nil, nil, err
	}
	next, err := r.publishedNeighborContext(ctx, ">", "ASC", publishedAt, post.ID, now)
	if err != nil {
		return nil, nil, err
	}
	return previous, next, nil
}

func (r *SQLiteRepository) publishedNeighborContext(ctx context.Context, comparison, direction, publishedAt, id, now string) (*Post, error) {
	query := `SELECT id, title, slug, published_at FROM posts
		WHERE status = 'published' AND published_at IS NOT NULL AND published_at != '' AND published_at <= ?
		AND (published_at, id) ` + comparison + ` (?, ?)
		ORDER BY published_at ` + direction + `, id ` + direction + ` LIMIT 1`
	var post Post
	var storedPublishedAt string
	err := r.db.QueryRowContext(ctx, query, now, publishedAt, id).Scan(&post.ID, &post.Title, &post.Slug, &storedPublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read published neighbor: %w", err)
	}
	post.Status = "published"
	post.PublishedAt = parseTime(storedPublishedAt)
	if post.PublishedAt.IsZero() {
		return nil, fmt.Errorf("read published neighbor: invalid published_at %q", storedPublishedAt)
	}
	return &post, nil
}

func idInClause(ids []string) (string, []any) {
	parts := make([]string, 0, (len(ids)+199)/200)
	args := make([]any, 0, len(ids))
	for start := 0; start < len(ids); start += 200 {
		end := start + 200
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := make([]string, len(chunk))
		for index, id := range chunk {
			placeholders[index] = "?"
			args = append(args, id)
		}
		parts = append(parts, "id IN ("+strings.Join(placeholders, ",")+")")
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func (r *SQLiteRepository) Post(id string) (Post, error) {
	return r.PostContext(context.Background(), id)
}

func (r *SQLiteRepository) PostContext(ctx context.Context, id string) (Post, error) {
	return r.postByContext(ctx, `id = ?`, id)
}

func (r *SQLiteRepository) PostBySlug(slug string) (Post, error) {
	return r.PostBySlugContext(context.Background(), slug)
}

func (r *SQLiteRepository) PostBySlugContext(ctx context.Context, slug string) (Post, error) {
	return r.postByContext(ctx, `slug = ?`, slug)
}

func (r *SQLiteRepository) postByContext(ctx context.Context, where, value string) (Post, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, title, slug, excerpt, content, status, allow_comment, published_at, updated_at, passphrase_hash, custom_fields FROM posts WHERE `+where, value)
	post, err := scanPost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, fmt.Errorf("read post: %w", err)
	}
	if err := r.populatePostTermsContext(ctx, &post); err != nil {
		return Post{}, err
	}
	return post, nil
}

func (r *SQLiteRepository) SavePost(post Post) (Post, error) {
	return r.SavePostContext(context.Background(), post)
}

func (r *SQLiteRepository) SavePostContext(ctx context.Context, post Post) (Post, error) {
	if post.ID == "" {
		post.ID = newID()
	}
	expectedUpdatedAt := post.ExpectedUpdatedAt
	if expectedUpdatedAt.IsZero() {
		expectedUpdatedAt = post.UpdatedAt
	}
	if post.Status == "" {
		post.Status = "draft"
	}
	post.UpdatedAt = time.Now().UTC()
	if post.Status == "published" && post.PublishedAt.IsZero() {
		post.PublishedAt = post.UpdatedAt
	}
	var publishedAt any
	if !post.PublishedAt.IsZero() {
		publishedAt = formatTime(post.PublishedAt)
	}
	customFields, err := encodeCustomFields(post.CustomFields)
	if err != nil {
		return Post{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Post{}, fmt.Errorf("start post save transaction: %w", err)
	}
	defer tx.Rollback()
	if err := r.snapshotPostContext(ctx, tx, post); err != nil {
		return Post{}, err
	}
	if expectedUpdatedAt.IsZero() {
		if _, err := tx.ExecContext(ctx, `INSERT INTO posts(id, title, slug, excerpt, content, status, allow_comment, published_at, updated_at, custom_fields)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET title = excluded.title, slug = excluded.slug, excerpt = excluded.excerpt, content = excluded.content, status = excluded.status, allow_comment = excluded.allow_comment, published_at = excluded.published_at, updated_at = excluded.updated_at, custom_fields = excluded.custom_fields`,
			post.ID, post.Title, post.Slug, post.Excerpt, post.Content, post.Status, boolToInt(post.AllowComment), publishedAt, formatTime(post.UpdatedAt), customFields); err != nil {
			return Post{}, wrapSQLiteWriteError("save post", err)
		}
	} else {
		result, err := tx.ExecContext(ctx, `UPDATE posts SET title = ?, slug = ?, excerpt = ?, content = ?, status = ?, allow_comment = ?, published_at = ?, updated_at = ?, custom_fields = ?
			WHERE id = ? AND updated_at = ?`,
			post.Title, post.Slug, post.Excerpt, post.Content, post.Status, boolToInt(post.AllowComment), publishedAt, formatTime(post.UpdatedAt), customFields,
			post.ID, formatTime(expectedUpdatedAt))
		if err != nil {
			return Post{}, wrapSQLiteWriteError("save post", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return Post{}, fmt.Errorf("read updated post count: %w", err)
		}
		if affected == 0 {
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM posts WHERE id = ?)`, post.ID).Scan(&exists); err != nil {
				return Post{}, fmt.Errorf("check post after failed update: %w", err)
			}
			if exists {
				return Post{}, ErrStaleWrite
			}
			return Post{}, ErrNotFound
		}
	}
	if err := r.savePostTermsContext(ctx, tx, post.ID, post.Categories, post.Tags); err != nil {
		return Post{}, err
	}
	if err := replaceSearchTokensContext(ctx, tx, "post", post.ID, post.Title, post.Excerpt, post.Slug, post.Content); err != nil {
		return Post{}, err
	}
	if post.SetPassphrase {
		if _, err := tx.ExecContext(ctx, `UPDATE posts SET passphrase_hash = ? WHERE id = ?`, post.PassphraseHash, post.ID); err != nil {
			return Post{}, fmt.Errorf("save post passphrase: %w", err)
		}
	}
	saved, err := scanPost(tx.QueryRowContext(ctx, `SELECT id, title, slug, excerpt, content, status, allow_comment, published_at, updated_at, passphrase_hash, custom_fields FROM posts WHERE id = ?`, post.ID))
	if err != nil {
		return Post{}, fmt.Errorf("read saved post: %w", err)
	}
	savedPosts := []Post{saved}
	if err := populatePostTermsWithQueryer(ctx, tx, savedPosts); err != nil {
		return Post{}, err
	}
	saved = savedPosts[0]
	if err := tx.Commit(); err != nil {
		return Post{}, fmt.Errorf("commit post save: %w", err)
	}
	return saved, nil
}

func (r *SQLiteRepository) populatePostTermsContext(ctx context.Context, post *Post) error {
	posts := []Post{*post}
	if err := r.populatePostTermsForPostsContext(ctx, posts); err != nil {
		return err
	}
	*post = posts[0]
	return nil
}

func (r *SQLiteRepository) populatePostTermsForPosts(posts []Post) error {
	return r.populatePostTermsForPostsContext(context.Background(), posts)
}

func (r *SQLiteRepository) populatePostTermsForPostsContext(ctx context.Context, posts []Post) error {
	return populatePostTermsWithQueryer(ctx, r.db, posts)
}

type contextRowsQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func populatePostTermsWithQueryer(ctx context.Context, queryer contextRowsQuerier, posts []Post) error {
	const batchSize = 200
	visibleAt := formatTime(time.Now().UTC())
	for start := 0; start < len(posts); start += batchSize {
		end := start + batchSize
		if end > len(posts) {
			end = len(posts)
		}
		batch := posts[start:end]
		placeholders := make([]string, len(batch))
		indexes := make(map[string]int, len(batch))
		args := make([]any, 0, len(batch)+1)
		for index, post := range batch {
			placeholders[index] = "?"
			args = append(args, post.ID)
			indexes[post.ID] = index
			batch[index].Categories = nil
			batch[index].Tags = nil
		}
		args = append(args, visibleAt)
		query := `WITH requested_terms AS (
			SELECT post_id, term_id FROM post_terms WHERE post_id IN (` + strings.Join(placeholders, ",") + `)
		), visible_counts AS (
			SELECT pt2.term_id, COUNT(*) AS post_count
			FROM post_terms pt2 JOIN posts p2 ON p2.id = pt2.post_id
			WHERE pt2.term_id IN (SELECT DISTINCT term_id FROM requested_terms)
				AND p2.status = 'published' AND p2.published_at IS NOT NULL AND p2.published_at != '' AND p2.published_at <= ?
			GROUP BY pt2.term_id
		)
		SELECT requested_terms.post_id, t.id, t.name, t.slug, t.kind,
			COALESCE(visible_counts.post_count, 0), t.created_at, t.updated_at
			FROM requested_terms JOIN terms t ON t.id = requested_terms.term_id
			LEFT JOIN visible_counts ON visible_counts.term_id = t.id
			ORDER BY requested_terms.post_id, CASE t.kind WHEN 'category' THEN 0 ELSE 1 END, t.name COLLATE NOCASE`
		rows, err := queryer.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("list terms for posts: %w", err)
		}
		for rows.Next() {
			var postID string
			term, err := scanTermWithPostID(rows, &postID)
			if err != nil {
				rows.Close()
				return fmt.Errorf("scan post term: %w", err)
			}
			index, ok := indexes[postID]
			if !ok {
				rows.Close()
				return fmt.Errorf("read term for unexpected post %q", postID)
			}
			if term.Kind == "category" {
				batch[index].Categories = append(batch[index].Categories, term)
			} else {
				batch[index].Tags = append(batch[index].Tags, term)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate post terms: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close post terms: %w", err)
		}
	}
	return nil
}

func (r *SQLiteRepository) savePostTermsContext(ctx context.Context, tx *sql.Tx, postID string, categories, tags []Term) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM post_terms WHERE post_id = ?`, postID); err != nil {
		return fmt.Errorf("clear post terms: %w", err)
	}
	for _, input := range append(append([]Term{}, categories...), tags...) {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			continue
		}
		kind := input.Kind
		if kind != "category" && kind != "tag" {
			return fmt.Errorf("invalid post term kind %q", kind)
		}
		termID, err := upsertTermTxContext(ctx, tx, kind, name)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO post_terms(post_id, term_id) VALUES (?, ?)`, postID, termID); err != nil {
			return fmt.Errorf("link post term: %w", err)
		}
	}
	return nil
}

func upsertTermTxContext(ctx context.Context, tx *sql.Tx, kind, name string) (string, error) {
	var existingID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM terms WHERE kind = ? AND name = ?`, kind, name).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read post term: %w", err)
	}
	id := newTermID()
	baseSlug := termSlug(name)
	now := time.Now().UTC()
	for suffix := 1; suffix < 10000; suffix++ {
		slug := baseSlug
		if suffix > 1 {
			slug = fmt.Sprintf("%s-%d", baseSlug, suffix)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO terms(id, name, slug, kind, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, id, name, slug, kind, formatTime(now), formatTime(now))
		if err == nil {
			return id, nil
		}
		if !isUniqueConstraintError(err) {
			return "", fmt.Errorf("create post term: %w", err)
		}
		var existingID string
		lookupErr := tx.QueryRowContext(ctx, `SELECT id FROM terms WHERE kind = ? AND name = ?`, kind, name).Scan(&existingID)
		if lookupErr == nil {
			return existingID, nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return "", fmt.Errorf("resolve post term conflict: %w", lookupErr)
		}
	}
	return "", fmt.Errorf("unable to allocate a unique term slug")
}

func (r *SQLiteRepository) SaveTerm(term Term) (Term, error) {
	saved, _, err := r.SaveTermWithChange(term)
	return saved, err
}

func (r *SQLiteRepository) SaveTermWithChange(term Term) (Term, bool, error) {
	return r.SaveTermWithChangeContext(context.Background(), term)
}

func (r *SQLiteRepository) SaveTermWithChangeContext(ctx context.Context, term Term) (Term, bool, error) {
	term.Name = strings.TrimSpace(term.Name)
	term.Kind = strings.TrimSpace(term.Kind)
	if term.ID == "" {
		term.ID = newTermID()
	}
	if term.Slug == "" {
		term.Slug = termSlug(term.Name)
	}
	if term.Kind != "category" && term.Kind != "tag" {
		return Term{}, false, fmt.Errorf("invalid term kind %q", term.Kind)
	}
	now := time.Now().UTC()
	if term.CreatedAt.IsZero() {
		term.CreatedAt = now
	}
	term.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Term{}, false, fmt.Errorf("begin term save: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO terms(id, name, slug, kind, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, slug = excluded.slug, kind = excluded.kind, updated_at = excluded.updated_at
		WHERE terms.name <> excluded.name OR terms.slug <> excluded.slug OR terms.kind <> excluded.kind`,
		term.ID, term.Name, term.Slug, term.Kind, formatTime(term.CreatedAt), formatTime(term.UpdatedAt))
	if err != nil {
		return Term{}, false, wrapSQLiteWriteError("save term", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Term{}, false, fmt.Errorf("read saved term count: %w", err)
	}
	saved, err := scanTermByID(tx.QueryRowContext(ctx, `SELECT t.id, t.name, t.slug, t.kind,
		COUNT(DISTINCT CASE WHEN p.status = 'published' AND p.published_at IS NOT NULL AND p.published_at != '' AND p.published_at <= ? THEN p.id END), t.created_at, t.updated_at
		FROM terms t LEFT JOIN post_terms pt ON pt.term_id = t.id LEFT JOIN posts p ON p.id = pt.post_id
		WHERE t.id = ? GROUP BY t.id, t.name, t.slug, t.kind, t.created_at, t.updated_at`, formatTime(time.Now().UTC()), term.ID))
	if err != nil {
		return Term{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Term{}, false, fmt.Errorf("commit term save: %w", err)
	}
	return saved, changed > 0, nil
}

func scanTermByID(row scanner) (Term, error) {
	term, err := scanTerm(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Term{}, ErrNotFound
	}
	if err != nil {
		return Term{}, fmt.Errorf("read term: %w", err)
	}
	return term, nil
}

func (r *SQLiteRepository) DeleteTerm(id string) error {
	return r.DeleteTermContext(context.Background(), id)
}

func (r *SQLiteRepository) DeleteTermContext(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM terms WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete term: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted term count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) ListTerms(query TermQuery) (TermPage, error) {
	return r.ListTermsContext(context.Background(), query)
}

func (r *SQLiteRepository) ListTermsContext(ctx context.Context, query TermQuery) (TermPage, error) {
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize)
	now := formatTime(time.Now().UTC())
	where := "1 = 1"
	args := make([]any, 0, 3)
	if !query.IncludeDrafts {
		where = `EXISTS (
			SELECT 1 FROM post_terms visible_pt JOIN posts visible_p ON visible_p.id = visible_pt.post_id
			WHERE visible_pt.term_id = t.id AND visible_p.status = 'published'
				AND visible_p.published_at IS NOT NULL AND visible_p.published_at != '' AND visible_p.published_at <= ?
		)`
		args = append(args, now)
	}
	if strings.TrimSpace(query.Kind) != "" {
		where += " AND t.kind = ?"
		args = append(args, strings.TrimSpace(query.Kind))
	}
	if strings.TrimSpace(query.Search) != "" {
		where += " AND t.name LIKE ? ESCAPE '\\'"
		args = append(args, containsPattern(query.Search))
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM terms t WHERE `+where, args...).Scan(&total); err != nil {
		return TermPage{}, fmt.Errorf("count terms: %w", err)
	}
	offset := pageOffset(query.Page, query.PageSize)
	pageArgs := append([]any{now}, args...)
	pageArgs = append(pageArgs, query.PageSize, offset)
	rows, err := r.db.QueryContext(ctx, `SELECT t.id, t.name, t.slug, t.kind,
		COUNT(DISTINCT CASE WHEN p.status = 'published' AND p.published_at IS NOT NULL AND p.published_at != '' AND p.published_at <= ? THEN p.id END), t.created_at, t.updated_at
		FROM terms t LEFT JOIN post_terms pt ON pt.term_id = t.id LEFT JOIN posts p ON p.id = pt.post_id
		WHERE `+where+` GROUP BY t.id, t.name, t.slug, t.kind, t.created_at, t.updated_at
		ORDER BY t.kind, t.name COLLATE NOCASE LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return TermPage{}, fmt.Errorf("list terms: %w", err)
	}
	defer rows.Close()
	items := make([]Term, 0, query.PageSize)
	for rows.Next() {
		term, err := scanTerm(rows)
		if err != nil {
			return TermPage{}, fmt.Errorf("scan term: %w", err)
		}
		items = append(items, term)
	}
	if err := rows.Err(); err != nil {
		return TermPage{}, fmt.Errorf("iterate terms: %w", err)
	}
	return TermPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (r *SQLiteRepository) DeletePost(id string) error {
	return r.DeletePostContext(context.Background(), id)
}

func (r *SQLiteRepository) DeletePostContext(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start post delete: %w", err)
	}
	defer tx.Rollback()
	if err := deleteSearchTokensContext(ctx, tx, "post", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM post_revisions WHERE post_id = ?`, id); err != nil {
		return fmt.Errorf("delete post revisions: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM posts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted post count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit post delete: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) SearchPages(query PageQuery) (PagePage, error) {
	return r.SearchPagesContext(context.Background(), query)
}

func (r *SQLiteRepository) SearchPagesContext(ctx context.Context, query PageQuery) (PagePage, error) {
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize)
	where := "1 = 1"
	args := make([]any, 0, 4)
	if !query.IncludeDrafts {
		where += " AND status = 'published' AND published_at IS NOT NULL AND published_at != '' AND published_at <= ?"
		args = append(args, formatTime(time.Now().UTC()))
	}
	search := strings.TrimSpace(query.Search)
	if search != "" {
		clause, searchArgs := searchTokenFilter("pages.id", "page", search)
		where += " AND " + clause
		args = append(args, searchArgs...)
	}
	orderBy := "updated_at DESC, id DESC"
	if search != "" {
		ids, total, err := r.matchingSearchPageIDsContext(ctx, "pages", where, orderBy, args, search, query.Page, query.PageSize)
		if err != nil {
			return PagePage{}, err
		}
		items := make([]Page, 0, len(ids))
		if len(ids) > 0 {
			clause, idArgs := idInClause(ids)
			pageArgs := append(append([]any(nil), args...), idArgs...)
			rows, err := r.db.QueryContext(ctx, `SELECT id, title, slug, excerpt, content, status, published_at, updated_at
				FROM pages WHERE `+where+` AND `+clause+` ORDER BY `+orderBy, pageArgs...)
			if err != nil {
				return PagePage{}, fmt.Errorf("read searched pages: %w", err)
			}
			for rows.Next() {
				page, err := scanPage(rows)
				if err != nil {
					rows.Close()
					return PagePage{}, fmt.Errorf("scan searched page: %w", err)
				}
				items = append(items, page)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return PagePage{}, fmt.Errorf("iterate searched pages: %w", err)
			}
			if err := rows.Close(); err != nil {
				return PagePage{}, fmt.Errorf("close searched pages: %w", err)
			}
		}
		return PagePage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pages WHERE `+where, args...).Scan(&total); err != nil {
		return PagePage{}, fmt.Errorf("count pages: %w", err)
	}
	offset := pageOffset(query.Page, query.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT id, title, slug, excerpt, content, status, published_at, updated_at
		FROM pages WHERE `+where+` ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, append(args, query.PageSize, offset)...)
	if err != nil {
		return PagePage{}, fmt.Errorf("search pages: %w", err)
	}
	defer rows.Close()
	items := make([]Page, 0, query.PageSize)
	for rows.Next() {
		page, err := scanPage(rows)
		if err != nil {
			return PagePage{}, fmt.Errorf("scan page: %w", err)
		}
		items = append(items, page)
	}
	if err := rows.Err(); err != nil {
		return PagePage{}, fmt.Errorf("iterate pages: %w", err)
	}
	return PagePage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (r *SQLiteRepository) Page(id string) (Page, error) {
	return r.PageContext(context.Background(), id)
}

func (r *SQLiteRepository) PageContext(ctx context.Context, id string) (Page, error) {
	return r.pageByContext(ctx, `id = ?`, id)
}

func (r *SQLiteRepository) PageBySlug(slug string) (Page, error) {
	return r.PageBySlugContext(context.Background(), slug)
}

func (r *SQLiteRepository) PageBySlugContext(ctx context.Context, slug string) (Page, error) {
	return r.pageByContext(ctx, `slug = ?`, slug)
}

func (r *SQLiteRepository) pageByContext(ctx context.Context, where, value string) (Page, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, title, slug, excerpt, content, status, published_at, updated_at FROM pages WHERE `+where, value)
	page, err := scanPage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("read page: %w", err)
	}
	return page, nil
}

func (r *SQLiteRepository) SavePage(page Page) (Page, error) {
	return r.SavePageContext(context.Background(), page)
}

func (r *SQLiteRepository) SavePageContext(ctx context.Context, page Page) (Page, error) {
	if page.ID == "" {
		page.ID = newPageID()
	}
	expectedUpdatedAt := page.ExpectedUpdatedAt
	if expectedUpdatedAt.IsZero() {
		expectedUpdatedAt = page.UpdatedAt
	}
	if page.Status == "" {
		page.Status = "draft"
	}
	page.UpdatedAt = time.Now().UTC()
	if page.Status == "published" {
		if page.PublishedAt.IsZero() {
			page.PublishedAt = page.UpdatedAt
		}
	} else {
		page.PublishedAt = time.Time{}
	}
	var publishedAt any
	if !page.PublishedAt.IsZero() {
		publishedAt = formatTime(page.PublishedAt)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Page{}, fmt.Errorf("start page save transaction: %w", err)
	}
	defer tx.Rollback()
	if expectedUpdatedAt.IsZero() {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pages(id, title, slug, excerpt, content, status, published_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET title = excluded.title, slug = excluded.slug, excerpt = excluded.excerpt,
			content = excluded.content, status = excluded.status, published_at = excluded.published_at, updated_at = excluded.updated_at`,
			page.ID, page.Title, page.Slug, page.Excerpt, page.Content, page.Status, publishedAt, formatTime(page.UpdatedAt)); err != nil {
			return Page{}, wrapSQLiteWriteError("save page", err)
		}
	} else {
		result, err := tx.ExecContext(ctx, `UPDATE pages SET title = ?, slug = ?, excerpt = ?, content = ?, status = ?, published_at = ?, updated_at = ?
			WHERE id = ? AND updated_at = ?`,
			page.Title, page.Slug, page.Excerpt, page.Content, page.Status, publishedAt, formatTime(page.UpdatedAt),
			page.ID, formatTime(expectedUpdatedAt))
		if err != nil {
			return Page{}, wrapSQLiteWriteError("save page", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return Page{}, fmt.Errorf("read updated page count: %w", err)
		}
		if affected == 0 {
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pages WHERE id = ?)`, page.ID).Scan(&exists); err != nil {
				return Page{}, fmt.Errorf("check page after failed update: %w", err)
			}
			if exists {
				return Page{}, ErrStaleWrite
			}
			return Page{}, ErrNotFound
		}
	}
	if err := replaceSearchTokensContext(ctx, tx, "page", page.ID, page.Title, page.Excerpt, page.Slug, page.Content); err != nil {
		return Page{}, err
	}
	saved, err := scanPage(tx.QueryRowContext(ctx, `SELECT id, title, slug, excerpt, content, status, published_at, updated_at FROM pages WHERE id = ?`, page.ID))
	if err != nil {
		return Page{}, fmt.Errorf("read saved page: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Page{}, fmt.Errorf("commit page save: %w", err)
	}
	return saved, nil
}

func (r *SQLiteRepository) DeletePage(id string) error {
	return r.DeletePageContext(context.Background(), id)
}

func (r *SQLiteRepository) DeletePageContext(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start page delete: %w", err)
	}
	defer tx.Rollback()
	if err := deleteSearchTokensContext(ctx, tx, "page", id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete page: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted page count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit page delete: %w", err)
	}
	return nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func pageOffset(page, pageSize int) int {
	if page <= 1 || pageSize <= 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	pageIndex := page - 1
	if pageIndex > maxInt/pageSize {
		return maxInt
	}
	return pageIndex * pageSize
}
