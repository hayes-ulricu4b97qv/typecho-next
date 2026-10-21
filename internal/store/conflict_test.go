package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSQLiteUniqueConstraintsExposeConflict(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	if _, err := repo.SaveTerm(Term{ID: "term-first", Name: "Shared", Slug: "shared", Kind: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveTerm(Term{ID: "term-second", Name: "Shared", Slug: "shared-2", Kind: "category"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate term name error = %v, want ErrConflict", err)
	}

	if _, err := repo.SavePage(Page{ID: "page-first", Title: "First", Slug: "shared-page"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SavePage(Page{ID: "page-second", Title: "Second", Slug: "shared-page"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate page slug error = %v, want ErrConflict", err)
	}

	if _, err := repo.SavePost(Post{ID: "post-first", Title: "First", Slug: "shared-post", Status: "draft"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SavePost(Post{ID: "post-second", Title: "Second", Slug: "shared-post", Status: "draft"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate post slug error = %v, want ErrConflict", err)
	}

	if _, err := repo.CreateUser(User{Username: "shared-user", PasswordHash: "hash", Email: "shared@example.com", Role: "editor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateUser(User{Username: "shared-user", PasswordHash: "hash", Email: "other@example.com", Role: "editor"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate username error = %v, want ErrConflict", err)
	}

	if _, err := repo.SavePost(Post{ID: "invalid-status", Title: "Invalid", Slug: "invalid-status", Status: "queued"}); errors.Is(err, ErrConflict) {
		t.Fatalf("check constraint was misclassified as a conflict: %v", err)
	}
}
