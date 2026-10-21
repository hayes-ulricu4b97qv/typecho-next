package httpapi

import (
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

const defaultFrontendPostsPerPage = 10

func (s *Server) renderServerPostList(r *http.Request, site *store.Site) string {
	pageSize := defaultFrontendPostsPerPage
	if site.PostsPerPage >= 1 && site.PostsPerPage <= 50 {
		pageSize = site.PostsPerPage
	}
	page := frontendPage(r.URL.Query().Get("page"), pageSize)
	search := r.URL.Query().Get("q")
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	posts, err := s.app.ListPostsByTermsContext(r.Context(), search, category, tag, month, "", false, "", page, pageSize)
	if err != nil {
		return `<main class="public-content"><h1>` + html.EscapeString(site.Name) + `</h1><div class="empty-state empty-error">文章加载失败，请检查筛选条件后重试。</div></main>`
	}
	if posts.Total > 0 && page > frontendTotalPages(posts.Total, pageSize) {
		page = frontendTotalPages(posts.Total, pageSize)
		posts, err = s.app.ListPostsByTermsContext(r.Context(), search, category, tag, month, "", false, "", page, pageSize)
		if err != nil {
			return `<main class="public-content"><h1>` + html.EscapeString(site.Name) + `</h1><div class="empty-state empty-error">文章加载失败，请检查筛选条件后重试。</div></main>`
		}
	}

	var body strings.Builder
	body.WriteString(`<main class="public-content"><header class="list-mast"><p class="eyebrow">文章</p><h1>`)
	body.WriteString(html.EscapeString(site.Name))
	body.WriteString(`</h1>`)
	if description := strings.TrimSpace(site.Description); description != "" {
		body.WriteString(`<p>`)
		body.WriteString(html.EscapeString(description))
		body.WriteString(`</p>`)
	}
	body.WriteString(`<form class="search-bar" action="/user" method="get"><input id="reader-search" name="q" type="search" maxlength="200" placeholder="搜索标题或正文" aria-label="搜索文章" aria-describedby="reader-search-hint" value="`)
	body.WriteString(html.EscapeString(search))
	body.WriteString(`">`)
	for _, filter := range []struct{ name, value string }{
		{name: "category", value: category},
		{name: "tag", value: tag},
		{name: "month", value: month},
	} {
		if filter.value == "" {
			continue
		}
		body.WriteString(`<input type="hidden" name="`)
		body.WriteString(filter.name)
		body.WriteString(`" value="`)
		body.WriteString(html.EscapeString(filter.value))
		body.WriteString(`">`)
	}
	body.WriteString(`<button class="button button-primary" type="submit">搜索</button></form><p id="reader-search-hint" class="search-hint">最多 200 个字符</p></header>`)

	if search != "" || category != "" || tag != "" || month != "" {
		body.WriteString(`<div class="month-filter-bar"><span>当前筛选：`)
		filters := make([]string, 0, 4)
		if search != "" {
			filters = append(filters, `“`+html.EscapeString(search)+`”`)
		}
		if category != "" {
			filters = append(filters, `分类 `+html.EscapeString(category))
		}
		if tag != "" {
			filters = append(filters, `标签 `+html.EscapeString(tag))
		}
		if month != "" {
			filters = append(filters, html.EscapeString(month))
		}
		body.WriteString(strings.Join(filters, ` · `))
		body.WriteString(`</span><a href="/user">查看全部</a></div>`)
	}

	heading := "最新文章"
	switch {
	case month != "":
		heading = month + " 的文章"
	case search != "":
		heading = "搜索“" + search + "”"
	case category != "" || tag != "":
		heading = "筛选结果"
	}
	body.WriteString(`<div class="content-heading"><div><h2>`)
	body.WriteString(html.EscapeString(heading))
	body.WriteString(`</h2><span>共 `)
	body.WriteString(strconv.Itoa(posts.Total))
	body.WriteString(` 篇</span></div></div>`)

	if len(posts.Items) == 0 {
		if search != "" || category != "" || tag != "" || month != "" {
			body.WriteString(`<div class="empty-state"><strong>没有匹配的文章</strong><span>换一个词，或清掉当前筛选。</span><a class="button button-dark" href="/user">清除筛选</a></div>`)
		} else {
			body.WriteString(`<div class="empty-state"><strong>还没有公开文章</strong><span>登录管理端，写下第一篇吧。</span><a href="/admin" class="button button-dark">进入管理端</a></div>`)
		}
		body.WriteString(`</main>`)
		return body.String()
	}

	body.WriteString(`<div class="post-list">`)
	location := service.SiteLocationOrUTC(site.Timezone)
	for _, post := range posts.Items {
		publishedAt := post.PublishedAt
		if publishedAt.IsZero() {
			publishedAt = post.UpdatedAt
		}
		if publishedAt.IsZero() {
			publishedAt = time.Unix(0, 0).UTC()
		}
		link := "/user/" + url.PathEscape(post.Slug)
		body.WriteString(`<article class="post-card"><time datetime="`)
		body.WriteString(html.EscapeString(publishedAt.Format(time.RFC3339Nano)))
		body.WriteString(`"><span class="post-day">`)
		body.WriteString(publishedAt.In(location).Format("02"))
		body.WriteString(`</span><span class="post-month">`)
		body.WriteString(publishedAt.In(location).Format("2006.01"))
		body.WriteString(`</span></time><div class="post-card-main"><h3><a href="`)
		body.WriteString(html.EscapeString(link))
		body.WriteString(`">`)
		body.WriteString(html.EscapeString(post.Title))
		body.WriteString(`</a></h3>`)
		if len(post.Categories)+len(post.Tags) > 0 {
			body.WriteString(`<div class="post-terms">`)
			for _, term := range append(post.Categories, post.Tags...) {
				body.WriteString(`<a href="`)
				body.WriteString(html.EscapeString(frontendTermURL(term)))
				body.WriteString(`">`)
				body.WriteString(html.EscapeString(term.Name))
				body.WriteString(`</a>`)
			}
			body.WriteString(`</div>`)
		}
		if post.Excerpt != "" {
			body.WriteString(`<p>`)
			body.WriteString(html.EscapeString(post.Excerpt))
			body.WriteString(`</p>`)
		}
		body.WriteString(`</div><a class="post-open" href="`)
		body.WriteString(html.EscapeString(link))
		body.WriteString(`">阅读</a></article>`)
	}
	body.WriteString(`</div>`)
	if totalPages := frontendTotalPages(posts.Total, pageSize); totalPages > 1 {
		body.WriteString(`<nav class="pagination" aria-label="文章分页">`)
		if page > 1 {
			body.WriteString(`<a class="text-button" href="`)
			body.WriteString(html.EscapeString(frontendPostListURL(r, page-1)))
			body.WriteString(`">上一页</a>`)
		} else {
			body.WriteString(`<span class="text-button" aria-disabled="true">上一页</span>`)
		}
		body.WriteString(`<span>` + strconv.Itoa(page) + ` / ` + strconv.Itoa(totalPages) + `</span>`)
		if page < totalPages {
			body.WriteString(`<a class="text-button" href="`)
			body.WriteString(html.EscapeString(frontendPostListURL(r, page+1)))
			body.WriteString(`">下一页</a>`)
		} else {
			body.WriteString(`<span class="text-button" aria-disabled="true">下一页</span>`)
		}
		body.WriteString(`</nav>`)
	}
	body.WriteString(`</main>`)
	return body.String()
}

func frontendTotalPages(total, pageSize int) int {
	if total <= 0 {
		return 1
	}
	return 1 + (total-1)/pageSize
}

func frontendPage(raw string, pageSize int) int {
	page := 1
	if strings.TrimSpace(raw) == "" {
		return page
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed < 1 {
		return page
	}
	maxInt := int(^uint(0) >> 1)
	if parsed > 1 && parsed-1 > maxInt/pageSize {
		return page
	}
	return parsed
}

func frontendTermURL(term store.Term) string {
	query := url.Values{}
	switch term.Kind {
	case "category":
		query.Set("category", term.Slug)
	case "tag":
		query.Set("tag", term.Slug)
	default:
		return "/user"
	}
	return "/user?" + query.Encode()
}

func frontendPostListURL(r *http.Request, page int) string {
	query := url.Values{}
	for _, name := range []string{"q", "category", "tag", "month"} {
		if value := r.URL.Query().Get(name); value != "" {
			query.Set(name, value)
		}
	}
	if page > 1 {
		query.Set("page", strconv.Itoa(page))
	}
	if encoded := query.Encode(); encoded != "" {
		return "/user?" + encoded
	}
	return "/user"
}
