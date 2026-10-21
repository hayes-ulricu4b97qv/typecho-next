package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestPublishedOrderDraftsFeedAndRobots(t *testing.T) {
	repo, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	earlyAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	laterAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	early := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "较早", "slug": "early-post", "excerpt": "较早摘要", "content": "较早正文", "status": "published",
		"publishAt": earlyAt.Format(time.RFC3339),
	}, http.StatusCreated)
	later := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "较晚", "slug": "later-post", "content": strings.Repeat("晚", 400), "status": "published",
		"publishAt": laterAt.Format(time.RFC3339),
	}, http.StatusCreated)
	earlyPublished := early["publishedAt"].(string)
	putJSON(t, owner, server.URL+"/api/v1/content/posts/"+early["id"].(string), map[string]any{
		"title": "较早", "slug": "early-post", "excerpt": "较早摘要", "content": "改过的正文", "status": "published",
		"expectedUpdatedAt": early["updatedAt"],
	}, http.StatusOK)
	public := &http.Client{}
	listed := get(t, public, server.URL+"/api/v1/content/posts?pageSize=10")
	items := listed["items"].([]any)
	if len(items) < 2 || items[0].(map[string]any)["slug"] != "later-post" || items[1].(map[string]any)["slug"] != "early-post" {
		t.Fatalf("public order = %#v", items)
	}
	if _, ok := items[0].(map[string]any)["content"]; ok {
		t.Fatal("post list included a body")
	}
	if items[1].(map[string]any)["publishedAt"] != earlyPublished {
		t.Fatalf("editing moved publishedAt: before %s after %#v", earlyPublished, items[1])
	}
	sidebar := get(t, public, server.URL+"/api/v1/content/posts?order=published&pageSize=10")
	if sidebar["items"].([]any)[0].(map[string]any)["slug"] != items[0].(map[string]any)["slug"] {
		t.Fatal("homepage order differs from the published sidebar order")
	}
	detail := get(t, public, server.URL+"/api/v1/content/posts/early-post")
	if detail["content"] != "改过的正文" {
		t.Fatalf("detail body = %#v", detail["content"])
	}

	draft := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "草稿", "slug": "secret-draft", "content": "不能公开", "status": "draft",
	}, http.StatusCreated)
	ownerList := get(t, owner, server.URL+"/api/v1/content/posts?pageSize=20")
	if slugs(ownerList)["secret-draft"] {
		t.Fatal("owner list without includeDraft showed a draft")
	}
	withDrafts := get(t, owner, server.URL+"/api/v1/content/posts?includeDraft=true&pageSize=20")
	if !slugs(withDrafts)["secret-draft"] {
		t.Fatal("owner list with includeDraft hid the draft")
	}
	missing := getStatus(t, public, server.URL+"/api/v1/content/posts/secret-draft", http.StatusNotFound)
	if missing["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("anonymous draft = %#v", missing)
	}
	postJSON(t, owner, server.URL+"/api/v1/settings/users", map[string]string{
		"username": "editor2", "email": "editor2@example.com", "password": "password1", "role": "editor",
	}, http.StatusCreated)
	editor := loginAs(t, server.URL, "editor2", "password1")
	preview := get(t, editor, server.URL+"/api/v1/content/posts/secret-draft")
	if preview["content"] != "不能公开" || preview["id"] != draft["id"] {
		t.Fatalf("editor preview = %#v", preview)
	}

	for index := 0; index < 101; index++ {
		when := time.Now().UTC().Add(-time.Duration(index+3) * time.Hour)
		if _, err := repo.SavePost(store.Post{
			Title: "批量", Slug: fmt.Sprintf("bulk-%03d", index), Content: "批量正文", Status: "published", PublishedAt: when, AllowComment: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	feed, _ := readFeed(t, public, server.URL+"/feed.xml")
	if len(feed.Channel.Items) != 103 {
		t.Fatalf("feed items = %d, want all 103 published posts across pages", len(feed.Channel.Items))
	}
	if feed.Channel.Items[0].Title != "较晚" {
		t.Fatalf("feed order starts with %q", feed.Channel.Items[0].Title)
	}
	if feed.Channel.Items[1].Description != "较早摘要" {
		t.Fatalf("feed excerpt = %q", feed.Channel.Items[1].Description)
	}
	if feed.Channel.Items[0].Description == later["content"] || len([]rune(feed.Channel.Items[0].Description)) > 281 {
		t.Fatalf("feed used the full body: %d runes", len([]rune(feed.Channel.Items[0].Description)))
	}
	for _, item := range feed.Channel.Items {
		if strings.Contains(item.Link, "secret-draft") {
			t.Fatal("feed included a draft")
		}
	}

	robotsResp, err := public.Get(server.URL + "/robots.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer robotsResp.Body.Close()
	robotsText, _ := io.ReadAll(robotsResp.Body)
	robots := string(robotsText)
	if !strings.Contains(robots, "Allow: /") || !strings.Contains(robots, "Disallow: /admin") || !strings.Contains(robots, "Disallow: /setting") || !strings.Contains(robots, "Sitemap: https://example.com/sitemap.xml") {
		t.Fatalf("robots = %s", robots)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": contentSecurityPolicy,
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
	} {
		if got := robotsResp.Header.Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}

	tooBig := getStatus(t, public, server.URL+"/api/v1/content/posts?pageSize=101", http.StatusUnprocessableEntity)
	if tooBig["error"].(map[string]any)["code"] != "validation_error" || !containsHan(tooBig["error"].(map[string]any)["message"].(string)) {
		t.Fatalf("pageSize 101 = %#v", tooBig)
	}
}

func TestLoginLimitSessionLogAndChineseErrors(t *testing.T) {
	t.Setenv("APP_SESSION_TTL_MIN", "17")
	if config.FromEnv().SessionTTLMin != 17 {
		t.Fatalf("session ttl from env = %d", config.FromEnv().SessionTTLMin)
	}
	dir := t.TempDir()
	repo, err := store.NewSQLiteRepository(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	logs := &captureLog{}
	handler := NewServer(repo, config.Settings{Version: "test", SessionTTLMin: config.FromEnv().SessionTTLMin, LoginFailureLimit: 3}, slog.New(logs)).Handler()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := &http.Client{Jar: jar}
	setupSite(t, owner, server.URL, "UTC")
	for range 2 {
		wrong := postJSON(t, owner, server.URL+"/api/v1/auth/login", map[string]string{"username": "owner", "password": "wrong-pass"}, http.StatusUnauthorized)
		if wrong["error"].(map[string]any)["code"] != "invalid_credentials" || !containsHan(wrong["error"].(map[string]any)["message"].(string)) {
			t.Fatalf("wrong password = %#v", wrong)
		}
	}
	response := sendRaw(t, owner, http.MethodPost, server.URL+"/api/v1/auth/login", map[string]string{"username": "owner", "password": "password1"}, http.StatusOK)
	defer response.Body.Close()
	var found bool
	for _, cookie := range response.Cookies() {
		if cookie.Name != "app_session" || cookie.Value == "" {
			continue
		}
		found = true
		delta := time.Until(cookie.Expires)
		if delta < 16*time.Minute || delta > 18*time.Minute {
			t.Fatalf("session expires in %s", delta)
		}
	}
	if !found {
		t.Fatal("correct password did not set app_session")
	}
	if response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("login response missing nosniff")
	}
	missing := getStatus(t, owner, server.URL+"/api/v1/content/posts/missing-post", http.StatusNotFound)
	if missing["error"].(map[string]any)["code"] != "not_found" || !containsHan(missing["error"].(map[string]any)["message"].(string)) {
		t.Fatalf("not found = %#v", missing)
	}
	text := strings.Join(logs.lines(), "\n")
	if !strings.Contains(text, "status=200") || !strings.Contains(text, "status=404") {
		t.Fatalf("access log = %s", text)
	}
}

func TestFuturePublishRevisionsPassphraseCommentsMailAndBackup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APP_MEDIA_DIR", filepath.Join(dir, "uploads"))
	webDir := filepath.Join(dir, "web")
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<!doctype html><html><head><title>Typecho Next</title><meta name="description" content="{{META_DESCRIPTION}}"><meta name="keywords" content="{{META_KEYWORDS}}"><meta name="robots" content="{{ROBOTS_CONTENT}}"><meta property="og:type" content="{{OG_TYPE}}"><meta property="og:title" content="{{OG_TITLE}}"><meta property="og:description" content="{{OG_DESCRIPTION}}"><meta property="og:site_name" content="{{OG_SITE_NAME}}"><meta property="og:url" content="{{OG_URL}}"><link rel="canonical" href="{{CANONICAL_URL}}"></head><body><div id="app"></div></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WEB_DIR", webDir)
	repo, err := store.NewSQLiteRepository(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	server := httptest.NewServer(NewServer(repo, config.Settings{Version: "test", SessionTTLMin: 60, CommentLimit: 3}, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := &http.Client{Jar: jar}
	setupSite(t, owner, server.URL, "UTC")
	putJSON(t, owner, server.URL+"/api/v1/settings/site", map[string]string{
		"name": "海岸", "url": "https://example.com", "adminMail": "owner@example.com", "timezone": "UTC",
		"description": "海岸 & <笔记>", "keywords": "Go,博客 & Markdown",
	}, http.StatusOK)

	futureAt := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	future := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "明天", "slug": "tomorrow", "content": "还没到点", "status": "published",
		"publishAt": futureAt.Format(time.RFC3339),
	}, http.StatusCreated)
	publicJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	public := &http.Client{Jar: publicJar}
	get(t, public, server.URL+"/api/v1/system/status")
	hidden := getStatus(t, public, server.URL+"/api/v1/content/posts/tomorrow", http.StatusNotFound)
	if hidden["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("future post = %#v", hidden)
	}
	if slugs(get(t, public, server.URL+"/api/v1/content/posts"))["tomorrow"] {
		t.Fatal("future post is on the public list")
	}
	futureHTMLStatus, futureHTML := getShellResponse(t, public, server.URL+"/user/tomorrow")
	if futureHTMLStatus != http.StatusNotFound {
		t.Fatalf("future post shell status = %d, want 404", futureHTMLStatus)
	}
	if strings.Contains(futureHTML, "还没到点") {
		t.Fatal("future post body leaked into the anonymous HTML shell")
	}
	if future["publishedAt"] == nil || !strings.HasPrefix(future["publishedAt"].(string), futureAt.Format("2006-01-02T15:04:05")) {
		t.Fatalf("future publishedAt = %#v", future["publishedAt"])
	}

	originalAt := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	original := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "初稿", "slug": "stable-time", "content": "第一版", "status": "published", "customFields": map[string]string{"edition": "第一版字段"},
		"publishAt": originalAt.Format(time.RFC3339),
	}, http.StatusCreated)
	draftVersion := putJSON(t, owner, server.URL+"/api/v1/content/posts/"+original["id"].(string), map[string]any{
		"title": "初稿", "slug": "stable-time", "content": "第二版", "status": "draft",
		"expectedUpdatedAt": original["updatedAt"],
	}, http.StatusOK)
	republished := putJSON(t, owner, server.URL+"/api/v1/content/posts/"+original["id"].(string), map[string]any{
		"title": "初稿", "slug": "stable-time", "content": "第二版", "status": "published",
		"publishAt":         time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
		"expectedUpdatedAt": draftVersion["updatedAt"],
	}, http.StatusOK)
	if republished["publishedAt"] != original["publishedAt"] {
		t.Fatalf("republish changed publishedAt from %v to %v", original["publishedAt"], republished["publishedAt"])
	}
	if republished["customFields"].(map[string]any)["edition"] != "第一版字段" {
		t.Fatalf("an update without customFields cleared the existing values: %#v", republished)
	}
	revisions := get(t, owner, server.URL+"/api/v1/content/posts/"+original["id"].(string)+"/revisions")
	revItems := revisions["items"].([]any)
	if len(revItems) == 0 || revItems[0].(map[string]any)["title"] != "初稿" {
		t.Fatalf("revisions = %#v", revItems)
	}
	if _, ok := revItems[0].(map[string]any)["content"]; ok {
		t.Fatalf("revision list unexpectedly returned the body: %#v", revItems[0])
	}
	if _, ok := revItems[0].(map[string]any)["customFields"]; ok {
		t.Fatalf("revision list unexpectedly returned custom fields: %#v", revItems[0])
	}
	restored := postJSON(t, owner, server.URL+"/api/v1/content/posts/"+original["id"].(string)+"/revisions/"+revItems[0].(map[string]any)["id"].(string)+"/restore", map[string]any{}, http.StatusOK)
	if restored["content"] != "第一版" || restored["customFields"].(map[string]any)["edition"] != "第一版字段" {
		t.Fatalf("restored = %#v", restored["content"])
	}
	retained := get(t, owner, server.URL+"/api/v1/content/posts/stable-time")
	if retained["customFields"].(map[string]any)["edition"] != "第一版字段" {
		t.Fatalf("an update without customFields cleared the existing values: %#v", retained)
	}
	cleared := putJSON(t, owner, server.URL+"/api/v1/content/posts/"+original["id"].(string), map[string]any{
		"title": "初稿", "slug": "stable-time", "content": "第一版", "status": "published", "customFields": map[string]string{},
		"expectedUpdatedAt": retained["updatedAt"],
	}, http.StatusOK)
	if _, ok := cleared["customFields"]; ok {
		t.Fatalf("an explicit empty customFields object did not clear fields: %#v", cleared["customFields"])
	}
	publicList := get(t, public, server.URL+"/api/v1/content/posts?q="+urlQuery("初稿"))
	for _, item := range publicList["items"].([]any) {
		if _, ok := item.(map[string]any)["customFields"]; ok {
			t.Fatal("post list returned custom fields")
		}
	}
	invalidCustomFields := []map[string]any{
		{"title": "重复字段", "slug": "duplicate-fields", "content": "Body", "status": "draft", "customFields": map[string]string{"field": "one", " field ": "two"}},
		{"title": "字段名过长", "slug": "long-custom-key", "content": "Body", "status": "draft", "customFields": map[string]string{strings.Repeat("k", 81): "value"}},
		{"title": "字段过长", "slug": "long-custom-field", "content": "Body", "status": "draft", "customFields": map[string]string{"field": strings.Repeat("值", 2001)}},
		{"title": "字段数过多", "slug": "too-many-fields", "content": "Body", "status": "draft", "customFields": func() map[string]string {
			fields := make(map[string]string, 51)
			for index := 0; index < 51; index++ {
				fields[fmt.Sprintf("field-%02d", index)] = "value"
			}
			return fields
		}()},
		{"title": "字段总量过大", "slug": "too-many-field-runes", "content": "Body", "status": "draft", "customFields": func() map[string]string {
			fields := make(map[string]string, 11)
			for index := 0; index < 11; index++ {
				fields[fmt.Sprintf("field-%02d", index)] = strings.Repeat("v", 1900)
			}
			return fields
		}()},
	}
	for _, payload := range invalidCustomFields {
		response := postJSON(t, owner, server.URL+"/api/v1/content/posts", payload, http.StatusUnprocessableEntity)
		if response["error"].(map[string]any)["code"] != "validation_error" {
			t.Fatalf("invalid custom fields were not rejected: %#v", response)
		}
	}

	locked := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "口令", "slug": "locked-post", "content": "只有口令能看", "status": "published", "passphrase": "secret-pass", "customFields": map[string]string{"secret": "隐藏字段"},
	}, http.StatusCreated)
	if locked["content"] != "只有口令能看" {
		t.Fatalf("author create hid the body: %#v", locked["content"])
	}
	lockedList := get(t, public, server.URL+"/api/v1/content/posts?q="+urlQuery("口令"))
	for _, item := range lockedList["items"].([]any) {
		post := item.(map[string]any)
		if _, ok := post["content"]; ok {
			t.Fatal("locked list item included a body")
		}
		if _, ok := post["customFields"]; ok {
			t.Fatal("locked list item included custom fields")
		}
	}
	denied := get(t, public, server.URL+"/api/v1/content/posts/locked-post")
	if _, ok := denied["content"]; ok || denied["locked"] != true {
		t.Fatalf("locked detail = %#v", denied)
	}
	if _, ok := denied["customFields"]; ok {
		t.Fatalf("locked detail returned custom fields: %#v", denied)
	}
	wrong := getWithHeader(t, server.URL+"/api/v1/content/posts/locked-post", "X-Post-Passphrase", "nope")
	if _, ok := wrong["content"]; ok {
		t.Fatal("wrong passphrase returned the body")
	}
	if _, ok := wrong["customFields"]; ok {
		t.Fatal("wrong passphrase returned custom fields")
	}
	opened := getWithHeader(t, server.URL+"/api/v1/content/posts/locked-post", "X-Post-Passphrase", "secret-pass")
	if opened["content"] != "只有口令能看" || opened["customFields"].(map[string]any)["secret"] != "隐藏字段" {
		t.Fatalf("unlocked = %#v", opened["content"])
	}
	anonymousFeed := getText(t, public, server.URL+"/feed.xml")
	if strings.Contains(anonymousFeed, "只有口令能看") {
		t.Fatal("anonymous feed included a locked body")
	}
	if !strings.Contains(anonymousFeed, "locked-post") || !strings.Contains(anonymousFeed, "第一版") {
		t.Fatal("anonymous feed dropped the locked listing or an unlocked body")
	}
	wrongFeed := getTextWithHeader(t, public, server.URL+"/feed.xml", "X-Post-Passphrase", "nope")
	if strings.Contains(wrongFeed, "只有口令能看") {
		t.Fatal("wrong passphrase feed included a locked body")
	}
	openedFeed := getTextWithHeader(t, public, server.URL+"/feed.xml", "X-Post-Passphrase", "secret-pass")
	if !strings.Contains(openedFeed, "只有口令能看") {
		t.Fatal("feed with the matching passphrase omitted the locked body")
	}

	pending := postJSON(t, public, server.URL+"/api/v1/content/posts/stable-time/comments", map[string]string{
		"authorName": "读者", "content": "第一条正常评论",
	}, http.StatusAccepted)
	if pending["status"] != "pending" {
		t.Fatalf("normal comment = %#v", pending)
	}
	postJSON(t, public, server.URL+"/api/v1/content/posts/stable-time/comments", map[string]string{"authorName": "读者", "content": "第二条"}, http.StatusAccepted)
	postJSON(t, public, server.URL+"/api/v1/content/posts/stable-time/comments", map[string]string{"authorName": "读者", "content": "第三条"}, http.StatusAccepted)
	burst := postJSON(t, public, server.URL+"/api/v1/content/posts/stable-time/comments", map[string]string{"authorName": "读者", "content": "第四条"}, http.StatusTooManyRequests)
	if burst["error"].(map[string]any)["code"] != "rate_limited" {
		t.Fatalf("comment burst = %#v", burst)
	}
	visibleComments := get(t, public, server.URL+"/api/v1/content/posts/stable-time/comments")
	for _, item := range visibleComments["items"].([]any) {
		if item.(map[string]any)["content"] == "第四条" {
			t.Fatal("rate limited comment was stored on the public list")
		}
	}
	mailPage, err := repo.ListOutboundMail(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMail(mailPage.Items, "owner@example.com", "新评论待审核", "第一条正常评论") {
		t.Fatalf("comment mail = %#v", mailPage.Items)
	}

	putJSON(t, owner, server.URL+"/api/v1/settings/reading", map[string]any{
		"postsPerPage": 10, "commentsEnabled": true, "commentsRequireModeration": false, "commentsThreaded": true,
	}, http.StatusOK)
	approvedServer := newLimitedServer(t, 30)
	setupSite(t, approvedServer.owner, approvedServer.server.URL, "UTC")
	postJSON(t, approvedServer.owner, approvedServer.server.URL+"/api/v1/content/posts", map[string]any{
		"title": "可评", "slug": "open-comments", "content": "正文", "status": "published",
	}, http.StatusCreated)
	putJSON(t, approvedServer.owner, approvedServer.server.URL+"/api/v1/settings/reading", map[string]any{
		"postsPerPage": 10, "commentsEnabled": true, "commentsRequireModeration": false, "commentsThreaded": false,
	}, http.StatusOK)
	readerJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	reader := &http.Client{Jar: readerJar}
	get(t, reader, approvedServer.server.URL+"/api/v1/system/status")
	approved := postJSON(t, reader, approvedServer.server.URL+"/api/v1/content/posts/open-comments/comments", map[string]string{
		"authorName": "读者", "content": "直接公开",
	}, http.StatusCreated)
	if approved["status"] != "approved" {
		t.Fatalf("approved comment = %#v", approved)
	}

	upload := uploadGIF(t, owner, server.URL)
	if !strings.HasPrefix(upload["url"].(string), "/media/") {
		t.Fatalf("media = %#v", upload)
	}
	backupResp, err := owner.Get(server.URL + "/api/v1/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer backupResp.Body.Close()
	if backupResp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(backupResp.Body)
		t.Fatalf("backup status = %d body = %s", backupResp.StatusCode, payload)
	}
	payload, err := io.ReadAll(backupResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	var sawDB, sawMedia bool
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == "app.db" && bytes.HasPrefix(data, []byte("SQLite format 3")) {
			sawDB = true
		}
		if strings.HasPrefix(file.Name, "media/") && bytes.Contains(data, []byte("GIF89a")) {
			sawMedia = true
		}
	}
	if !sawDB || !sawMedia {
		t.Fatalf("backup files db=%v media=%v", sawDB, sawMedia)
	}

	page := postJSON(t, owner, server.URL+"/api/v1/content/pages", map[string]any{
		"title": "关于海岸", "slug": "about-shore", "content": "页面正文甲", "status": "published",
	}, http.StatusCreated)
	postHTML := getText(t, public, server.URL+"/user/stable-time")
	if !strings.Contains(postHTML, "初稿") || !strings.Contains(postHTML, "第一版") || strings.Contains(postHTML, "<title>Typecho Next</title>") {
		t.Fatalf("post html = %s", postHTML)
	}
	for _, expected := range []string{
		`<meta name="description" content="海岸 &amp; &lt;笔记&gt;">`,
		`<meta name="keywords" content="Go,博客 &amp; Markdown">`,
		`<meta property="og:title" content="初稿 - 海岸">`,
		`<meta property="og:type" content="article">`,
		`<link rel="canonical" href="https://example.com/user/stable-time">`,
	} {
		if !strings.Contains(postHTML, expected) {
			t.Fatalf("post metadata missing %q: %s", expected, postHTML)
		}
	}
	pageHTML := getText(t, public, server.URL+"/page/"+page["slug"].(string))
	if !strings.Contains(pageHTML, "关于海岸") || !strings.Contains(pageHTML, "页面正文甲") || !strings.Contains(pageHTML, `<meta property="og:type" content="article">`) {
		t.Fatalf("page html = %s", pageHTML)
	}
	homeHTML := getText(t, public, server.URL+"/user")
	if !strings.Contains(homeHTML, "海岸") || strings.Contains(homeHTML, "<title>Typecho Next</title>") || !strings.Contains(homeHTML, `<meta property="og:type" content="website">`) {
		t.Fatalf("home html = %s", homeHTML)
	}
	adminHTML := getText(t, owner, server.URL+"/admin")
	if !strings.Contains(adminHTML, `<meta name="robots" content="noindex, nofollow">`) {
		t.Fatalf("admin shell is indexable: %s", adminHTML)
	}
	previewHTML := getText(t, owner, server.URL+"/user/stable-time?preview=true")
	if !strings.Contains(previewHTML, `<meta name="robots" content="noindex, nofollow">`) {
		t.Fatalf("preview shell is indexable: %s", previewHTML)
	}
	lockedHTML := getText(t, public, server.URL+"/user/locked-post")
	if strings.Contains(lockedHTML, "只有口令能看") {
		t.Fatal("locked html included the body")
	}
}

