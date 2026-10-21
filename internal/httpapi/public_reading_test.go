package httpapi

import (
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
	_ "time/tzdata"
)

func TestMaxContentRoundTrip(t *testing.T) {
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")

	body := strings.Repeat("测", 500000)
	for _, kind := range []string{"posts", "pages"} {
		created := postJSON(t, owner, server.URL+"/api/v1/content/"+kind, map[string]any{
			"title": "上限", "slug": "max-" + kind, "excerpt": "摘要", "content": body, "status": "published",
		}, http.StatusCreated)
		assertSameContent(t, created["content"], body)
		slug, _ := created["slug"].(string)
		if slug == "" {
			t.Fatalf("created %s did not return a slug", kind)
		}
		loaded := get(t, &http.Client{}, server.URL+"/api/v1/content/"+kind+"/"+slug)
		assertSameContent(t, loaded["content"], body)
	}

	tooLong := body + "测"
	for _, kind := range []string{"posts", "pages"} {
		rejected := postJSON(t, owner, server.URL+"/api/v1/content/"+kind, map[string]any{
			"title": "超限", "slug": "over-" + kind, "content": tooLong, "status": "published",
		}, http.StatusUnprocessableEntity)
		errBody, _ := rejected["error"].(map[string]any)
		if errBody["code"] != "validation_error" {
			t.Fatalf("%s over the limit was not a validation error: %#v", kind, rejected)
		}
		message, _ := errBody["message"].(string)
		lower := strings.ToLower(message)
		if strings.Contains(lower, "invalid json") || strings.Contains(lower, "too large") || strings.Contains(lower, "request body") {
			t.Fatalf("%s over the limit was rejected by the transport: %s", kind, message)
		}
	}
}

