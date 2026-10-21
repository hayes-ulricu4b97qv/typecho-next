package httpapi

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

const (
	contentSecurityPolicy = "default-src 'self'; base-uri 'self'; object-src 'none'; " +
		"form-action 'self'; frame-ancestors 'none'; script-src 'self'; " +
		"style-src 'self' 'unsafe-inline'; img-src 'self' data: http: https:; connect-src 'self'"
	// maxJSONBody is large enough for one post or page document whose body
	// is a single rune past the documented maximum. A supplementary rune can
	// be represented by two JSON Unicode escapes (12 bytes); other limits use
	// the same conservative allowance.
	// The extra rune lets validation, rather than the transport limit,
	// reject an over-long body. The remaining allowance covers the other
	// documented fields and JSON framing.
	maxAuthJSONBody     = 8 << 10
	maxSmallJSONBody    = 128 << 10
	maxFieldRunes       = 200 + 120 + 500 + 20*60 + 20*60
	maxCustomFieldRunes = 20_000
	maxJSONBytesPerRune = 12
	maxJSONBody         = (service.MaxContentRunes+1)*maxJSONBytesPerRune + (maxFieldRunes+maxCustomFieldRunes)*maxJSONBytesPerRune + 16*1024
	maxUploadSize       = 10 << 20
	cookieName          = "app_session"
	csrfCookie          = "app_csrf"
	csrfHeader          = "X-CSRF-Token"
)

var scalarAPIQueryParameters = []string{
	"action", "category", "cursor", "includeDraft", "includePending", "kind", "month", "order", "page", "pageSize", "q", "resourceType", "status", "tag",
}

var scalarReaderQueryParameters = []string{"category", "month", "page", "preview", "q", "tag"}

type Server struct {
	repo              store.Repository
	app               *service.Service
	settings          config.Settings
	logger            *slog.Logger
	webDir            string
	mediaRoot         string
	mediaDeleteMu     sync.Mutex
	mediaUploadMu     sync.Mutex
	backupMu          sync.Mutex
	trustedProxies    []*net.IPNet
	loginFailures     *service.HitCounter
	commentHits       *service.HitCounter
	forgotHits        *service.HitCounter
	forgotAccounts    *service.HitCounter
	forgotFailureLogs *service.HitCounter
	failureLogLimits  *failureLogLimiter
}

func NewServer(repo store.Repository, settings config.Settings, logger *slog.Logger, mailers ...service.Mailer) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	webDir := os.Getenv("APP_WEB_DIR")
	if webDir == "" {
		webDir = "./web/dist"
	}
	mediaRoot := os.Getenv("APP_MEDIA_DIR")
	if mediaRoot == "" {
		dataDir := os.Getenv("APP_DATA_DIR")
		if dataDir == "" {
			dataDir = "./data"
		}
		mediaRoot = filepath.Join(dataDir, "uploads")
	}
	var mailer service.Mailer
	if len(mailers) > 0 {
		mailer = mailers[0]
	}
	return &Server{
		repo:              repo,
		app:               service.New(repo, logger, mailer),
		settings:          settings,
		logger:            logger,
		webDir:            webDir,
		mediaRoot:         mediaRoot,
		trustedProxies:    parseTrustedProxyCIDRs(settings.TrustedProxyCIDRs, logger),
		loginFailures:     service.NewHitCounter(),
		commentHits:       service.NewHitCounter(),
		forgotHits:        service.NewHitCounter(),
		forgotAccounts:    service.NewHitCounter(),
		forgotFailureLogs: service.NewHitCounter(),
		failureLogLimits:  newFailureLogLimiter(),
	}
}

