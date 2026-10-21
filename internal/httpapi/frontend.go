package httpapi

import (
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func (s *Server) serveAppShell(w http.ResponseWriter, r *http.Request, cleanPath string) {
	// This HTML is rendered from live site data and may contain authorized drafts.
	// Never let a browser or shared intermediary retain a user-specific shell.
	w.Header().Set("Cache-Control", "no-store")
	raw, err := os.ReadFile(filepath.Join(s.webDir, "index.html"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	siteName := "Typecho Next"
	description := ""
	keywords := ""
	var site *store.Site
	initialized, err := s.repo.InitializedContext(r.Context())
	if r.Context().Err() != nil {
		return
	}
	if err == nil && initialized {
		if loaded, err := s.app.SiteContext(r.Context()); err == nil && loaded != nil && strings.TrimSpace(loaded.Name) != "" {
			site = loaded
			siteName = loaded.Name
			description = strings.TrimSpace(loaded.Description)
			keywords = strings.TrimSpace(loaded.Keywords)
		}
	}
	if r.Context().Err() != nil {
		return
	}
	title := siteName
	body := "<h1>" + html.EscapeString(siteName) + "</h1>"
	if description != "" {
		body += "<p>" + html.EscapeString(description) + "</p>"
	}
	openGraphType := "website"
	privatePreview := false
	if cleanPath == "/user" && site != nil {
		body = s.renderServerPostList(r, site)
	}
	if strings.HasPrefix(cleanPath, "/user/") {
		slug, ok := routeSlug(cleanPath, "/user/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		canEdit, err := s.contentEditorAccess(r.Context(), r)
		if r.Context().Err() != nil {
			return
		}
		if err != nil {
			s.writeRepositoryUnavailable(w, r, err)
			return
		}
		post, err := s.app.GetPostContext(r.Context(), slug, canEdit)
		if err != nil && r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.logErrorRateLimited(r.Context(), "public-post-shell-read", "read public post shell", "slug", slug, "error", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		privatePreview = post.Status != "published" || post.PublishedAt.IsZero() || post.PublishedAt.After(time.Now().UTC())
		title = post.Title + " - " + siteName
		if strings.TrimSpace(post.Excerpt) != "" {
			description = strings.TrimSpace(post.Excerpt)
		}
		body = `<article class="article-detail"><header class="article-head"><h1>` + html.EscapeString(post.Title) + `</h1></header>`
		if strings.TrimSpace(post.Excerpt) != "" {
			body += `<p class="article-excerpt">` + html.EscapeString(post.Excerpt) + `</p>`
		}
		if post.PassphraseHash == "" {
			body += `<div class="article-body article-rich">` + renderServerMarkdown(post.Content) + `</div>`
		}
		body += "</article>"
		openGraphType = "article"
	}
	if strings.HasPrefix(cleanPath, "/page/") {
		slug, ok := routeSlug(cleanPath, "/page/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		canEdit, err := s.contentEditorAccess(r.Context(), r)
		if r.Context().Err() != nil {
			return
		}
		if err != nil {
			s.writeRepositoryUnavailable(w, r, err)
			return
		}
		page, err := s.app.GetPageContext(r.Context(), slug, canEdit)
		if err != nil && r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.logErrorRateLimited(r.Context(), "public-page-shell-read", "read public page shell", "slug", slug, "error", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		privatePreview = page.Status != "published" || page.PublishedAt.IsZero() || page.PublishedAt.After(time.Now().UTC())
		title = page.Title + " - " + siteName
		if strings.TrimSpace(page.Excerpt) != "" {
			description = strings.TrimSpace(page.Excerpt)
		}
		body = `<article class="article-detail page-detail"><header class="article-head"><h1>` + html.EscapeString(page.Title) + `</h1></header>`
		if strings.TrimSpace(page.Excerpt) != "" {
			body += `<p class="article-excerpt">` + html.EscapeString(page.Excerpt) + `</p>`
		}
		body += `<div class="article-body article-rich">` + renderServerMarkdown(page.Content) + `</div></article>`
		openGraphType = "article"
	}
	noIndex := ""
	if cleanPath == "/admin" || strings.HasPrefix(cleanPath, "/admin/") || cleanPath == "/setting" || strings.HasPrefix(cleanPath, "/setting/") || privatePreview || r.URL.Query().Get("preview") == "true" {
		noIndex = "noindex, nofollow"
	}
	canonical := ""
	if site != nil && strings.TrimSpace(site.URL) != "" {
		canonical = strings.TrimRight(site.URL, "/") + (&url.URL{Path: cleanPath}).EscapedPath()
	}
	if site == nil && (cleanPath == "/admin" || strings.HasPrefix(cleanPath, "/setting")) {
		body = "<h1>" + html.EscapeString(siteName) + "</h1>"
	}

	htmlText := replaceHTMLTitle(string(raw), title)
	values := map[string]string{
		"{{META_DESCRIPTION}}": html.EscapeString(description),
		"{{META_KEYWORDS}}":    html.EscapeString(keywords),
		"{{OG_TITLE}}":         html.EscapeString(title),
		"{{OG_DESCRIPTION}}":   html.EscapeString(description),
		"{{OG_TYPE}}":          html.EscapeString(openGraphType),
		"{{OG_SITE_NAME}}":     html.EscapeString(siteName),
		"{{OG_URL}}":           html.EscapeString(canonical),
		"{{CANONICAL_URL}}":    html.EscapeString(canonical),
		"{{ROBOTS_CONTENT}}":   html.EscapeString(noIndex),
	}
	for placeholder, value := range values {
		htmlText = strings.ReplaceAll(htmlText, placeholder, value)
	}
	const marker = `<div id="app"></div>`
	htmlText = strings.Replace(htmlText, marker, `<div id="app">`+body+`</div>`, 1)
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, htmlText)
}

func replaceHTMLTitle(htmlText, title string) string {
	start := strings.Index(htmlText, "<title>")
	end := strings.Index(htmlText, "</title>")
	if start < 0 || end < start {
		return htmlText
	}
	return htmlText[:start] + "<title>" + html.EscapeString(title) + htmlText[end:]
}

func routeSlug(cleanPath, prefix string) (string, bool) {
	if !strings.HasPrefix(cleanPath, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(cleanPath, prefix)
	if rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	return rest, true
}

func isFrontendPath(rawPath string) bool {
	cleanPath := path.Clean(rawPath)
	return cleanPath == "/" || cleanPath == "/favicon.svg" ||
		cleanPath == "/admin" || strings.HasPrefix(cleanPath, "/admin/") ||
		cleanPath == "/user" || strings.HasPrefix(cleanPath, "/user/") ||
		cleanPath == "/page" || strings.HasPrefix(cleanPath, "/page/") ||
		cleanPath == "/setting" || strings.HasPrefix(cleanPath, "/setting/") ||
		strings.HasPrefix(cleanPath, "/assets/")
}

func isVersionedFrontendAsset(name string) bool {
	extension := path.Ext(name)
	if extension == "" {
		return false
	}
	stem := strings.TrimSuffix(name, extension)
	const viteHashLength = 8
	hashStart := len(stem) - viteHashLength
	if hashStart <= 0 || stem[hashStart-1] != '-' {
		return false
	}
	for _, char := range stem[hashStart:] {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}
