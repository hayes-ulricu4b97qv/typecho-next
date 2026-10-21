package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestFrontendRejectsBackslashTraversalBeforeStaticLookup(t *testing.T) {
	root := t.TempDir()
	webDir := filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.js"), []byte("outside web root"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WEB_DIR", webDir)

	repo, err := store.NewSQLiteRepository(filepath.Join(root, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	server := NewServer(repo, config.Settings{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/assets/", nil)
	request.URL.Path = `/assets/..\..\secret.js`
	response := httptest.NewRecorder()

	server.frontend(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("backslash traversal returned %d, want %d", response.Code, http.StatusNotFound)
	}
	if strings.Contains(response.Body.String(), "outside web root") {
		t.Fatal("static route returned a file outside the web root")
	}
}

func TestFrontendFallbackOnlyAcceptsGetAndHead(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<html><body><div id="app"></div></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WEB_DIR", webDir)
	_, server, owner := newAPIServer(t)
	client := &http.Client{}
	for _, path := range []string{"/", "/user"} {
		for _, method := range []string{
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		} {
			request, err := http.NewRequest(method, server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != "GET, HEAD" {
				t.Errorf("%s %s before setup returned %d Allow=%q, want 405 and GET, HEAD", method, path, response.StatusCode, response.Header.Get("Allow"))
			}
		}
	}
	setupSite(t, owner, server.URL, "UTC")
	for _, path := range []string{"/", "/user"} {
		for _, method := range []string{
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		} {
			request, err := http.NewRequest(method, server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := owner.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != "GET, HEAD" {
				t.Errorf("%s %s after setup returned %d Allow=%q, want 405 and GET, HEAD", method, path, response.StatusCode, response.Header.Get("Allow"))
			}
		}
	}

	request, err := http.NewRequest(http.MethodHead, server.URL+"/user", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := owner.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HEAD /user status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Fatalf("HEAD /user returned %d body bytes", len(body))
	}
}

func TestPublicShellRendersSafePaginatedPostListWithoutJavaScript(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<html><head><title>{{TITLE}}</title></head><body><div id="app"></div></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WEB_DIR", webDir)
	repo, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	putJSON(t, owner, server.URL+"/api/v1/settings/reading", map[string]any{
		"postsPerPage": 2, "commentsEnabled": true, "commentsRequireModeration": true, "commentsThreaded": true,
	}, http.StatusOK)

	news := []store.Term{{Name: "News", Kind: "category"}}
	other := []store.Term{{Name: "Other", Kind: "category"}}
	posts := []store.Post{
		{Title: `<script>alert("x")</script>`, Slug: "newest", Excerpt: `<img src=x onerror=alert(1)>`, Content: "BODY_MUST_NOT_RENDER", Status: "published", PublishedAt: time.Date(2025, time.April, 10, 8, 0, 0, 0, time.UTC), Categories: news, Tags: []store.Term{{Name: `<svg onload=alert(2)>`, Kind: "tag"}}},
		{Title: "Second story", Slug: "second", Excerpt: "Safe excerpt", Content: "unique-search-token BODY_MUST_NOT_RENDER", Status: "published", PublishedAt: time.Date(2025, time.March, 20, 8, 0, 0, 0, time.UTC), Categories: news},
		{Title: "Third story", Slug: "third", Excerpt: "Other category", Content: "third body", Status: "published", PublishedAt: time.Date(2025, time.March, 15, 8, 0, 0, 0, time.UTC), Categories: other},
		{Title: "Fourth story", Slug: "fourth", Excerpt: "Older news", Content: "fourth body", Status: "published", PublishedAt: time.Date(2025, time.February, 1, 8, 0, 0, 0, time.UTC), Categories: news},
		{Title: "Draft secret", Slug: "draft-secret", Content: "unique-search-token", Status: "draft", Categories: news},
		{Title: "Future secret", Slug: "future-secret", Content: "unique-search-token", Status: "published", PublishedAt: time.Now().UTC().Add(24 * time.Hour), Categories: news},
	}
	for _, post := range posts {
		if _, err := repo.SavePost(post); err != nil {
			t.Fatal(err)
		}
	}

	client := &http.Client{}
	defaultHTML := getShellHTML(t, client, server.URL+"/user?pageSize=100")
	if got := strings.Count(defaultHTML, `<article class="post-card">`); got != 2 {
		t.Fatalf("default page rendered %d posts, want configured limit 2", got)
	}
	for _, want := range []string{
		`共 4 篇`,
		`href="/user/newest"`,
		`href="/user/second"`,
		`href="/user?page=2"`,
		`&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`,
		`&lt;img src=x onerror=alert(1)&gt;`,
		`&lt;svg onload=alert(2)&gt;`,
	} {
		if !strings.Contains(defaultHTML, want) {
			t.Errorf("default shell missing %q", want)
		}
	}
	for _, hidden := range []string{"BODY_MUST_NOT_RENDER", "Draft secret", "Future secret", "draft-secret", "future-secret", "<script>alert", "<svg onload"} {
		if strings.Contains(defaultHTML, hidden) {
			t.Errorf("default shell leaked %q", hidden)
		}
	}

	pageTwo := getShellHTML(t, client, server.URL+"/user?page=2&pageSize=100")
	if strings.Count(pageTwo, `<article class="post-card">`) != 2 || !strings.Contains(pageTwo, `href="/user/third"`) || !strings.Contains(pageTwo, `href="/user/fourth"`) || !strings.Contains(pageTwo, `2 / 2`) {
		t.Fatalf("second page did not follow the configured page size: %s", pageTwo)
	}
	lastPage := getShellHTML(t, client, server.URL+"/user?page=999")
	if !strings.Contains(lastPage, `2 / 2`) || !strings.Contains(lastPage, `href="/user/fourth"`) {
		t.Fatalf("out-of-range page was not clamped: %s", lastPage)
	}

	searchHTML := getShellHTML(t, client, server.URL+"/user?q=unique-search-token")
	if !strings.Contains(searchHTML, `共 1 篇`) || !strings.Contains(searchHTML, `href="/user/second"`) || strings.Contains(searchHTML, "Draft secret") || strings.Contains(searchHTML, "Future secret") {
		t.Fatalf("search shell returned wrong posts: %s", searchHTML)
	}
	categoryHTML := getShellHTML(t, client, server.URL+"/user?category=news")
	if !strings.Contains(categoryHTML, `共 3 篇`) || !strings.Contains(categoryHTML, `name="category" value="news"`) || !strings.Contains(categoryHTML, `href="/user?category=news&amp;page=2"`) {
		t.Fatalf("category shell returned wrong posts: %s", categoryHTML)
	}
	categoryPageTwo := getShellHTML(t, client, server.URL+"/user?category=news&page=2")
	if !strings.Contains(categoryPageTwo, `href="/user/fourth"`) || strings.Contains(categoryPageTwo, `href="/user/third"`) || !strings.Contains(categoryPageTwo, `共 3 篇`) {
		t.Fatalf("category pagination did not preserve its filter: %s", categoryPageTwo)
	}
	monthHTML := getShellHTML(t, client, server.URL+"/user?month=2025-03")
	if !strings.Contains(monthHTML, `共 2 篇`) || !strings.Contains(monthHTML, `2025-03 的文章`) || strings.Contains(monthHTML, `href="/user/newest"`) {
		t.Fatalf("month shell returned wrong posts: %s", monthHTML)
	}
	combinedHTML := getShellHTML(t, client, server.URL+"/user?q=unique-search-token&category=news&month=2025-03")
	if !strings.Contains(combinedHTML, `共 1 篇`) || !strings.Contains(combinedHTML, `href="/user/second"`) {
		t.Fatalf("combined filters returned wrong posts: %s", combinedHTML)
	}
}

func TestPublicShellRendersAnEmptyPostState(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<html><body><div id="app"></div></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WEB_DIR", webDir)
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	body := getShellHTML(t, &http.Client{}, server.URL+"/user")
	if !strings.Contains(body, `还没有公开文章`) || !strings.Contains(body, `href="/admin"`) || strings.Contains(body, `<article class="post-card">`) {
		t.Fatalf("empty shell = %s", body)
	}
}

func TestPublicDetailShellReturnsNotFoundForUnavailableContent(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<html><body><div id="app"></div></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WEB_DIR", webDir)
	repo, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	posts := []store.Post{
		{Title: "Draft post", Slug: "draft-shell", Status: "draft"},
		{Title: "Future post", Slug: "future-shell", Status: "published", PublishedAt: time.Now().UTC().Add(time.Hour)},
	}
	for _, post := range posts {
		if _, err := repo.SavePost(post); err != nil {
			t.Fatal(err)
		}
	}
	pages := []store.Page{
		{Title: "Draft page", Slug: "draft-shell", Status: "draft"},
		{Title: "Future page", Slug: "future-shell", Status: "published", PublishedAt: time.Now().UTC().Add(time.Hour)},
		{Title: "Published page", Slug: "published-shell", Content: "Visible page body", Status: "published", PublishedAt: time.Now().UTC().Add(-time.Hour)},
	}
	for _, page := range pages {
		if _, err := repo.SavePage(page); err != nil {
			t.Fatal(err)
		}
	}

	for _, route := range []string{
		"/user/missing-shell",
		"/user/draft-shell",
		"/user/future-shell",
		"/user/nested/invalid-route",
		"/page/missing-shell",
		"/page/draft-shell",
		"/page/future-shell",
		"/page/nested/invalid-route",
	} {
		status, body := getShellResponse(t, &http.Client{}, server.URL+route)
		if status != http.StatusNotFound {
			t.Errorf("GET %s returned %d, want 404 (body: %s)", route, status, body)
		}
		if strings.Contains(body, "Draft post") || strings.Contains(body, "Future post") || strings.Contains(body, "Draft page") || strings.Contains(body, "Future page") {
			t.Errorf("GET %s exposed unavailable content: %s", route, body)
		}
	}
	pageList := get(t, &http.Client{}, server.URL+"/api/v1/content/pages")
	items := pageList["items"].([]any)
	if pageList["total"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["slug"] != "published-shell" {
		t.Fatalf("public page list included draft/future pages or hid a published page: %#v", pageList)
	}
	futurePage := getStatus(t, &http.Client{}, server.URL+"/api/v1/content/pages/future-shell", http.StatusNotFound)
	if futurePage["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("future page API response = %#v", futurePage)
	}
	if status, body := getShellResponse(t, &http.Client{}, server.URL+"/page/published-shell"); status != http.StatusOK || !strings.Contains(body, "Published page") || !strings.Contains(body, "Visible page body") {
		t.Fatalf("published page shell = %d %s", status, body)
	}
	response, err := (&http.Client{}).Get(server.URL + "/sitemap.xml")
	if err != nil {
		t.Fatal(err)
	}
	sitemap, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(sitemap), "/page/published-shell") || strings.Contains(string(sitemap), "future-shell") || strings.Contains(string(sitemap), "draft-shell") {
		t.Fatalf("sitemap page visibility is wrong: status %d body %s", response.StatusCode, sitemap)
	}
}

func getShellHTML(t *testing.T, client *http.Client, rawURL string) string {
	t.Helper()
	status, body := getShellResponse(t, client, rawURL)
	if status != http.StatusOK {
		t.Fatalf("GET %s returned %d", rawURL, status)
	}
	return body
}

func getShellResponse(t *testing.T, client *http.Client, rawURL string) (int, string) {
	t.Helper()
	response, err := client.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}
