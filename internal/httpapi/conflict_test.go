package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

type conflictRepository struct {
	store.Repository
	conflictFor string
}

func (r *conflictRepository) SavePost(post store.Post) (store.Post, error) {
	if r.conflictFor == "post" {
		return store.Post{}, fmt.Errorf("save post: %w", store.ErrConflict)
	}
	return r.Repository.SavePost(post)
}

func (r *conflictRepository) SavePostContext(ctx context.Context, post store.Post) (store.Post, error) {
	if r.conflictFor == "post" {
		return store.Post{}, fmt.Errorf("save post: %w", store.ErrConflict)
	}
	return r.Repository.SavePostContext(ctx, post)
}

func (r *conflictRepository) SavePage(page store.Page) (store.Page, error) {
	if r.conflictFor == "page" {
		return store.Page{}, fmt.Errorf("save page: %w", store.ErrConflict)
	}
	return r.Repository.SavePage(page)
}

func (r *conflictRepository) SavePageContext(ctx context.Context, page store.Page) (store.Page, error) {
	if r.conflictFor == "page" {
		return store.Page{}, fmt.Errorf("save page: %w", store.ErrConflict)
	}
	return r.Repository.SavePageContext(ctx, page)
}

func (r *conflictRepository) SaveTerm(term store.Term) (store.Term, error) {
	if r.conflictFor == "term" {
		return store.Term{}, fmt.Errorf("save term: %w", store.ErrConflict)
	}
	return r.Repository.SaveTerm(term)
}

func (r *conflictRepository) SaveTermWithChangeContext(ctx context.Context, term store.Term) (store.Term, bool, error) {
	if r.conflictFor == "term" {
		return store.Term{}, false, fmt.Errorf("save term: %w", store.ErrConflict)
	}
	return r.Repository.SaveTermWithChangeContext(ctx, term)
}

func (r *conflictRepository) CreateUser(user store.User) (store.User, error) {
	if r.conflictFor == "user" {
		return store.User{}, fmt.Errorf("create user: %w", store.ErrConflict)
	}
	return r.Repository.CreateUser(user)
}

func (r *conflictRepository) CreateUserContext(ctx context.Context, user store.User) (store.User, error) {
	if r.conflictFor == "user" {
		return store.User{}, fmt.Errorf("create user: %w", store.ErrConflict)
	}
	return r.Repository.CreateUserContext(ctx, user)
}

func TestUniqueConflictsUseStableResourceErrors(t *testing.T) {
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	wrapped := &conflictRepository{Repository: repo}
	handler := NewServer(wrapped, config.Settings{Version: "test", SessionTTLMin: 60}, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := &http.Client{Jar: jar}
	setupSite(t, owner, server.URL, "UTC")

	cases := []struct {
		resource string
		path     string
		body     map[string]any
		code     string
	}{
		{"term", "/api/v1/content/terms", map[string]any{"name": "Collision", "kind": "category"}, "term_taken"},
		{"post", "/api/v1/content/posts", map[string]any{"title": "Collision", "slug": "collision", "content": "body", "status": "draft"}, "post_taken"},
		{"page", "/api/v1/content/pages", map[string]any{"title": "Collision", "slug": "collision-page", "content": "body", "status": "draft"}, "page_taken"},
		{"user", "/api/v1/settings/users", map[string]any{"username": "collision-user", "email": "collision@example.com", "password": "staff-password-123", "role": "editor"}, "username_taken"},
	}
	for _, tc := range cases {
		t.Run(tc.resource, func(t *testing.T) {
			wrapped.conflictFor = tc.resource
			response := postJSON(t, owner, server.URL+tc.path, tc.body, 409)
			wrapped.conflictFor = ""
			apiError, ok := response["error"].(map[string]any)
			if !ok || apiError["code"] != tc.code {
				t.Fatalf("%s conflict response = %#v, want code %q", tc.resource, response, tc.code)
			}
			if message, ok := apiError["message"].(string); !ok || !containsHan(message) {
				t.Fatalf("%s conflict message = %#v, want localized text", tc.resource, apiError["message"])
			}
		})
	}
}