// WaitForBackgroundWork drains asynchronous application work during shutdown.
func (s *Server) WaitForBackgroundWork(ctx context.Context) error {
	return s.app.WaitForBackgroundWork(ctx)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /feed.xml", s.feed)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.HandleFunc("GET /api/v1/system/status", s.systemStatus)
	mux.HandleFunc("POST /api/v1/setup", s.setup)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/forgot", s.forgotPassword)
	mux.HandleFunc("POST /api/v1/auth/reset", s.resetPassword)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/auth/me", s.me)
	mux.HandleFunc("GET /api/v1/content/posts", s.listPosts)
	mux.HandleFunc("GET /api/v1/content/archive", s.listArchive)
	mux.HandleFunc("GET /api/v1/content/comments/recent", s.listRecentComments)
	mux.HandleFunc("GET /api/v1/content/pages", s.listPages)
	mux.HandleFunc("POST /api/v1/content/pages", s.createPage)
	mux.HandleFunc("GET /api/v1/content/pages/{slug}", s.getPage)
	mux.HandleFunc("PUT /api/v1/content/pages/{id}", s.updatePage)
	mux.HandleFunc("DELETE /api/v1/content/pages/{id}", s.deletePage)
	mux.HandleFunc("GET /api/v1/content/terms", s.listTerms)
	mux.HandleFunc("POST /api/v1/content/terms", s.createTerm)
	mux.HandleFunc("PUT /api/v1/content/terms/{id}", s.updateTerm)
	mux.HandleFunc("DELETE /api/v1/content/terms/{id}", s.deleteTerm)
	mux.HandleFunc("GET /api/v1/content/posts/{id}/revisions", s.listPostRevisions)
	mux.HandleFunc("POST /api/v1/content/posts/{id}/revisions/{revisionID}/restore", s.restorePostRevision)
	mux.HandleFunc("GET /api/v1/content/posts/{slug}", s.getPost)
	mux.HandleFunc("GET /api/v1/content/posts/{slug}/comments", s.listComments)
	mux.HandleFunc("POST /api/v1/content/posts/{slug}/comments", s.createComment)
	mux.HandleFunc("GET /api/v1/content/comments", s.listAllComments)
	mux.HandleFunc("PUT /api/v1/content/comments/{id}", s.updateComment)
	mux.HandleFunc("DELETE /api/v1/content/comments/{id}", s.deleteComment)
	mux.HandleFunc("GET /api/v1/media", s.listMedia)
	mux.HandleFunc("POST /api/v1/media", s.uploadMedia)
	mux.HandleFunc("DELETE /api/v1/media/{id}", s.deleteMedia)
	mux.HandleFunc("GET /media/{id}", s.serveMedia)
	mux.HandleFunc("POST /api/v1/content/posts", s.createPost)
	mux.HandleFunc("PUT /api/v1/content/posts/{id}", s.updatePost)
	mux.HandleFunc("DELETE /api/v1/content/posts/{id}", s.deletePost)
	mux.HandleFunc("GET /api/v1/dashboard/stats", s.dashboardStats)
	mux.HandleFunc("GET /api/v1/settings/site", s.getSite)
	mux.HandleFunc("PUT /api/v1/settings/site", s.updateSite)
	mux.HandleFunc("GET /api/v1/settings/reading", s.getReading)
	mux.HandleFunc("PUT /api/v1/settings/reading", s.updateReading)
	mux.HandleFunc("PUT /api/v1/settings/account", s.updateAccount)
	mux.HandleFunc("GET /api/v1/settings/users", s.listUsers)
	mux.HandleFunc("POST /api/v1/settings/users", s.createUser)
	mux.HandleFunc("PUT /api/v1/settings/users/{username}", s.updateUser)
	mux.HandleFunc("DELETE /api/v1/settings/users/{username}", s.deleteUser)
	mux.HandleFunc("GET /api/v1/audit", s.listAudit)
	mux.HandleFunc("GET /api/v1/mail", s.listOutboundMail)
	mux.HandleFunc("GET /api/v1/backup", s.backup)
	mux.HandleFunc("GET /", s.frontend)

	return requestLog(apiErrorEnvelope(s.routeGuard(mux)), s.logger)
}

func (s *Server) writeRepositoryUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	s.logErrorRateLimited(r.Context(), "repository-unavailable", "repository unavailable", "error", err)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "The content store is unavailable")
		return
	}
	http.Error(w, "The content store is unavailable", http.StatusServiceUnavailable)
}

func (s *Server) logErrorRateLimited(ctx context.Context, key string, message string, args ...any) {
	s.logFailureRateLimited(ctx, key, slog.LevelError, message, args...)
}

func (s *Server) logWarnRateLimited(ctx context.Context, key string, message string, args ...any) {
	s.logFailureRateLimited(ctx, key, slog.LevelWarn, message, args...)
}

func (s *Server) logFailureRateLimited(ctx context.Context, key string, level slog.Level, message string, args ...any) {
	suppressed, allowed := s.failureLogLimits.allow(key, time.Minute)
	if !allowed {
		return
	}
	if requestID := requestIDFromContext(ctx); requestID != "" {
		args = append([]any{"request_id", requestID}, args...)
	}
	if suppressed > 0 {
		args = append(args, "suppressed", suppressed)
	}
	s.logger.Log(context.Background(), level, message, args...)
}

