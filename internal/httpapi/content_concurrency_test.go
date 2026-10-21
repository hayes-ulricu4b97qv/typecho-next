package httpapi

import (
	"net/http"
	"testing"
)

func TestPostAndPageUpdatesRejectStaleEditorVersions(t *testing.T) {
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")

	post := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Concurrency", "slug": "post-concurrency", "content": "first post version", "status": "draft",
	}, http.StatusCreated)
	postID := post["id"].(string)
	postPath := server.URL + "/api/v1/content/posts/" + postID
	postVersion := post["updatedAt"].(string)
	missingPostVersion := putJSON(t, owner, postPath, map[string]any{
		"title": "Concurrency", "slug": "post-concurrency", "content": "unconditional post version", "status": "draft",
	}, http.StatusPreconditionRequired)
	if missingPostVersion["error"].(map[string]any)["code"] != "precondition_required" {
		t.Fatalf("missing post precondition error = %#v", missingPostVersion["error"])
	}
	zeroPostVersion := putJSON(t, owner, postPath, map[string]any{
		"title": "Concurrency", "slug": "post-concurrency", "content": "zero post version", "status": "draft",
		"expectedUpdatedAt": "0001-01-01T00:00:00Z",
	}, http.StatusPreconditionRequired)
	if zeroPostVersion["error"].(map[string]any)["code"] != "precondition_required" {
		t.Fatalf("zero post precondition error = %#v", zeroPostVersion["error"])
	}
	if current := get(t, owner, postPath); current["content"] != "first post version" {
		t.Fatalf("missing post precondition changed content: %#v", current["content"])
	}
	updatedPost := putJSON(t, owner, postPath, map[string]any{
		"title": "Concurrency", "slug": "post-concurrency", "content": "latest post version", "status": "draft",
		"expectedUpdatedAt": postVersion,
	}, http.StatusOK)
	if updatedPost["content"] != "latest post version" {
		t.Fatalf("post update content = %#v", updatedPost["content"])
	}
	stalePost := putJSON(t, owner, postPath, map[string]any{
		"title": "Concurrency", "slug": "post-concurrency", "content": "stale post version", "status": "draft",
		"expectedUpdatedAt": postVersion,
	}, http.StatusConflict)
	if stalePost["error"].(map[string]any)["code"] != "conflict" {
		t.Fatalf("stale post update error = %#v", stalePost["error"])
	}
	currentPost := get(t, owner, postPath)
	if currentPost["content"] != "latest post version" {
		t.Fatalf("stale post update overwrote current content: %#v", currentPost["content"])
	}
	revisions := get(t, owner, postPath+"/revisions")
	if revisions["total"] != float64(1) {
		t.Fatalf("stale post update added a revision: %#v", revisions)
	}

	page := postJSON(t, owner, server.URL+"/api/v1/content/pages", map[string]any{
		"title": "Concurrency page", "slug": "page-concurrency", "content": "first page version", "status": "draft",
	}, http.StatusCreated)
	pageID := page["id"].(string)
	pagePath := server.URL + "/api/v1/content/pages/" + pageID
	pageVersion := page["updatedAt"].(string)
	missingPageVersion := putJSON(t, owner, pagePath, map[string]any{
		"title": "Concurrency page", "slug": "page-concurrency", "content": "unconditional page version", "status": "draft",
	}, http.StatusPreconditionRequired)
	if missingPageVersion["error"].(map[string]any)["code"] != "precondition_required" {
		t.Fatalf("missing page precondition error = %#v", missingPageVersion["error"])
	}
	zeroPageVersion := putJSON(t, owner, pagePath, map[string]any{
		"title": "Concurrency page", "slug": "page-concurrency", "content": "zero page version", "status": "draft",
		"expectedUpdatedAt": "0001-01-01T00:00:00Z",
	}, http.StatusPreconditionRequired)
	if zeroPageVersion["error"].(map[string]any)["code"] != "precondition_required" {
		t.Fatalf("zero page precondition error = %#v", zeroPageVersion["error"])
	}
	if current := get(t, owner, pagePath); current["content"] != "first page version" {
		t.Fatalf("missing page precondition changed content: %#v", current["content"])
	}
	updatedPage := putJSON(t, owner, pagePath, map[string]any{
		"title": "Concurrency page", "slug": "page-concurrency", "content": "latest page version", "status": "draft",
		"expectedUpdatedAt": pageVersion,
	}, http.StatusOK)
	if updatedPage["content"] != "latest page version" {
		t.Fatalf("page update content = %#v", updatedPage["content"])
	}
	stalePage := putJSON(t, owner, pagePath, map[string]any{
		"title": "Concurrency page", "slug": "page-concurrency", "content": "stale page version", "status": "draft",
		"expectedUpdatedAt": pageVersion,
	}, http.StatusConflict)
	if stalePage["error"].(map[string]any)["code"] != "conflict" {
		t.Fatalf("stale page update error = %#v", stalePage["error"])
	}
	currentPage := get(t, owner, pagePath)
	if currentPage["content"] != "latest page version" {
		t.Fatalf("stale page update overwrote current content: %#v", currentPage["content"])
	}
}