func TestPublicReadingSurfaces(t *testing.T) {
	repo, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "America/Los_Angeles")
	putJSON(t, owner, server.URL+"/api/v1/settings/site", map[string]string{
		"name": "海岸", "url": "https://example.com", "adminMail": "owner@example.com",
		"timezone": "America/Los_Angeles", "description": "园子里的笔记", "keywords": "笔记, Go",
	}, http.StatusOK)

	publicJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	public := &http.Client{Jar: publicJar}
	status := get(t, public, server.URL+"/api/v1/system/status")
	site := status["site"].(map[string]any)
	if site["description"] != "园子里的笔记" || site["keywords"] != "笔记, Go" {
		t.Fatalf("anonymous site payload = %#v", site)
	}
	if _, exists := site["adminMail"]; exists {
		t.Fatalf("anonymous site status leaked the administrative email: %#v", site)
	}
	ownerSite := get(t, owner, server.URL+"/api/v1/settings/site")
	if ownerSite["adminMail"] != "owner@example.com" {
		t.Fatalf("owner site settings lost the administrative email: %#v", ownerSite)
	}
	feed, mediaType := readFeed(t, public, server.URL+"/feed.xml")
	if !strings.Contains(mediaType, "rss") || feed.Version != "2.0" || feed.Channel.Description != "园子里的笔记" {
		t.Fatalf("feed description = %q, type = %q, version = %q", feed.Channel.Description, mediaType, feed.Version)
	}

	putJSON(t, owner, server.URL+"/api/v1/settings/site", map[string]string{
		"name": "海岸", "url": "https://example.com", "adminMail": "owner@example.com",
		"timezone": "America/Los_Angeles", "description": "", "keywords": "笔记, Go",
	}, http.StatusOK)
	emptyFeed, _ := readFeed(t, public, server.URL+"/feed.xml")
	if emptyFeed.Channel.Description != "海岸 latest posts" {
		t.Fatalf("empty description feed = %q", emptyFeed.Channel.Description)
	}
	cleared := get(t, public, server.URL+"/api/v1/system/status")["site"].(map[string]any)
	if cleared["description"] != "" || cleared["keywords"] != "笔记, Go" {
		t.Fatalf("cleared site payload = %#v", cleared)
	}

	postJSON(t, owner, server.URL+"/api/v1/settings/users", map[string]string{
		"username": "editor1", "email": "editor@example.com", "password": "password1", "role": "editor",
	}, http.StatusCreated)
	postJSON(t, owner, server.URL+"/api/v1/settings/users", map[string]string{
		"username": "moderator1", "email": "moderator@example.com", "password": "password1", "role": "moderator",
	}, http.StatusCreated)
	for _, username := range []string{"editor1", "moderator1"} {
		staff := loginAs(t, server.URL, username, "password1")
		denied := putJSON(t, staff, server.URL+"/api/v1/settings/site", map[string]string{
			"name": "海岸", "url": "https://example.com", "adminMail": "owner@example.com",
			"timezone": "America/Los_Angeles", "description": "被篡改", "keywords": "nope",
		}, http.StatusForbidden)
		if denied["error"].(map[string]any)["code"] != "forbidden" {
			t.Fatalf("%s site write was not forbidden: %#v", username, denied)
		}
	}
	unchanged := get(t, public, server.URL+"/api/v1/system/status")["site"].(map[string]any)
	if unchanged["description"] != "" || unchanged["keywords"] != "笔记, Go" {
		t.Fatalf("non-owner write changed the site: %#v", unchanged)
	}

	same := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	published := []timedPost{
		saveTimedPost(t, repo, "post-a", "tie-a", "甲", same, "published"),
		saveTimedPost(t, repo, "post-b", "tie-b", "乙", same, "published"),
		saveTimedPost(t, repo, "post-c", "tie-c", "丙", same.Add(time.Hour), "published"),
		saveTimedPost(t, repo, "post-dec", "boundary-dec", "跨月", time.Date(2026, 1, 1, 7, 30, 0, 0, time.UTC), "published"),
		saveTimedPost(t, repo, "post-jan", "boundary-jan", "元旦", time.Date(2026, 1, 1, 8, 30, 0, 0, time.UTC), "published"),
	}
	draft := saveTimedPost(t, repo, "post-draft", "hidden-draft", "草稿", same.Add(time.Minute), "draft")
	apiDraft := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "接口草稿", "slug": "api-draft", "content": "隐藏", "status": "draft",
	}, http.StatusCreated)
	apiPost := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "现在", "slug": "api-now", "content": "公开", "status": "published",
	}, http.StatusCreated)
	apiWhen, err := time.Parse(time.RFC3339Nano, apiPost["publishedAt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	published = append(published, timedPost{slug: apiPost["slug"].(string), at: apiWhen})

	middle := get(t, public, server.URL+"/api/v1/content/posts/tie-b")
	if neighborSlug(middle, "previous") != "tie-a" || neighborSlug(middle, "next") != "tie-c" {
		t.Fatalf("tie-b neighbors = previous %q next %q", neighborSlug(middle, "previous"), neighborSlug(middle, "next"))
	}
	first := get(t, public, server.URL+"/api/v1/content/posts/tie-a")
	if neighborSlug(first, "previous") != "" || neighborSlug(first, "next") != "tie-b" {
		t.Fatalf("tie-a neighbors = previous %q next %q", neighborSlug(first, "previous"), neighborSlug(first, "next"))
	}
	for _, post := range []map[string]any{first, middle} {
		if neighborSlug(post, "previous") == draft.slug || neighborSlug(post, "next") == draft.slug || neighborSlug(post, "previous") == "api-draft" || neighborSlug(post, "next") == "api-draft" {
			t.Fatalf("draft leaked into neighbors: %#v", post)
		}
	}
	draftRead := getStatus(t, public, server.URL+"/api/v1/content/posts/hidden-draft", http.StatusNotFound)
	if draftRead["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("anonymous draft read = %#v", draftRead)
	}
	if apiDraft["status"] != "draft" {
		t.Fatalf("api draft = %#v", apiDraft)
	}

	published = append(published,
		saveTimedPost(t, repo, "post-dst-before", "dst-before", "DST before", time.Date(2024, 3, 1, 7, 59, 59, 999999999, time.UTC), "published"),
		saveTimedPost(t, repo, "post-dst-start", "dst-start", "DST start", time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC), "published"),
		saveTimedPost(t, repo, "post-dst-end-minus", "dst-end-minus", "DST end minus", time.Date(2024, 4, 1, 6, 59, 59, 999999999, time.UTC), "published"),
		saveTimedPost(t, repo, "post-dst-end", "dst-end", "DST end", time.Date(2024, 4, 1, 7, 0, 0, 0, time.UTC), "published"),
	)
	assertArchive(t, public, server.URL, "America/Los_Angeles", published)
	invalidZone := putJSON(t, owner, server.URL+"/api/v1/settings/site", map[string]string{
		"name": "海岸", "url": "https://example.com", "adminMail": "owner@example.com",
		"timezone": "Not/ARealZone", "description": "", "keywords": "笔记, Go",
	}, http.StatusUnprocessableEntity)
	if invalidZone["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("invalid timezone = %#v", invalidZone)
	}
	assertArchive(t, public, server.URL, "America/Los_Angeles", published)
	for _, siteURL := range []string{
		"https://example.com/blog",
		"https://example.com/?campaign=test",
		"https://example.com/#section",
		"https://owner:secret@example.com",
	} {
		invalidAddress := putJSON(t, owner, server.URL+"/api/v1/settings/site", map[string]string{
			"name": "海岸", "url": siteURL, "adminMail": "owner@example.com",
			"timezone": "America/Los_Angeles", "description": "", "keywords": "笔记, Go",
		}, http.StatusUnprocessableEntity)
		if invalidAddress["error"].(map[string]any)["code"] != "validation_error" {
			t.Fatalf("invalid site URL %q = %#v", siteURL, invalidAddress)
		}
	}
	unchangedSite := get(t, owner, server.URL+"/api/v1/settings/site")
	if unchangedSite["url"] != "https://example.com" {
		t.Fatalf("failed site URL updates changed the stored address: %#v", unchangedSite)
	}
	putJSON(t, owner, server.URL+"/api/v1/settings/site", map[string]string{
		"name": "海岸", "url": "https://example.com", "adminMail": "owner@example.com",
		"timezone": "UTC", "description": "", "keywords": "笔记, Go",
	}, http.StatusOK)
	assertArchive(t, public, server.URL, "UTC", published)

	putJSON(t, owner, server.URL+"/api/v1/settings/reading", map[string]any{
		"postsPerPage": 10, "commentsEnabled": true, "commentsRequireModeration": false, "commentsThreaded": true,
	}, http.StatusOK)
	approved := postJSON(t, public, server.URL+"/api/v1/content/posts/tie-a/comments", map[string]string{
		"authorName": "读者", "content": "公开评论",
	}, http.StatusCreated)
	if approved["status"] != "approved" || approved["postSlug"] != "tie-a" {
		t.Fatalf("approved comment = %#v", approved)
	}
	putJSON(t, owner, server.URL+"/api/v1/settings/reading", map[string]any{
		"postsPerPage": 10, "commentsEnabled": true, "commentsRequireModeration": true, "commentsThreaded": true,
	}, http.StatusOK)
	pending := postJSON(t, public, server.URL+"/api/v1/content/posts/tie-a/comments", map[string]string{
		"authorName": "读者", "content": "待审核",
	}, http.StatusAccepted)
	spam := postJSON(t, public, server.URL+"/api/v1/content/posts/tie-a/comments", map[string]string{
		"authorName": "读者", "content": "垃圾评论",
	}, http.StatusAccepted)
	putJSON(t, owner, server.URL+"/api/v1/content/comments/"+spam["id"].(string), map[string]string{"status": "spam"}, http.StatusOK)
	if _, err := repo.SaveComment(store.Comment{PostID: draft.id, AuthorName: "隐藏", Content: "草稿评论", Status: "approved"}); err != nil {
		t.Fatal(err)
	}

	recent := get(t, public, server.URL+"/api/v1/content/comments/recent?pageSize=50")
	seen := map[string]map[string]any{}
	for _, item := range recent["items"].([]any) {
		comment := item.(map[string]any)
		seen[comment["content"].(string)] = comment
		if comment["status"] != "approved" {
			t.Fatalf("recent comment is not approved: %#v", comment)
		}
	}
	if seen["公开评论"] == nil || seen["公开评论"]["postSlug"] != "tie-a" {
		t.Fatalf("approved comment missing from recent list: %#v", recent["items"])
	}
	if seen["待审核"] != nil || seen["垃圾评论"] != nil || seen["草稿评论"] != nil {
		t.Fatalf("recent comments include hidden entries: %#v", recent["items"])
	}
	if pending["status"] != "pending" {
		t.Fatalf("pending comment = %#v", pending)
	}

	listed := get(t, public, server.URL+"/api/v1/content/posts?order=published&pageSize=100")
	for _, item := range listed["items"].([]any) {
		slug := item.(map[string]any)["slug"].(string)
		if slug == "hidden-draft" || slug == "api-draft" {
			t.Fatalf("anonymous recent list included draft %s", slug)
		}
	}
}