func (s *Server) routeGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if isVersionedAPIPath(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-store")
			values, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "API 查询参数格式无效")
				return
			}
			for _, key := range scalarAPIQueryParameters {
				if len(values[key]) > 1 {
					writeError(w, http.StatusUnprocessableEntity, "validation_error", fmt.Sprintf("查询参数 %s 只能指定一次", key))
					return
				}
			}
			for _, key := range []string{"includeDraft", "includePending"} {
				if value, exists := values[key]; exists && value[0] != "true" && value[0] != "false" {
					writeError(w, http.StatusUnprocessableEntity, "validation_error", fmt.Sprintf("查询参数 %s 必须为 true 或 false", key))
					return
				}
			}
		}
		cleanReaderPath := path.Clean(r.URL.Path)
		isReaderContentPath := cleanReaderPath == "/user" || strings.HasPrefix(cleanReaderPath, "/user/") ||
			cleanReaderPath == "/page" || strings.HasPrefix(cleanReaderPath, "/page/")
		if isReaderContentPath {
			values, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				http.Error(w, "请求参数格式无效", http.StatusBadRequest)
				return
			}
			keys := []string{"preview"}
			if cleanReaderPath == "/user" {
				keys = scalarReaderQueryParameters
			}
			for _, key := range keys {
				if len(values[key]) > 1 {
					http.Error(w, fmt.Sprintf("查询参数 %s 只能指定一次", key), http.StatusBadRequest)
					return
				}
			}
		}
		if isFrontendPath(r.URL.Path) && r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "This page only supports GET and HEAD")
			return
		}
		if shouldSetCSRFCookie(r.URL.Path) {
			s.ensureCSRFCookie(w, r)
		}
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/feed.xml" || r.URL.Path == "/sitemap.xml" || r.URL.Path == "/robots.txt" || r.URL.Path == "/api/v1/system/status" || r.URL.Path == "/api/v1/setup" || r.URL.Path == "/api/v1/auth/login" || strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/favicon.svg" {
			next.ServeHTTP(w, r)
			return
		}
		initialized, err := s.repo.InitializedContext(r.Context())
		if r.Context().Err() != nil {
			return
		}
		if r.URL.Path == "/setting" || strings.HasPrefix(r.URL.Path, "/setting/") {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			s.writeRepositoryUnavailable(w, r, err)
			return
		}
		if !initialized {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusPreconditionRequired, "setup_required", "Complete the initial setup at /setting")
				return
			}
			http.Redirect(w, r, "/setting", http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			http.Redirect(w, r, "/user", http.StatusTemporaryRedirect)
			return
		}
		if isUnsafeMethod(r.Method) && !s.validCSRF(r) {
			writeError(w, http.StatusForbidden, "csrf_failed", "A valid CSRF token is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func shouldSetCSRFCookie(path string) bool {
	return path != "/favicon.svg" && !strings.HasPrefix(path, "/assets/") && !strings.HasPrefix(path, "/media/")
}

func (s *Server) ensureCSRFCookie(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(csrfCookie); err == nil && cookie.Value != "" {
		return
	}
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: hex.EncodeToString(buffer), Path: "/", HttpOnly: false, SameSite: http.SameSiteLaxMode, Secure: s.settings.CookieSecure})
}

func (s *Server) validCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	header := r.Header.Get(csrfHeader)
	if header == "" || len(header) != len(cookie.Value) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) == 1
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.settings.Version})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.repo.InitializedContext(r.Context())
	if r.Context().Err() != nil {
		return
	}
	if err != nil || !initialized {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "Complete the initial setup before serving traffic")
		return
	}
	if _, err := s.repo.SiteContext(r.Context()); err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusServiceUnavailable, "not_ready", "The content store is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "version": s.settings.Version})
}

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	GUID        rssGUID `xml:"guid"`
	Description string  `xml:"description"`
	PubDate     string  `xml:"pubDate"`
}

type rssGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink bool   `xml:"isPermaLink,attr"`
}

func (s *Server) feed(w http.ResponseWriter, r *http.Request) {
	site, err := s.app.SiteContext(r.Context())
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) || site == nil {
		writeError(w, http.StatusNotFound, "not_found", "Site settings not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read site settings")
		return
	}
	baseURL := strings.TrimRight(site.URL, "/")
	editor, err := s.contentEditorAccess(r.Context(), r)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		s.writeRepositoryUnavailable(w, r, err)
		return
	}
	passphrase := r.Header.Get("X-Post-Passphrase")
	posts, err := s.app.ListPostsForFeed(r.Context(), 1, 100)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read published posts")
		return
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, xml.Header); err != nil {
		s.logger.Debug("stop RSS response", "error", err)
		return
	}
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	root := xml.StartElement{
		Name: xml.Name{Local: "rss"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "version"}, Value: "2.0"}},
	}
	channel := xml.StartElement{Name: xml.Name{Local: "channel"}}
	if err := encoder.EncodeToken(root); err != nil {
		s.logger.Debug("stop RSS response", "error", err)
		return
	}
	if err := encoder.EncodeToken(channel); err != nil {
		s.logger.Debug("stop RSS response", "error", err)
		return
	}
	channelFields := []struct {
		name  string
		value string
	}{
		{name: "title", value: site.Name},
		{name: "link", value: baseURL},
		{name: "description", value: feedDescription(site)},
		{name: "lastBuildDate", value: time.Now().UTC().Format(time.RFC1123Z)},
	}
	for _, field := range channelFields {
		if err := encodeRSSTextElement(encoder, field.name, field.value); err != nil {
			s.logger.Debug("stop RSS response", "error", err)
			return
		}
	}
	if err := encoder.Flush(); err != nil {
		s.logger.Debug("stop RSS response", "error", err)
		return
	}

	for page := 1; ; page++ {
		if err := r.Context().Err(); err != nil {
			s.logger.Debug("stop RSS response", "error", err)
			return
		}
		if page > 1 {
			posts, err = s.app.ListPostsForFeed(r.Context(), page, 100)
			if err != nil {
				if r.Context().Err() != nil {
					s.logger.Debug("stop RSS response", "error", err)
					return
				}
				s.logErrorRateLimited(r.Context(), "rss-read-posts", "read published posts while streaming RSS", "page", page, "error", err)
				return
			}
		}
		for _, post := range posts.Items {
			if err := r.Context().Err(); err != nil {
				s.logger.Debug("stop RSS response", "error", err)
				return
			}
			publishedAt := post.PublishedAt
			if publishedAt.IsZero() {
				publishedAt = post.UpdatedAt
			}
			permalink := baseURL + "/user/" + url.PathEscape(post.Slug)
			content := ""
			if s.app.RevealPost(post, editor, passphrase) {
				content = post.Content
			}
			item := rssItem{
				Title:       sanitizeXMLText(post.Title),
				Link:        permalink,
				GUID:        rssGUID{Value: permalink, IsPermaLink: true},
				Description: sanitizeXMLText(service.FeedBlurb(post.Excerpt, content)),
				PubDate:     publishedAt.UTC().Format(time.RFC1123Z),
			}
			if err := encoder.EncodeElement(item, xml.StartElement{Name: xml.Name{Local: "item"}}); err != nil {
				s.logger.Debug("stop RSS response", "error", err)
				return
			}
			if err := encoder.Flush(); err != nil {
				s.logger.Debug("stop RSS response", "error", err)
				return
			}
		}
		if len(posts.Items) == 0 || posts.PageSize <= 0 || len(posts.Items) < posts.PageSize {
			break
		}
	}
	if err := encoder.EncodeToken(channel.End()); err != nil {
		s.logger.Debug("stop RSS response", "error", err)
		return
	}
	if err := encoder.EncodeToken(root.End()); err != nil {
		s.logger.Debug("stop RSS response", "error", err)
		return
	}
	if err := encoder.Flush(); err != nil {
		s.logger.Debug("stop RSS response", "error", err)
	}
}