func TestPublicShellRendersSafeMarkdownWithoutJavaScript(t *testing.T) {
	webDir := t.TempDir()
	index := `<!doctype html><html><head><title>Typecho Next</title><meta name="description" content="{{META_DESCRIPTION}}"><meta name="keywords" content="{{META_KEYWORDS}}"><meta name="robots" content="{{ROBOTS_CONTENT}}"><meta property="og:type" content="{{OG_TYPE}}"><meta property="og:title" content="{{OG_TITLE}}"><meta property="og:description" content="{{OG_DESCRIPTION}}"><meta property="og:site_name" content="{{OG_SITE_NAME}}"><meta property="og:url" content="{{OG_URL}}"><link rel="canonical" href="{{CANONICAL_URL}}"></head><body><div id="app"></div></body></html>`
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WEB_DIR", webDir)
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")

	markdown := "## Server Markdown\n\n**bold** and [safe](https://example.com)\n\n![diagram](/media/example.png)\n\n[unsafe](javascript:alert%281%29) [entity-unsafe](java&#x73;cript:alert%284%29) [data-unsafe](data:text/html,alert%285%29)\n\n<script>alert(2)</script>\n<img src=x onerror=alert(3)>"
	postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Rendered", "slug": "server-markdown", "content": markdown, "status": "published",
	}, http.StatusCreated)
	document := getText(t, &http.Client{}, server.URL+"/user/server-markdown")
	postJSON(t, owner, server.URL+"/api/v1/content/pages", map[string]any{
		"title": "Page Markdown", "slug": "page-markdown", "content": markdown, "status": "published",
	}, http.StatusCreated)
	pageDocument := getText(t, &http.Client{}, server.URL+"/page/page-markdown")
	for _, expected := range []string{
		"<h2>Server Markdown</h2>",
		"<strong>bold</strong>",
		`<a href="https://example.com">safe</a>`,
		`<img src="/media/example.png" alt="diagram">`,
	} {
		if !strings.Contains(document, expected) {
			t.Errorf("server-rendered document is missing %q: %s", expected, document)
		}
	}
	if !strings.Contains(pageDocument, "<h2>Server Markdown</h2>") || !strings.Contains(pageDocument, "<strong>bold</strong>") {
		t.Fatalf("page shell did not render Markdown: %s", pageDocument)
	}
	lowerDocument := strings.ToLower(document)
	for _, unsafe := range []string{"<script", "onerror=", "javascript:", "data:text/html", "alert(2)", "alert(3)", "alert(4)", "alert(5)"} {
		if strings.Contains(lowerDocument, unsafe) {
			t.Errorf("server-rendered document contains unsafe content %q: %s", unsafe, document)
		}
	}
	var requests sync.WaitGroup
	requestErrors := make(chan error, 16)
	client := &http.Client{Timeout: 5 * time.Second}
	for range 16 {
		requests.Add(1)
		go func() {
			defer requests.Done()
			response, err := client.Get(server.URL + "/user/server-markdown")
			if err != nil {
				requestErrors <- err
				return
			}
			defer response.Body.Close()
			payload, err := io.ReadAll(response.Body)
			if err != nil {
				requestErrors <- err
				return
			}
			if response.StatusCode != http.StatusOK || !bytes.Contains(payload, []byte("<h2>Server Markdown</h2>")) {
				requestErrors <- fmt.Errorf("concurrent page response status=%d body=%s", response.StatusCode, payload)
			}
		}()
	}
	requests.Wait()
	close(requestErrors)
	for err := range requestErrors {
		t.Error(err)
	}
}

