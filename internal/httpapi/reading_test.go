package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestReadingPolicyAndCommentThreads(t *testing.T) {
	dir := t.TempDir()
	repo, err := store.NewSQLiteRepository(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	server := httptest.NewServer(NewServer(repo, config.Settings{Version: "test", SessionTTLMin: 60}, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	get(t, client, server.URL+"/api/v1/system/status")
	postJSON(t, client, server.URL+"/api/v1/setup", map[string]string{
		"siteName": "Next", "siteUrl": "https://example.com", "adminUsername": "owner",
		"adminPassword": "password1", "adminEmail": "owner@example.com", "timezone": "UTC",
	}, http.StatusCreated)

	reading := putJSON(t, client, server.URL+"/api/v1/settings/reading", map[string]any{
		"postsPerPage": 2, "commentsEnabled": true, "commentsRequireModeration": false, "commentsThreaded": true,
	}, http.StatusOK)
	if reading["postsPerPage"].(float64) != 2 || reading["commentsRequireModeration"].(bool) {
		t.Fatalf("reading settings were not saved: %#v", reading)
	}
	putJSON(t, client, server.URL+"/api/v1/settings/site", map[string]string{
		"name": "Renamed", "url": "https://example.com", "adminMail": "owner@example.com", "timezone": "UTC",
	}, http.StatusOK)
	preserved := get(t, client, server.URL+"/api/v1/settings/reading")
	if preserved["postsPerPage"].(float64) != 2 || preserved["commentsEnabled"] != true || preserved["commentsThreaded"] != true {
		t.Fatalf("site update cleared reading settings: %#v", preserved)
	}

	created := postJSON(t, client, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Hello", "content": "Body", "status": "published", "allowComment": true,
	}, http.StatusCreated)
	slug := created["slug"].(string)
	if created["allowComment"] != true {
		t.Fatal("new post should allow comments")
	}
	comment := postJSON(t, client, server.URL+"/api/v1/content/posts/"+url.PathEscape(slug)+"/comments", map[string]string{
		"authorName": "Ada", "authorUrl": "https://ada.example", "content": "First",
	}, http.StatusCreated)
	if comment["status"] != "approved" {
		t.Fatalf("expected an approved comment, got %#v", comment)
	}
	reply := postJSON(t, client, server.URL+"/api/v1/content/posts/"+url.PathEscape(slug)+"/comments", map[string]string{
		"authorName": "Bea", "content": "Reply", "parentId": comment["id"].(string),
	}, http.StatusCreated)
	if reply["parentId"] != comment["id"] || reply["status"] != "approved" {
		t.Fatalf("reply was not attached: %#v", reply)
	}
	nested := postJSON(t, client, server.URL+"/api/v1/content/posts/"+url.PathEscape(slug)+"/comments", map[string]string{
		"authorName": "Cara", "content": "Nested", "parentId": reply["id"].(string),
	}, http.StatusCreated)
	tooDeep := postJSON(t, client, server.URL+"/api/v1/content/posts/"+url.PathEscape(slug)+"/comments", map[string]string{
		"authorName": "Dora", "content": "Too deep", "parentId": nested["id"].(string),
	}, http.StatusUnprocessableEntity)
	if tooDeep["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("deep reply was not rejected by the service: %#v", tooDeep)
	}

	putJSON(t, client, server.URL+"/api/v1/settings/reading", map[string]any{
		"postsPerPage": 2, "commentsEnabled": false, "commentsRequireModeration": true, "commentsThreaded": true,
	}, http.StatusOK)
	closedSite := postJSON(t, client, server.URL+"/api/v1/content/posts/"+url.PathEscape(slug)+"/comments", map[string]string{
		"authorName": "Ada", "content": "Closed",
	}, http.StatusUnprocessableEntity)
	if closedSite["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("closed site accepted a comment: %#v", closedSite)
	}

	closed := postJSON(t, client, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Quiet", "content": "Body", "status": "published", "allowComment": false,
	}, http.StatusCreated)
	putJSON(t, client, server.URL+"/api/v1/settings/reading", map[string]any{
		"postsPerPage": 2, "commentsEnabled": true, "commentsRequireModeration": true, "commentsThreaded": false,
	}, http.StatusOK)
	closedPost := postJSON(t, client, server.URL+"/api/v1/content/posts/"+url.PathEscape(closed["slug"].(string))+"/comments", map[string]string{
		"authorName": "Ada", "content": "No",
	}, http.StatusUnprocessableEntity)
	if closedPost["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("closed post accepted a comment: %#v", closedPost)
	}
	disabledReply := postJSON(t, client, server.URL+"/api/v1/content/posts/"+url.PathEscape(slug)+"/comments", map[string]string{
		"authorName": "Ada", "content": "No reply", "parentId": comment["id"].(string),
	}, http.StatusUnprocessableEntity)
	if disabledReply["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("disabled threading accepted a reply: %#v", disabledReply)
	}

	for _, title := range []string{"Second", "Third"} {
		postJSON(t, client, server.URL+"/api/v1/content/posts", map[string]any{
			"title": title, "content": "Body", "status": "published",
		}, http.StatusCreated)
	}
	publicClient := &http.Client{}
	page := get(t, publicClient, server.URL+"/api/v1/content/posts")
	if page["pageSize"].(float64) != 2 || page["total"].(float64) != 4 {
		t.Fatalf("public list should use the reading page size: %#v", page)
	}
}

func TestPublicCommentPaginationKeepsPageBoundariesAndReplies(t *testing.T) {
	repo, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	post := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Many comments", "content": "Body", "status": "published", "allowComment": true,
	}, http.StatusCreated)
	postID := post["id"].(string)
	slug := post["slug"].(string)
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for index := 0; index < 205; index++ {
		id := fmt.Sprintf("comment-%03d", index)
		parentID := ""
		if index == 150 {
			parentID = "comment-000"
		}
		if _, err := repo.SaveComment(store.Comment{
			ID: id, PostID: postID, ParentID: parentID, AuthorName: "Reader", Content: id,
			Status: "approved", CreatedAt: baseTime.Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	public := &http.Client{}
	pageSizes := []int{100, 100, 5}
	firstIDs := []string{"comment-204", "comment-104", "comment-004"}
	seen := make(map[string]bool, 205)
	for index, wantSize := range pageSizes {
		pageNumber := index + 1
		response := get(t, public, fmt.Sprintf("%s/api/v1/content/posts/%s/comments?page=%d&pageSize=100", server.URL, url.PathEscape(slug), pageNumber))
		if int(response["total"].(float64)) != 205 || int(response["page"].(float64)) != pageNumber || int(response["pageSize"].(float64)) != 100 {
			t.Fatalf("page metadata = %#v", response)
		}
		items := response["items"].([]any)
		if len(items) != wantSize || items[0].(map[string]any)["id"] != firstIDs[index] {
			t.Fatalf("page %d item count=%d first=%#v", pageNumber, len(items), items[0])
		}
		for _, raw := range items {
			comment := raw.(map[string]any)
			id := comment["id"].(string)
			if seen[id] {
				t.Fatalf("comment %s repeated across pages", id)
			}
			seen[id] = true
			if id == "comment-150" && comment["parentId"] != "comment-000" {
				t.Fatalf("cross-page reply parent = %#v", comment["parentId"])
			}
		}
	}
	if len(seen) != 205 {
		t.Fatalf("collected %d unique comments, want 205", len(seen))
	}
}

func TestPublicCommentCursorKeepsSnapshotAcrossInsertAndApproval(t *testing.T) {
	repo, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	post := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Comment cursor", "content": "Body", "status": "published", "allowComment": true,
	}, http.StatusCreated)
	postID := post["id"].(string)
	slug := post["slug"].(string)
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for index := 0; index < 101; index++ {
		id := fmt.Sprintf("cursor-comment-%03d", index)
		if _, err := repo.SaveComment(store.Comment{
			ID: id, PostID: postID, AuthorName: "Reader", Content: id,
			Status: "approved", CreatedAt: baseTime.Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.SaveComment(store.Comment{
		ID: "cursor-pending", PostID: postID, AuthorName: "Reader", Content: "Pending before snapshot",
		Status: "pending", CreatedAt: baseTime.Add(50 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	public := &http.Client{}
	first := get(t, public, server.URL+"/api/v1/content/posts/"+url.PathEscape(slug)+"/comments?page=1&pageSize=100")
	if first["total"].(float64) != 101 || first["nextCursor"] == "" {
		t.Fatalf("first comment page = %#v", first)
	}
	cursor := first["nextCursor"].(string)
	decodedCursor, err := decodeCommentCursor(cursor)
	if err != nil {
		t.Fatalf("decode first-page cursor: %v", err)
	}

	time.Sleep(2 * time.Millisecond)
	if _, err := repo.SaveComment(store.Comment{
		ID: "cursor-new", PostID: postID, AuthorName: "Reader", Content: "Inserted after snapshot",
		Status: "approved", CreatedAt: baseTime.Add(500 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateCommentStatus("cursor-pending", "approved"); err != nil {
		t.Fatal(err)
	}
	invalidCursor := getStatus(t, public, server.URL+"/api/v1/content/posts/"+url.PathEscape(slug)+"/comments?page=2&pageSize=100&cursor=not-a-cursor", http.StatusUnprocessableEntity)
	if invalidCursor["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("invalid cursor response = %#v", invalidCursor)
	}

	secondURL := server.URL + "/api/v1/content/posts/" + url.PathEscape(slug) + "/comments?page=2&pageSize=100&cursor=" + url.QueryEscape(cursor)
	second := get(t, public, secondURL)
	items := second["items"].([]any)
	if second["total"].(float64) != 101 || second["page"].(float64) != 2 || len(items) != 1 {
		t.Fatalf("second comment page escaped its snapshot: %#v", second)
	}
	if items[0].(map[string]any)["id"] != "cursor-comment-000" || second["nextCursor"] != nil {
		t.Fatalf("second page item or continuation = %#v", second)
	}
	fresh := get(t, public, server.URL+"/api/v1/content/posts/"+url.PathEscape(slug)+"/comments?page=1&pageSize=100")
	if fresh["total"].(float64) != 103 {
		t.Fatalf("fresh comment total = %#v, want 103", fresh["total"])
	}
	if !decodedCursor.SnapshotAt.Before(time.Now().UTC()) {
		t.Fatalf("cursor snapshot timestamp is not in the past: %s", decodedCursor.SnapshotAt)
	}
}

func get(t *testing.T, client *http.Client, url string) map[string]any {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return decodeBody(t, response, http.StatusOK)
}

func postJSON(t *testing.T, client *http.Client, url string, body any, want int) map[string]any {
	t.Helper()
	return sendJSON(t, client, http.MethodPost, url, body, want)
}

func putJSON(t *testing.T, client *http.Client, url string, body any, want int) map[string]any {
	t.Helper()
	return sendJSON(t, client, http.MethodPut, url, body, want)
}

func sendJSON(t *testing.T, client *http.Client, method, url string, body any, want int) map[string]any {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if client.Jar != nil {
		for _, cookie := range client.Jar.Cookies(request.URL) {
			if cookie.Name == csrfCookie {
				request.Header.Set(csrfHeader, cookie.Value)
			}
		}
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return decodeBody(t, response, want)
}

func decodeBody(t *testing.T, response *http.Response, want int) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("status = %d, want %d, body = %#v", response.StatusCode, want, payload)
	}
	return payload
}