func encodeRSSTextElement(encoder *xml.Encoder, name, value string) error {
	return encoder.EncodeElement(sanitizeXMLText(value), xml.StartElement{Name: xml.Name{Local: name}})
}

func sanitizeXMLText(value string) string {
	return strings.Map(func(char rune) rune {
		if char == '\t' || char == '\n' || char == '\r' ||
			(char >= 0x20 && char <= 0xD7FF) ||
			(char >= 0xE000 && char <= 0xFFFD) ||
			(char >= 0x10000 && char <= 0x10FFFF) {
			return char
		}
		return '\uFFFD'
	}, value)
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	site, err := s.app.SiteContext(r.Context())
	if r.Context().Err() != nil {
		return
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read site settings")
		return
	}
	lines := []string{"User-agent: *", "Allow: /", "Disallow: /admin", "Disallow: /setting"}
	if site != nil && strings.TrimSpace(site.URL) != "" {
		lines = append(lines, "Sitemap: "+strings.TrimRight(site.URL, "/")+"/sitemap.xml")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, strings.Join(lines, "\n")+"\n")
}

func (s *Server) systemStatus(w http.ResponseWriter, r *http.Request) {
	site, err := s.app.SiteContext(r.Context())
	if err != nil && r.Context().Err() != nil {
		return
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.writeRepositoryUnavailable(w, r, err)
		return
	}
	initialized, err := s.repo.InitializedContext(r.Context())
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		s.writeRepositoryUnavailable(w, r, err)
		return
	}
	response := map[string]any{
		"initialized": initialized,
		"version":     s.settings.Version,
		"features":    []string{"content", "pages", "taxonomy", "comments", "discussion", "media", "users", "sessions", "settings", "feeds", "archive", "audit"},
	}
	if site != nil {
		response["site"] = publicSiteSummary(site)
		response["reading"] = publicReading(site)
	}
	writeJSON(w, http.StatusOK, response)
}

type postRequest struct {
	Title             string             `json:"title"`
	Slug              string             `json:"slug"`
	Excerpt           string             `json:"excerpt"`
	Content           string             `json:"content"`
	Status            string             `json:"status"`
	AllowComment      *bool              `json:"allowComment"`
	Categories        []string           `json:"categories"`
	Tags              []string           `json:"tags"`
	PublishAt         *string            `json:"publishAt"`
	Passphrase        *string            `json:"passphrase"`
	CustomFields      *map[string]string `json:"customFields"`
	ExpectedUpdatedAt *time.Time         `json:"expectedUpdatedAt"`
}

func (input postRequest) post() (store.Post, error) {
	post := store.Post{
		Title:      input.Title,
		Slug:       input.Slug,
		Excerpt:    input.Excerpt,
		Content:    input.Content,
		Status:     input.Status,
		Categories: termInputs(input.Categories, "category"),
		Tags:       termInputs(input.Tags, "tag"),
	}
	if input.CustomFields != nil {
		post.CustomFields = *input.CustomFields
		post.SetCustomFields = true
	}
	if input.PublishAt != nil && strings.TrimSpace(*input.PublishAt) != "" {
		raw := strings.TrimSpace(*input.PublishAt)
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, raw)
		}
		if err != nil {
			return store.Post{}, fmt.Errorf("%w: 发布时间格式无效", service.ErrValidation)
		}
		post.RequestedPublishAt = parsed.UTC()
	}
	if input.Passphrase != nil {
		post.SetPassphrase = true
		post.PassphrasePlain = *input.Passphrase
	}
	return post, nil
}

func applyAllowComment(post store.Post, input postRequest, fallback bool) store.Post {
	post.AllowComment = fallback
	if input.AllowComment != nil {
		post.AllowComment = *input.AllowComment
	}
	return post
}

