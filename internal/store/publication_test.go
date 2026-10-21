package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatTimePreservesLexicalSubsecondOrder(t *testing.T) {
	base := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.FixedZone("offset", 8*60*60))
	earlier := base.Add(100 * time.Millisecond)
	later := base.Add(123 * time.Millisecond)
	formattedEarlier := formatTime(earlier)
	formattedLater := formatTime(later)
	if len(formattedEarlier) != len(formattedLater) || formattedEarlier >= formattedLater {
		t.Fatalf("formatted timestamps do not sort chronologically: %q >= %q", formattedEarlier, formattedLater)
	}
	if got := parseTime(formattedEarlier); !got.Equal(earlier) {
		t.Fatalf("parseTime(%q) = %s, want %s", formattedEarlier, got, earlier)
	}
}

func TestPublishedQueriesKeepSubsecondOrderAndNeighbors(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	stamp := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	earlier, err := repo.SavePost(Post{ID: "post-earlier", Title: "Earlier", Slug: "earlier", Status: "published", PublishedAt: stamp.Add(100 * time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	later, err := repo.SavePost(Post{ID: "post-later", Title: "Later", Slug: "later", Status: "published", PublishedAt: stamp.Add(123 * time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SavePost(Post{ID: "post-future", Title: "Future", Slug: "future", Status: "published", PublishedAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	page, err := repo.SearchPosts(PostQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != later.ID || page.Items[1].ID != earlier.ID {
		t.Fatalf("public post order = %#v, want later before earlier", page.Items)
	}
	meta, err := repo.ListPublishedMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 2 || meta[0].ID != later.ID || meta[1].ID != earlier.ID {
		t.Fatalf("published metadata = %#v, want only visible posts in timestamp order", meta)
	}

	sameTime := stamp.Add(time.Second)
	first, err := repo.SavePost(Post{ID: "neighbor-a", Title: "A", Slug: "neighbor-a", Status: "published", PublishedAt: sameTime})
	if err != nil {
		t.Fatal(err)
	}
	middle, err := repo.SavePost(Post{ID: "neighbor-b", Title: "B", Slug: "neighbor-b", Status: "published", PublishedAt: sameTime})
	if err != nil {
		t.Fatal(err)
	}
	last, err := repo.SavePost(Post{ID: "neighbor-c", Title: "C", Slug: "neighbor-c", Status: "published", PublishedAt: sameTime})
	if err != nil {
		t.Fatal(err)
	}
	previous, next, err := repo.PublishedNeighbors(middle)
	if err != nil {
		t.Fatal(err)
	}
	if previous == nil || previous.ID != first.ID || next == nil || next.ID != last.ID {
		t.Fatalf("neighbors = (%#v, %#v), want %q and %q", previous, next, first.ID, last.ID)
	}
}

func TestSearchPostsPublicationWindowBoundariesAndIndex(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	now := time.Now().UTC()
	from := now.Add(-4 * time.Hour).Truncate(time.Second)
	before := now.Add(-2 * time.Hour).Truncate(time.Second)
	posts := []Post{
		{ID: "window-before", Title: "Before", Slug: "window-before", Status: "published", PublishedAt: from.Add(-time.Nanosecond)},
		{ID: "window-start", Title: "Start", Slug: "window-start", Status: "published", PublishedAt: from},
		{ID: "window-fraction", Title: "Fraction", Slug: "window-fraction", Status: "published", PublishedAt: from.Add(123 * time.Nanosecond)},
		{ID: "window-end-minus", Title: "End minus", Slug: "window-end-minus", Status: "published", PublishedAt: before.Add(-time.Nanosecond)},
		{ID: "window-end", Title: "End", Slug: "window-end", Status: "published", PublishedAt: before},
		{ID: "window-draft", Title: "Draft", Slug: "window-draft", Status: "draft", PublishedAt: now.Add(-3 * time.Hour)},
		{ID: "window-future", Title: "Future", Slug: "window-future", Status: "published", PublishedAt: now.Add(24 * time.Hour)},
	}
	for _, post := range posts {
		if _, err := repo.SavePost(post); err != nil {
			t.Fatal(err)
		}
	}

	page, err := repo.SearchPosts(PostQuery{
		IncludeDrafts: true, PublishedFrom: from, PublishedBefore: before, Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"window-start": true, "window-fraction": true, "window-end-minus": true,
	}
	got := make(map[string]bool, len(page.Items))
	for _, post := range page.Items {
		got[post.ID] = true
	}
	if len(got) != len(want) || page.Total != len(want) {
		t.Fatalf("publication window ids = %#v, total = %d, want %#v", got, page.Total, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("publication window omitted %q: %#v", id, got)
		}
	}

	futurePage, err := repo.SearchPosts(PostQuery{
		IncludeDrafts: true, PublishedFrom: now.Add(time.Hour), PublishedBefore: now.Add(48 * time.Hour), Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(futurePage.Items) != 0 || futurePage.Total != 0 {
		t.Fatalf("future publication window = %#v, want no publicly visible posts", futurePage)
	}

	rows, err := repo.db.Query(`EXPLAIN QUERY PLAN SELECT id FROM posts
		WHERE status = 'published' AND published_at IS NOT NULL AND published_at != '' AND published_at <= ?
		AND published_at >= ? AND published_at < ?
		ORDER BY published_at DESC, id DESC LIMIT 20`, formatTime(now), formatTime(from), formatTime(before))
	if err != nil {
		t.Fatal(err)
	}
	var usesPublicationIndex bool
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if strings.Contains(detail, "posts_status_published_idx") {
			usesPublicationIndex = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !usesPublicationIndex {
		t.Fatal("publication window query did not use posts_status_published_idx")
	}
}

func TestPublicTermsAndRecentCommentsExcludeScheduledPosts(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	term, err := repo.SaveTerm(Term{Name: "Scheduled category", Kind: "category"})
	if err != nil {
		t.Fatal(err)
	}
	future, err := repo.SavePost(Post{
		ID: "future-post", Title: "Future", Slug: "future-post", Status: "published",
		PublishedAt: time.Now().UTC().Add(time.Hour), Categories: []Term{{ID: term.ID, Name: term.Name, Kind: term.Kind}},
	})
	if err != nil {
		t.Fatal(err)
	}
	term, err = repo.SaveTerm(term)
	if err != nil {
		t.Fatal(err)
	}
	if term.PostCount != 0 {
		t.Fatalf("saved scheduled-only term count = %d, want 0", term.PostCount)
	}
	if len(future.Categories) != 1 || future.Categories[0].PostCount != 0 {
		t.Fatalf("scheduled post category count = %#v, want 0", future.Categories)
	}

	publicTerms, err := repo.ListTerms(TermQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(publicTerms.Items) != 0 {
		t.Fatalf("public terms include scheduled-only term: %#v", publicTerms.Items)
	}
	allTerms, err := repo.ListTerms(TermQuery{IncludeDrafts: true, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(allTerms.Items) != 1 || allTerms.Items[0].PostCount != 0 {
		t.Fatalf("all terms = %#v, want scheduled term with public count 0", allTerms.Items)
	}

	if _, err := repo.SaveComment(Comment{ID: "future-comment", PostID: future.ID, AuthorName: "Reader", Content: "Scheduled discussion", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	comments, err := repo.ListComments(CommentQuery{Status: "approved", PublishedPostsOnly: true, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments.Items) != 0 {
		t.Fatalf("recent public comments include scheduled post: %#v", comments.Items)
	}
}

func TestPublicTermsAndRecentCommentsExcludeEmptyPublicationDates(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	term, err := repo.SaveTerm(Term{Name: "Malformed category", Kind: "category"})
	if err != nil {
		t.Fatal(err)
	}
	post, err := repo.SavePost(Post{
		ID: "empty-publication-post", Title: "Hidden post", Slug: "empty-publication-post", Status: "published",
		Categories: []Term{{ID: term.ID, Name: term.Name, Kind: term.Kind}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE posts SET published_at = '' WHERE id = ?`, post.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveComment(Comment{
		ID: "empty-publication-comment", PostID: post.ID, AuthorName: "Reader", Content: "Hidden discussion", Status: "approved",
	}); err != nil {
		t.Fatal(err)
	}

	publicTerms, err := repo.ListTerms(TermQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(publicTerms.Items) != 0 {
		t.Fatalf("public terms include post with empty publication date: %#v", publicTerms.Items)
	}
	allTerms, err := repo.ListTerms(TermQuery{IncludeDrafts: true, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(allTerms.Items) != 1 || allTerms.Items[0].PostCount != 0 {
		t.Fatalf("all terms = %#v, want malformed term with public count 0", allTerms.Items)
	}
	comments, err := repo.ListComments(CommentQuery{
		Status: "approved", PublishedPostsOnly: true, Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments.Items) != 0 {
		t.Fatalf("recent public comments include post with empty publication date: %#v", comments.Items)
	}
}

func TestPublishedNeighborQueriesUsePublicationIndex(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	for _, order := range []struct {
		comparison string
		direction  string
	}{{comparison: "<", direction: "DESC"}, {comparison: ">", direction: "ASC"}} {
		query := `EXPLAIN QUERY PLAN SELECT id FROM posts
			WHERE status = 'published' AND published_at IS NOT NULL AND published_at != '' AND published_at <= ?
			AND (published_at, id) ` + order.comparison + ` (?, ?)
			ORDER BY published_at ` + order.direction + `, id ` + order.direction + ` LIMIT 1`
		rows, err := repo.db.Query(query, formatTime(time.Now().UTC()), formatTime(time.Now().UTC()), "pivot")
		if err != nil {
			t.Fatal(err)
		}
		var usesPublicationIndex bool
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if strings.Contains(detail, "posts_status_published_idx") {
				usesPublicationIndex = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if !usesPublicationIndex {
			t.Errorf("neighbor order %s did not use publication index", order.direction)
		}
	}
}
