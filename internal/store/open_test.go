package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestLegacyJSONIsNotImported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{"initialized":true,"site":{"name":"From JSON","url":"https://json.example","adminMail":"json@example.com","timezone":"UTC"},"admin":{"username":"from-json","passwordHash":"hash","email":"json@example.com"},"posts":[{"id":"json-post","title":"JSON","slug":"json-post","content":"body","status":"published"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	repo, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if repo.Initialized() {
		t.Fatal("legacy JSON initialized the site")
	}
	if _, err := repo.User("from-json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy file user was imported: %v", err)
	}
	users, err := repo.Users()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Fatalf("users = %#v", users)
	}
	if _, err := repo.Site(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy file site was imported: %v", err)
	}
	posts, err := repo.Posts()
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 0 {
		t.Fatalf("posts = %#v", posts)
	}
}