type timedPost struct {
	id   string
	slug string
	at   time.Time
}

func newAPIServer(t *testing.T, mailers ...service.Mailer) (*store.SQLiteRepository, *httptest.Server, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	repo, err := store.NewSQLiteRepository(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(repo, config.Settings{Version: "test", SessionTTLMin: 60}, slog.New(slog.NewTextHandler(io.Discard, nil)), mailers...).Handler())
	t.Cleanup(server.Close)
	t.Cleanup(func() { _ = repo.Close() })
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return repo, server, &http.Client{Jar: jar}
}

func setupSite(t *testing.T, client *http.Client, base, timezone string) {
	t.Helper()
	get(t, client, base+"/api/v1/system/status")
	postJSON(t, client, base+"/api/v1/setup", map[string]string{
		"siteName": "Next", "siteUrl": "https://example.com", "adminUsername": "owner",
		"adminPassword": "password1", "adminEmail": "owner@example.com", "timezone": timezone,
	}, http.StatusCreated)
}

func loginAs(t *testing.T, base, username, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	postJSON(t, client, base+"/api/v1/auth/login", map[string]string{"username": username, "password": password}, http.StatusOK)
	return client
}

func saveTimedPost(t *testing.T, repo *store.SQLiteRepository, id, slug, title string, published time.Time, status string) timedPost {
	t.Helper()
	post := store.Post{ID: id, Title: title, Slug: slug, Content: title, Status: status, AllowComment: true}
	if status == "published" {
		post.PublishedAt = published
	}
	saved, err := repo.SavePost(post)
	if err != nil {
		t.Fatal(err)
	}
	return timedPost{id: saved.ID, slug: saved.Slug, at: saved.PublishedAt}
}