func termInputs(names []string, kind string) []store.Term {
	terms := make([]store.Term, 0, len(names))
	for _, name := range names {
		terms = append(terms, store.Term{Name: name, Kind: kind})
	}
	return terms
}

type pageRequest struct {
	Title             string     `json:"title"`
	Slug              string     `json:"slug"`
	Excerpt           string     `json:"excerpt"`
	Content           string     `json:"content"`
	Status            string     `json:"status"`
	ExpectedUpdatedAt *time.Time `json:"expectedUpdatedAt"`
}

func (input pageRequest) page() store.Page {
	return store.Page{Title: input.Title, Slug: input.Slug, Excerpt: input.Excerpt, Content: input.Content, Status: input.Status}
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	return s.requireRole(w, r, "owner", "editor", "moderator")
}

func (s *Server) requireOwner(w http.ResponseWriter, r *http.Request) bool {
	return s.requireRoleWithMessage(w, r, "Owner permission is required", "owner")
}

func (s *Server) requireContentEditor(w http.ResponseWriter, r *http.Request) bool {
	return s.requireRole(w, r, "owner", "editor")
}

func (s *Server) requireModerator(w http.ResponseWriter, r *http.Request) bool {
	return s.requireRole(w, r, "owner", "moderator")
}

func (s *Server) requireRole(w http.ResponseWriter, r *http.Request, roles ...string) bool {
	return s.requireRoleWithMessage(w, r, "You do not have permission for this action", roles...)
}

func (s *Server) requireRoleWithMessage(w http.ResponseWriter, r *http.Request, deniedMessage string, roles ...string) bool {
	user, err := s.authenticatedAdmin(r.Context(), r)
	if r.Context().Err() != nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue")
		return false
	}
	if err != nil {
		s.writeRepositoryUnavailable(w, r, err)
		return false
	}
	if r.Context().Err() != nil {
		return false
	}
	for _, role := range roles {
		if user.Role == role {
			return true
		}
	}
	writeError(w, http.StatusForbidden, "forbidden", deniedMessage)
	return false
}

func (s *Server) contentEditorAccess(ctx context.Context, r *http.Request) (bool, error) {
	user, err := s.authenticatedAdmin(ctx, r)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return user.Role == "owner" || user.Role == "editor", nil
}

func (s *Server) currentAdmin(r *http.Request) (*store.Admin, bool) {
	return s.currentAdminContext(r.Context(), r)
}

func (s *Server) currentAdminContext(ctx context.Context, r *http.Request) (*store.Admin, bool) {
	admin, err := s.authenticatedAdmin(ctx, r)
	return admin, err == nil
}

func (s *Server) authenticatedAdmin(ctx context.Context, r *http.Request) (*store.Admin, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return nil, store.ErrNotFound
	}
	username, err := s.app.ResolveSessionContext(ctx, cookie.Value, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	admin, err := s.app.UserContext(ctx, username)
	if err != nil {
		return nil, err
	}
	if admin == nil || admin.Username != username || !admin.Active {
		return nil, store.ErrNotFound
	}
	return admin, nil
}

func sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (s *Server) recordAudit(r *http.Request, action, resourceType, resourceID string, details map[string]string) {
	event := store.AuditEvent{
		ActorUsername: "anonymous",
		Action:        action,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		Details:       details,
	}
	user, ok := s.currentAdmin(r)
	if !ok && r.Context().Err() != nil {
		// The mutation already succeeded; keep its audit actor accurate even
		// when a disconnected client canceled the request context.
		user, ok = s.currentAdminContext(context.Background(), r)
	}
	if ok {
		event.ActorUserID = user.ID
		event.ActorUsername = user.Username
	}
	if err := s.app.RecordAudit(event); err != nil {
		s.logWarnRateLimited(r.Context(), "audit-write", "record audit event failed", "action", action, "resource_type", resourceType, "resource_id", resourceID, "error", err)
	}
}

func (s *Server) issueSessionContext(ctx context.Context, w http.ResponseWriter, username string) error {
	return s.issueSessionWithContext(ctx, w, username, "")
}

func (s *Server) issueAuthenticatedSessionContext(ctx context.Context, w http.ResponseWriter, username, passwordHash string) error {
	return s.issueSessionWithContext(ctx, w, username, passwordHash)
}

