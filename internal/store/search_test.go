package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedSearchCandidates(t *testing.T, repo *SQLiteRepository, table, kind string, count int) {
	t.Helper()
	tx, err := repo.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	insertSQL := `INSERT INTO posts(id, title, slug, excerpt, content, status, allow_comment, published_at, updated_at, passphrase_hash, custom_fields)
		VALUES (?, 'Bulk', ?, '', 'x', 'draft', 1, NULL, '2026-01-01T00:00:00Z', '', '{}')`
	if table == "pages" {
		insertSQL = `INSERT INTO pages(id, title, slug, excerpt, content, status, published_at, updated_at)
			VALUES (?, 'Bulk', ?, '', 'x', 'draft', NULL, '2026-01-01T00:00:00Z')`
	}
	recordInsert, err := tx.Prepare(insertSQL)
	if err != nil {
		t.Fatal(err)
	}
	tokenInsert, err := tx.Prepare(`INSERT INTO search_tokens(kind, token, record_id) VALUES (?, 'x', ?)`)
	if err != nil {
		recordInsert.Close()
		t.Fatal(err)
	}
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("%s-%05d", table, index)
		if _, err := recordInsert.Exec(id, id); err != nil {
			recordInsert.Close()
			tokenInsert.Close()
			t.Fatal(err)
		}
		if _, err := tokenInsert.Exec(kind, id); err != nil {
			recordInsert.Close()
			tokenInsert.Close()
			t.Fatal(err)
		}
	}
	if err := recordInsert.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tokenInsert.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestSearchUsesTokenIndexInsteadOfContentLike(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	bodyHit, err := repo.SavePost(Post{Title: "普通标题", Slug: "body-hit", Content: "只有正文里出现独有词紫薇", Status: "published"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SavePost(Post{Title: "百分号", Slug: "percent", Content: "进度 100%", Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SavePost(Post{Title: "不是百分号", Slug: "not-percent", Content: "进度 100X", Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SavePost(Post{Title: "下划线", Slug: "underscore", Content: "a_b", Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SavePost(Post{Title: "没有下划线", Slug: "no-underscore", Content: "axb", Status: "published"}); err != nil {
		t.Fatal(err)
	}

	found, err := repo.SearchPosts(PostQuery{Search: "紫薇", IncludeDrafts: true, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Items) != 1 || found.Items[0].ID != bodyHit.ID {
		t.Fatalf("body search = %#v", found.Items)
	}
	percents, err := repo.SearchPosts(PostQuery{Search: "100%", IncludeDrafts: true, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(percents.Items) != 1 || percents.Items[0].Slug != "percent" {
		t.Fatalf("percent search = %#v", percents.Items)
	}
	underscores, err := repo.SearchPosts(PostQuery{Search: "_", IncludeDrafts: true, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(underscores.Items) != 1 || underscores.Items[0].Slug != "underscore" {
		t.Fatalf("underscore search = %#v", underscores.Items)
	}

	rows, err := repo.db.Query("EXPLAIN QUERY PLAN "+searchTokenCandidatesSQL(3), "post", "紫", "紫薇", "薇", 3)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(plan.String())
	if text == "" || !strings.Contains(text, "search_tokens") || strings.Contains(text, "like") || strings.Contains(text, "content") {
		t.Fatalf("query plan = %q", plan.String())
	}
}

func TestSearchHandlesMoreMatchesThanSQLiteBindLimit(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	const matchCount = 32767 // One above SQLite's default maximum of 32766 bound variables.
	seedSearchCandidates(t, repo, "posts", "post", matchCount)
	seedSearchCandidates(t, repo, "pages", "page", matchCount)

	posts, err := repo.SearchPosts(PostQuery{Search: "x", IncludeDrafts: true, Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("SearchPosts() error = %v", err)
	}
	if posts.Total != matchCount || len(posts.Items) != 1 || posts.Items[0].ID != "posts-32766" {
		t.Fatalf("SearchPosts() = total %d, items %#v", posts.Total, posts.Items)
	}

	pages, err := repo.SearchPages(PageQuery{Search: "x", IncludeDrafts: true, Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("SearchPages() error = %v", err)
	}
	if pages.Total != matchCount || len(pages.Items) != 1 || pages.Items[0].ID != "pages-32766" {
		t.Fatalf("SearchPages() = total %d, items %#v", pages.Total, pages.Items)
	}
}

func TestPaginationOffsetDoesNotOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := pageOffset(maxInt, 100); got != maxInt {
		t.Fatalf("pageOffset(maxInt, 100) = %d, want %d", got, maxInt)
	}
	if got := pageOffset(2, 100); got != 100 {
		t.Fatalf("pageOffset(2, 100) = %d, want 100", got)
	}

	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	if _, err := repo.SavePost(Post{Title: "One", Slug: "one", Content: "body", Status: "draft"}); err != nil {
		t.Fatal(err)
	}
	page, err := repo.SearchPosts(PostQuery{IncludeDrafts: true, Page: maxInt, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 0 || page.Page != maxInt {
		t.Fatalf("large page = total %d, items %d, page %d", page.Total, len(page.Items), page.Page)
	}
}

func TestSearchPostsSkipsContentUnlessRequested(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	body := strings.Repeat("长正文", 5000)
	created, err := repo.SavePost(Post{Title: "列表摘要", Slug: "summary", Excerpt: "可公开的摘要", Content: body, Status: "published"})
	if err != nil {
		t.Fatal(err)
	}

	listed, err := repo.SearchPosts(PostQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != created.ID || listed.Items[0].Content != "" {
		t.Fatalf("summary query returned body data: %#v", listed.Items)
	}

	feed, err := repo.SearchPosts(PostQuery{IncludeContent: true, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 1 || feed.Items[0].Content != body {
		t.Fatalf("explicit content query returned %d runes", len(feed.Items[0].Content))
	}
}

func TestPostListBatchPopulatesTermsAndVisibleCounts(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	now := time.Now().UTC()
	posts := []Post{
		{Title: "Public", Slug: "batch-public", Content: "body", Status: "published", PublishedAt: now.Add(-time.Hour)},
		{Title: "Scheduled", Slug: "batch-scheduled", Content: "body", Status: "published", PublishedAt: now.Add(time.Hour)},
		{Title: "Draft", Slug: "batch-draft", Content: "body", Status: "draft"},
	}
	for index := range posts {
		posts[index].Categories = []Term{{Name: "Batch category", Kind: "category"}}
		posts[index].Tags = []Term{{Name: "Batch tag", Kind: "tag"}}
		if _, err := repo.SavePost(posts[index]); err != nil {
			t.Fatal(err)
		}
	}

	listed, err := repo.SearchPosts(PostQuery{IncludeDrafts: true, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != len(posts) {
		t.Fatalf("listed posts = %d, want %d", len(listed.Items), len(posts))
	}
	for _, post := range listed.Items {
		if len(post.Categories) != 1 || post.Categories[0].Name != "Batch category" || len(post.Tags) != 1 || post.Tags[0].Name != "Batch tag" {
			t.Fatalf("terms for %q = categories %#v, tags %#v", post.Slug, post.Categories, post.Tags)
		}
		if post.Categories[0].PostCount != 1 || post.Tags[0].PostCount != 1 {
			t.Fatalf("visible counts for %q = category %d, tag %d, want 1 each", post.Slug, post.Categories[0].PostCount, post.Tags[0].PostCount)
		}
	}

	all, err := repo.Posts()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(posts) {
		t.Fatalf("all posts = %d, want %d", len(all), len(posts))
	}
	for _, post := range all {
		if len(post.Categories) != 1 || len(post.Tags) != 1 || post.Categories[0].PostCount != 1 || post.Tags[0].PostCount != 1 {
			t.Fatalf("batched post terms for %q = %#v / %#v", post.Slug, post.Categories, post.Tags)
		}
	}
}

func TestRevisionSnapshotsDetectChangedEmptyCustomFieldKeys(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	post, err := repo.SavePost(Post{Title: "Fields", Slug: "empty-field-key", Content: "Body", CustomFields: map[string]string{"old": ""}})
	if err != nil {
		t.Fatal(err)
	}
	post.CustomFields = map[string]string{"new": ""}
	if _, err := repo.SavePost(post); err != nil {
		t.Fatal(err)
	}
	revisions, err := repo.ListPostRevisions(post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || len(revisions[0].CustomFields) != 1 || revisions[0].CustomFields["old"] != "" {
		t.Fatalf("changed empty-valued field keys were not snapshotted: %#v", revisions)
	}
}