func assertArchive(t *testing.T, client *http.Client, base, timezone string, posts []timedPost) {
	t.Helper()
	loc := time.UTC
	if timezone != "UTC" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			t.Fatal(err)
		}
		loc = loaded
	}
	expectCount := map[string]int{}
	expectSlugs := map[string]map[string]bool{}
	for _, post := range posts {
		month := post.at.In(loc).Format("2006-01")
		expectCount[month]++
		if expectSlugs[month] == nil {
			expectSlugs[month] = map[string]bool{}
		}
		expectSlugs[month][post.slug] = true
	}
	archive := get(t, client, base+"/api/v1/content/archive")
	gotCount := map[string]int{}
	for _, item := range archive["items"].([]any) {
		row := item.(map[string]any)
		gotCount[row["month"].(string)] = int(row["count"].(float64))
	}
	if len(gotCount) != len(expectCount) {
		t.Fatalf("archive months in %s = %#v, want %#v", timezone, gotCount, expectCount)
	}
	for month, count := range expectCount {
		if gotCount[month] != count {
			t.Fatalf("archive %s in %s = %d, want %d (%#v)", month, timezone, gotCount[month], count, gotCount)
		}
		page := get(t, client, base+"/api/v1/content/posts?month="+month+"&pageSize=100")
		gotSlugs := map[string]bool{}
		for _, item := range page["items"].([]any) {
			row := item.(map[string]any)
			if row["status"] != "published" {
				t.Fatalf("month %s included non-published post %#v", month, row)
			}
			gotSlugs[row["slug"].(string)] = true
		}
		if len(gotSlugs) != len(expectSlugs[month]) || page["total"].(float64) != float64(len(expectSlugs[month])) {
			t.Fatalf("month %s slugs = %#v, want %#v", month, gotSlugs, expectSlugs[month])
		}
		for slug := range expectSlugs[month] {
			if !gotSlugs[slug] {
				t.Fatalf("month %s missing %s", month, slug)
			}
		}
	}
}

func assertSameContent(t *testing.T, got any, want string) {
	t.Helper()
	text, _ := got.(string)
	if text != want {
		t.Fatalf("content length = %d, want %d", len([]rune(text)), len([]rune(want)))
	}
}

func neighborSlug(post map[string]any, field string) string {
	value, ok := post[field]
	if !ok || value == nil {
		return ""
	}
	neighbor, _ := value.(map[string]any)
	if neighbor == nil {
		return ""
	}
	slug, _ := neighbor["slug"].(string)
	return slug
}

func readFeed(t *testing.T, client *http.Client, rawURL string) (rssFeed, string) {
	t.Helper()
	response, err := client.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("feed status = %d, body = %s", response.StatusCode, payload)
	}
	var feed rssFeed
	if err := xml.Unmarshal(payload, &feed); err != nil {
		t.Fatal(err)
	}
	return feed, response.Header.Get("Content-Type")
}

func getStatus(t *testing.T, client *http.Client, rawURL string, want int) map[string]any {
	t.Helper()
	response, err := client.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return decodeBody(t, response, want)
}