func (s *Server) issueSessionWithContext(ctx context.Context, w http.ResponseWriter, username, passwordHash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	expires := time.Now().Add(s.sessionTTL())
	if err := ctx.Err(); err != nil {
		return err
	}
	var createErr error
	if passwordHash == "" {
		createErr = s.app.CreateSessionContext(ctx, username, token, expires)
	} else {
		createErr = s.app.CreateAuthenticatedSessionContext(ctx, username, passwordHash, token, expires)
	}
	if createErr != nil {
		return createErr
	}
	revokeUnissuedSession := func() {
		if err := s.app.RevokeSession(token); err != nil {
			s.logWarnRateLimited(ctx, "session-revoke-after-cancel", "revoke sign-in session after canceled request failed", "error", err)
		}
	}
	if err := ctx.Err(); err != nil {
		revokeUnissuedSession()
		return err
	}
	_ = s.app.PurgeSessionsContext(ctx, time.Now().UTC())
	if err := ctx.Err(); err != nil {
		revokeUnissuedSession()
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", Expires: expires, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.settings.CookieSecure})
	return nil
}

func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	cleanPath := path.Clean(r.URL.Path)
	if cleanPath == "/" {
		initialized, err := s.repo.InitializedContext(r.Context())
		if r.Context().Err() != nil {
			return
		}
		if err == nil && initialized {
			http.Redirect(w, r, "/user", http.StatusTemporaryRedirect)
		} else {
			http.Redirect(w, r, "/setting", http.StatusTemporaryRedirect)
		}
		return
	}
	if strings.HasPrefix(cleanPath, "/assets/") || cleanPath == "/favicon.svg" {
		if strings.Contains(cleanPath, `\`) {
			http.NotFound(w, r)
			return
		}
		file := path.Join(s.webDir, cleanPath)
		if _, err := os.Stat(file); err != nil {
			http.NotFound(w, r)
			return
		}
		if cleanPath != "/favicon.svg" && isVersionedFrontendAsset(path.Base(cleanPath)) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, file)
		return
	}
	if cleanPath == "/admin" || strings.HasPrefix(cleanPath, "/admin/") || cleanPath == "/user" || strings.HasPrefix(cleanPath, "/user/") || cleanPath == "/page" || strings.HasPrefix(cleanPath, "/page/") || cleanPath == "/setting" || strings.HasPrefix(cleanPath, "/setting/") {
		s.serveAppShell(w, r, cleanPath)
		return
	}
	http.NotFound(w, r)
}

func serviceError(err error) (int, string) {
	if errors.Is(err, store.ErrStaleWrite) {
		return http.StatusConflict, "conflict"
	}
	if errors.Is(err, service.ErrValidation) {
		return http.StatusUnprocessableEntity, "validation_error"
	}
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusNotFound, "not_found"
	}
	if errors.Is(err, store.ErrConflict) {
		return http.StatusConflict, "conflict"
	}
	return http.StatusInternalServerError, "save_failed"
}

func serviceReadError(err error) (int, string) {
	if errors.Is(err, service.ErrValidation) {
		return http.StatusUnprocessableEntity, "validation_error"
	}
	return http.StatusInternalServerError, "read_failed"
}

func (s *Server) readingPage(r *http.Request) (int, int, error) {
	page, pageSize, err := pagination(r)
	if err != nil {
		return 0, 0, err
	}
	if strings.TrimSpace(r.URL.Query().Get("pageSize")) != "" {
		return page, pageSize, nil
	}
	site, err := s.app.SiteContext(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return page, pageSize, nil
		}
		return 0, 0, err
	}
	if site == nil || site.PostsPerPage < 1 || site.PostsPerPage > 50 {
		return page, pageSize, nil
	}
	return page, site.PostsPerPage, nil
}

func (s *Server) pages(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page, pageSize, err := pagination(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return 0, 0, false
	}
	return page, pageSize, true
}

func pagination(r *http.Request) (int, int, error) {
	page := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return 0, 0, fmt.Errorf("%w: page 必须是正整数", service.ErrValidation)
		}
		page = parsed
	}
	pageSize := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("pageSize")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return 0, 0, fmt.Errorf("%w: pageSize 必须是 1 到 100 的整数", service.ErrValidation)
		}
		pageSize = parsed
	}
	maxInt := int(^uint(0) >> 1)
	if page > 1 && page-1 > maxInt/pageSize {
		return 0, 0, fmt.Errorf("%w: page 与 pageSize 组合后的偏移量超出范围", service.ErrValidation)
	}
	return page, pageSize, nil
}

func feedDescription(site *store.Site) string {
	if site != nil {
		if description := strings.TrimSpace(site.Description); description != "" {
			return description
		}
		return site.Name + " latest posts"
	}
	return "latest posts"
}

func publicSite(site *store.Site) map[string]string {
	return map[string]string{
		"name":        site.Name,
		"url":         site.URL,
		"adminMail":   site.AdminMail,
		"timezone":    site.Timezone,
		"description": site.Description,
		"keywords":    site.Keywords,
	}
}

func publicSiteSummary(site *store.Site) map[string]string {
	return map[string]string{
		"name":        site.Name,
		"url":         site.URL,
		"timezone":    site.Timezone,
		"description": site.Description,
		"keywords":    site.Keywords,
	}
}

func publicReading(site *store.Site) map[string]any {
	postsPerPage := site.PostsPerPage
	if postsPerPage < 1 || postsPerPage > 50 {
		postsPerPage = 10
	}
	return map[string]any{
		"postsPerPage":              postsPerPage,
		"commentsEnabled":           site.CommentsEnabled,
		"commentsRequireModeration": site.CommentsRequireModeration,
		"commentsThreaded":          site.CommentsThreaded,
	}
}

func publicUser(user *store.User) map[string]string {
	return map[string]string{"username": user.Username, "email": user.Email, "role": user.Role}
}

type userResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
}

func publicUserRecord(user store.User) userResponse {
	return userResponse{ID: user.ID, Username: user.Username, Email: user.Email, Role: user.Role, Active: user.Active, CreatedAt: user.CreatedAt}
}

type postResponse struct {
	ID           string                `json:"id"`
	Title        string                `json:"title"`
	Slug         string                `json:"slug"`
	Excerpt      string                `json:"excerpt"`
	Content      *string               `json:"content,omitempty"`
	CustomFields map[string]string     `json:"customFields,omitempty"`
	Locked       bool                  `json:"locked,omitempty"`
	Status       string                `json:"status"`
	AllowComment bool                  `json:"allowComment"`
	Categories   []termResponse        `json:"categories,omitempty"`
	Tags         []termResponse        `json:"tags,omitempty"`
	PublishedAt  *time.Time            `json:"publishedAt,omitempty"`
	UpdatedAt    time.Time             `json:"updatedAt"`
	Previous     *postNeighborResponse `json:"previous,omitempty"`
	Next         *postNeighborResponse `json:"next,omitempty"`
}

type postNeighborResponse struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

type termResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Kind      string `json:"kind"`
	PostCount int    `json:"postCount"`
}

type termPageResponse struct {
	Items    []termResponse `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

type postPageResponse struct {
	Items    []postResponse `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

type pageResponse struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Excerpt     string     `json:"excerpt"`
	Content     string     `json:"content"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type pagePageResponse struct {
	Items    []pageResponse `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

func publicPostWithNeighbors(post store.Post, previous, next *store.Post, withBody bool) postResponse {
	response := publicPost(post, withBody)
	response.Previous = publicNeighbor(previous)
	response.Next = publicNeighbor(next)
	return response
}

func publicNeighbor(post *store.Post) *postNeighborResponse {
	if post == nil {
		return nil
	}
	response := &postNeighborResponse{ID: post.ID, Title: post.Title, Slug: post.Slug}
	if !post.PublishedAt.IsZero() {
		publishedAt := post.PublishedAt
		response.PublishedAt = &publishedAt
	}
	return response
}

func publicPost(post store.Post, withBody bool) postResponse {
	response := postResponse{
		ID:           post.ID,
		Title:        post.Title,
		Slug:         post.Slug,
		Excerpt:      post.Excerpt,
		Status:       post.Status,
		AllowComment: post.AllowComment,
		Locked:       post.PassphraseHash != "",
		UpdatedAt:    post.UpdatedAt,
	}
	if withBody {
		content := post.Content
		response.Content = &content
		response.CustomFields = post.CustomFields
	}
	response.Categories = publicTerms(post.Categories)
	response.Tags = publicTerms(post.Tags)
	if !post.PublishedAt.IsZero() {
		publishedAt := post.PublishedAt
		response.PublishedAt = &publishedAt
	}
	return response
}

func publicTerms(terms []store.Term) []termResponse {
	items := make([]termResponse, 0, len(terms))
	for _, term := range terms {
		items = append(items, termResponse{ID: term.ID, Name: term.Name, Slug: term.Slug, Kind: term.Kind, PostCount: term.PostCount})
	}
	return items
}

func publicTerm(term store.Term) termResponse {
	return termResponse{ID: term.ID, Name: term.Name, Slug: term.Slug, Kind: term.Kind, PostCount: term.PostCount}
}

func publicTermPage(page store.TermPage) termPageResponse {
	return termPageResponse{Items: publicTerms(page.Items), Total: page.Total, Page: page.Page, PageSize: page.PageSize}
}

func publicPostPage(page store.PostPage) postPageResponse {
	items := make([]postResponse, 0, len(page.Items))
	for _, post := range page.Items {
		items = append(items, publicPost(post, false))
	}
	return postPageResponse{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize}
}

func publicPage(page store.Page) pageResponse {
	response := pageResponse{ID: page.ID, Title: page.Title, Slug: page.Slug, Excerpt: page.Excerpt, Content: page.Content, Status: page.Status, UpdatedAt: page.UpdatedAt}
	if !page.PublishedAt.IsZero() {
		publishedAt := page.PublishedAt
		response.PublishedAt = &publishedAt
	}
	return response
}

func publicPagePage(page store.PagePage) pagePageResponse {
	items := make([]pageResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, publicPage(item))
	}
	return pagePageResponse{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize}
}

type commentResponse struct {
	ID          string    `json:"id"`
	PostID      string    `json:"postId,omitempty"`
	ParentID    string    `json:"parentId,omitempty"`
	PostTitle   string    `json:"postTitle,omitempty"`
	PostSlug    string    `json:"postSlug,omitempty"`
	AuthorName  string    `json:"authorName"`
	AuthorEmail string    `json:"authorEmail,omitempty"`
	AuthorURL   string    `json:"authorUrl,omitempty"`
	Content     string    `json:"content"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type commentPageResponse struct {
	Items      []commentResponse `json:"items"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"pageSize"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

func publicComment(comment store.Comment, includePrivateFields bool) commentResponse {
	response := commentResponse{
		ID:         comment.ID,
		AuthorName: comment.AuthorName,
		ParentID:   comment.ParentID,
		PostTitle:  comment.PostTitle,
		PostSlug:   comment.PostSlug,
		AuthorURL:  comment.AuthorURL,
		Content:    comment.Content,
		Status:     comment.Status,
		CreatedAt:  comment.CreatedAt,
		UpdatedAt:  comment.UpdatedAt,
	}
	if includePrivateFields {
		response.PostID = comment.PostID
		response.AuthorEmail = comment.AuthorEmail
	}
	return response
}

func publicCommentPage(page store.CommentPage, includePrivateFields bool) commentPageResponse {
	items := make([]commentResponse, 0, len(page.Items))
	for _, comment := range page.Items {
		items = append(items, publicComment(comment, includePrivateFields))
	}
	return commentPageResponse{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize}
}

type mediaResponse struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"createdAt"`
}

type mediaPageResponse struct {
	Items    []mediaResponse `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

func publicMedia(media store.Media) mediaResponse {
	return mediaResponse{
		ID:          media.ID,
		Filename:    media.Filename,
		ContentType: media.ContentType,
		Size:        media.Size,
		URL:         "/media/" + media.ID,
		CreatedAt:   media.CreatedAt,
	}
}

func publicMediaPage(page store.MediaPage) mediaPageResponse {
	items := make([]mediaResponse, 0, len(page.Items))
	for _, media := range page.Items {
		items = append(items, publicMedia(media))
	}
	return mediaPageResponse{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize}
}

func cleanMediaFilename(raw string) string {
	filename := filepath.Base(strings.TrimSpace(raw))
	if filename == "." || filename == string(filepath.Separator) || filename == "" || strings.ContainsRune(filename, 0) {
		return ""
	}
	runes := []rune(filename)
	if len(runes) > 180 {
		filename = string(runes[:180])
	}
	return filename
}

func allowedMediaType(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func mediaExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}

func newMediaID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("media-%d", time.Now().UnixNano())
	}
	return "media-" + hex.EncodeToString(buffer)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	return decodeJSONWithLimit(w, r, destination, maxSmallJSONBody)
}

func decodeAuthJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	return decodeJSONWithLimit(w, r, destination, maxAuthJSONBody)
}

func decodeContentJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	return decodeJSONWithLimit(w, r, destination, maxJSONBody)
}

func decodeJSONWithLimit(w http.ResponseWriter, r *http.Request, destination any, limit int) error {
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return errors.New("Content-Type must be application/json")
	}
	mediaType, parameters, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	for name := range parameters {
		if !strings.EqualFold(name, "charset") {
			return errors.New("Content-Type must be application/json")
		}
	}
	if charset, exists := parameters["charset"]; exists && !strings.EqualFold(charset, "utf-8") {
		return errors.New("JSON Content-Type charset must be UTF-8")
	}
	reader := bufio.NewReader(http.MaxBytesReader(w, r.Body, int64(limit)))
	for {
		first, err := reader.ReadByte()
		if err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
		if first == ' ' || first == '\t' || first == '\r' || first == '\n' {
			continue
		}
		if first != '{' {
			return errors.New("invalid JSON: request body must be an object")
		}
		if err := reader.UnreadByte(); err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
		break
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("invalid JSON: request body must contain a single value")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func writeJSONDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds the supported size limit")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeXML(w http.ResponseWriter, status int, contentType string, value any) {
	payload, err := xml.MarshalIndent(value, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode_failed", "Unable to encode response")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, xml.Header)
	_, _ = w.Write(payload)
	_, _ = io.WriteString(w, "\n")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": localizedError(code, message)}})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

var requestIDSequence atomic.Uint64

type requestIDContextKey struct{}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusWriter) FlushError() error {
	flushError, supportsFlushError := w.ResponseWriter.(interface{ FlushError() error })
	flusher, supportsFlush := w.ResponseWriter.(http.Flusher)
	if !supportsFlushError && !supportsFlush {
		return http.ErrNotSupported
	}
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if supportsFlushError {
		return flushError.FlushError()
	}
	flusher.Flush()
	return nil
}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(payload []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(payload)
}

func requestLog(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID))
		started := time.Now()
		recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		logRequest := func(level slog.Level, message string, status int, panicked bool) {
			args := []any{"request_id", requestID, "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds()}
			if panicked {
				args = append(args, "panicked", true, "stack", string(debug.Stack()))
			}
			logger.Log(context.Background(), level, message, args...)
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				status := recorder.status
				if !recorder.wrote {
					status = http.StatusInternalServerError
				}
				logRequest(slog.LevelError, "http request panic", status, true)
				// ErrAbortHandler prevents net/http from logging the original panic value.
				panic(http.ErrAbortHandler)
			}
		}()

		next.ServeHTTP(recorder, r)
		status := recorder.status
		if !recorder.wrote && r.Context().Err() != nil {
			// 499 is a log-only marker for a request canceled before a response
			// was written; it is never sent as an HTTP response status.
			status = 499
		}
		if status >= http.StatusInternalServerError {
			logRequest(slog.LevelError, "http request", status, false)
			return
		}
		logRequest(slog.LevelInfo, "http request", status, false)
	})
}

func newRequestID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err == nil {
		return hex.EncodeToString(id[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 16) + "-" + strconv.FormatUint(requestIDSequence.Add(1), 16)
}
