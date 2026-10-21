package httpapi

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestPostStatusFilterIsPaginatedAndRespectsDraftAccess(t *testing.T) {
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	server := httptest.NewServer(NewServer(repo, config.Settings{SessionTTLMin: 60}, nil).Handler())
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := &http.Client{Jar: jar}
	public := &http.Client{}
	setupSite(t, owner, server.URL, "UTC")

	postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Visible", "slug": "visible-status", "content": "Public", "status": "published",
	}, http.StatusCreated)
	postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Hidden", "slug": "hidden-status", "content": "Private", "status": "draft",
	}, http.StatusCreated)

	publicPublished := get(t, public, server.URL+"/api/v1/content/posts?status=published")
	publicItems := publicPublished["items"].([]any)
	if publicPublished["total"].(float64) != 1 || len(publicItems) != 1 || publicItems[0].(map[string]any)["slug"] != "visible-status" {
		t.Fatalf("public published filter = %#v", publicPublished)
	}
	denied := getStatus(t, public, server.URL+"/api/v1/content/posts?status=draft", http.StatusUnauthorized)
	if denied["error"].(map[string]any)["code"] != "unauthorized" {
		t.Fatalf("public draft filter error = %#v", denied)
	}

	drafts := get(t, owner, server.URL+"/api/v1/content/posts?includeDraft=true&status=draft&page=1&pageSize=1")
	draftItems := drafts["items"].([]any)
	if drafts["total"].(float64) != 1 || drafts["pageSize"].(float64) != 1 || len(draftItems) != 1 || draftItems[0].(map[string]any)["slug"] != "hidden-status" {
		t.Fatalf("owner draft filter = %#v", drafts)
	}

	invalid := getStatus(t, owner, server.URL+"/api/v1/content/posts?includeDraft=true&status=scheduled", http.StatusUnprocessableEntity)
	if invalid["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("invalid post status filter error = %#v", invalid)
	}
}