func TestSearchHTTPMatchesBodyLiterally(t *testing.T) {
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "普通", "slug": "search-body", "content": "正文独有词青鸟", "status": "published",
	}, http.StatusCreated)
	postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "百分", "slug": "search-percent", "content": "完成 100%", "status": "published",
	}, http.StatusCreated)
	postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "其他", "slug": "search-other", "content": "完成 100X", "status": "published",
	}, http.StatusCreated)
	public := &http.Client{}
	found := slugs(get(t, public, server.URL+"/api/v1/content/posts?q="+urlQuery("青鸟")))
	if !found["search-body"] || found["search-other"] {
		t.Fatalf("body search = %#v", found)
	}
	percents := slugs(get(t, public, server.URL+"/api/v1/content/posts?q="+urlQuery("100%")))
	if !percents["search-percent"] || percents["search-other"] {
		t.Fatalf("literal percent search = %#v", percents)
	}
}

type captureLog struct {
	mu   sync.Mutex
	text []string
}

func (c *captureLog) Enabled(context.Context, slog.Level) bool { return true }
func (c *captureLog) Handle(_ context.Context, record slog.Record) error {
	var builder strings.Builder
	builder.WriteString(record.Message)
	record.Attrs(func(attr slog.Attr) bool {
		builder.WriteByte(' ')
		builder.WriteString(attr.Key)
		builder.WriteByte('=')
		builder.WriteString(attr.Value.String())
		return true
	})
	c.mu.Lock()
	c.text = append(c.text, builder.String())
	c.mu.Unlock()
	return nil
}
func (c *captureLog) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *captureLog) WithGroup(string) slog.Handler      { return c }
func (c *captureLog) lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.text...)
}

type limitedServer struct {
	server *httptest.Server
	owner  *http.Client
}

func newLimitedServer(t *testing.T, commentLimit int) limitedServer {
	t.Helper()
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	server := httptest.NewServer(NewServer(repo, config.Settings{Version: "test", SessionTTLMin: 60, CommentLimit: commentLimit}, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return limitedServer{server: server, owner: &http.Client{Jar: jar}}
}

func slugs(page map[string]any) map[string]bool {
	found := map[string]bool{}
	items, _ := page["items"].([]any)
	for _, item := range items {
		if slug, _ := item.(map[string]any)["slug"].(string); slug != "" {
			found[slug] = true
		}
	}
	return found
}

func urlQuery(value string) string {
	return url.QueryEscape(value)
}

func sendRaw(t *testing.T, client *http.Client, method, url string, body any, want int) *http.Response {
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
	if response.StatusCode != want {
		defer response.Body.Close()
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want %d, body = %s", response.StatusCode, want, payload)
	}
	return response
}

func getWithHeader(t *testing.T, url, header, value string) map[string]any {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(header, value)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return decodeBody(t, response, http.StatusOK)
}

func getTextWithHeader(t *testing.T, client *http.Client, rawURL, header, value string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(header, value)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.StatusCode, payload)
	}
	return string(payload)
}

func getText(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.StatusCode, payload)
	}
	return string(payload)
}

func hasMail(mail []store.OutboundMail, recipient, subject, bodyPart string) bool {
	for _, item := range mail {
		if item.Recipient == recipient && item.Subject == subject && strings.Contains(item.Body, bodyPart) {
			return true
		}
	}
	return false
}

func resetToken(body string) string {
	const marker = "重置口令："
	index := strings.Index(body, marker)
	if index < 0 {
		return ""
	}
	token := body[index+len(marker):]
	if end := strings.IndexByte(token, '\n'); end >= 0 {
		token = token[:end]
	}
	return strings.TrimSpace(token)
}

func uploadGIF(t *testing.T, client *http.Client, base string) map[string]any {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "dot.gif")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x00\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/media", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
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
	return decodeBody(t, response, http.StatusCreated)
}
