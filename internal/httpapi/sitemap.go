package httpapi

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

const (
	// 10,000 entries stay below the 50 MB sitemap limit with a 512-byte site
	// URL and a maximally escaped 120-rune slug.
	sitemapURLsPerFile = 10_000
	// The lower index cap also keeps its worst-case escaped XML below 50 MB.
	sitemapIndexMaxFiles = 15_000
	sitemapNamespace     = "http://www.sitemaps.org/schemas/sitemap/0.9"
)

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Location string `xml:"loc"`
	LastMod  string `xml:"lastmod,omitempty"`
}

type sitemapIndex struct {
	XMLName xml.Name          `xml:"sitemapindex"`
	XMLNS   string            `xml:"xmlns,attr"`
	Items   []sitemapIndexURL `xml:"sitemap"`
}

type sitemapIndexURL struct {
	Location string `xml:"loc"`
}

type sitemapCounts struct {
	posts int
	terms int
	pages int
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
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
	if len(baseURL) > service.MaxSiteURLBytes {
		writeError(w, http.StatusInternalServerError, "read_failed", "Site URL exceeds the supported size for public indexes")
		return
	}
	requestedPart, hasPart, ok := requestedSitemapPart(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "Sitemap part must be a positive integer")
		return
	}
	counts, err := s.sitemapCounts(r.Context())
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read published sitemap entries")
		return
	}
	total, ok := counts.total()
	if !ok {
		writeError(w, http.StatusInternalServerError, "read_failed", "Sitemap entry count exceeds the supported range")
		return
	}
	partCount := 1 + (total-1)/sitemapURLsPerFile
	if partCount > sitemapIndexMaxFiles {
		writeError(w, http.StatusInternalServerError, "read_failed", "Sitemap index exceeds the supported size")
		return
	}
	if r.Context().Err() != nil {
		return
	}

	if hasPart {
		if requestedPart > partCount {
			writeError(w, http.StatusNotFound, "not_found", "Sitemap part not found")
			return
		}
		start := (requestedPart - 1) * sitemapURLsPerFile
		length := sitemapMin(sitemapURLsPerFile, total-start)
		urls, err := s.sitemapURLs(r.Context(), baseURL, counts, start, start+length)
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read published sitemap entries")
			return
		}
		if r.Context().Err() != nil {
			return
		}
		writeXML(w, http.StatusOK, "application/xml; charset=utf-8", sitemapURLSet{XMLNS: sitemapNamespace, URLs: urls})
		return
	}

	if partCount > 1 {
		index := sitemapIndex{XMLNS: sitemapNamespace, Items: make([]sitemapIndexURL, 0, partCount)}
		for part := 1; part <= partCount; part++ {
			if r.Context().Err() != nil {
				return
			}
			index.Items = append(index.Items, sitemapIndexURL{Location: fmt.Sprintf("%s/sitemap.xml?part=%d", baseURL, part)})
		}
		if r.Context().Err() != nil {
			return
		}
		writeXML(w, http.StatusOK, "application/xml; charset=utf-8", index)
		return
	}

	urls, err := s.sitemapURLs(r.Context(), baseURL, counts, 0, total)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read published sitemap entries")
		return
	}
	if r.Context().Err() != nil {
		return
	}
	writeXML(w, http.StatusOK, "application/xml; charset=utf-8", sitemapURLSet{XMLNS: sitemapNamespace, URLs: urls})
}

func requestedSitemapPart(r *http.Request) (int, bool, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, true, false
	}
	values, exists := query["part"]
	if !exists {
		return 0, false, true
	}
	if len(values) != 1 {
		return 0, true, false
	}
	part, err := strconv.Atoi(values[0])
	if err != nil || part < 1 {
		return 0, true, false
	}
	return part, true, true
}

func (c sitemapCounts) total() (int, bool) {
	total := 1 // reader home
	for _, count := range []int{c.posts, c.terms, c.pages} {
		if count < 0 || count > int(^uint(0)>>1)-total {
			return 0, false
		}
		total += count
	}
	return total, true
}

func (s *Server) sitemapCounts(ctx context.Context) (sitemapCounts, error) {
	posts, err := s.app.ListPostsContext(ctx, "", false, 1, 1)
	if err != nil {
		return sitemapCounts{}, fmt.Errorf("count sitemap posts: %w", err)
	}
	terms, err := s.app.ListTermsContext(ctx, "", "", false, 1, 1)
	if err != nil {
		return sitemapCounts{}, fmt.Errorf("count sitemap terms: %w", err)
	}
	pages, err := s.app.ListPagesContext(ctx, "", false, 1, 1)
	if err != nil {
		return sitemapCounts{}, fmt.Errorf("count sitemap pages: %w", err)
	}
	return sitemapCounts{posts: posts.Total, terms: terms.Total, pages: pages.Total}, nil
}

func (s *Server) sitemapURLs(ctx context.Context, baseURL string, counts sitemapCounts, start, end int) ([]sitemapURL, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	urls := make([]sitemapURL, 0, end-start)
	if start == 0 && end > 0 {
		urls = append(urls, sitemapURL{Location: baseURL + "/user"})
	}

	groupStart := 1
	if from, to, ok := sitemapRange(start, end, groupStart, counts.posts); ok {
		if err := s.appendPostSitemapURLs(ctx, &urls, baseURL, from, to); err != nil {
			return nil, err
		}
	}
	groupStart += counts.posts
	if from, to, ok := sitemapRange(start, end, groupStart, counts.terms); ok {
		if err := s.appendTermSitemapURLs(ctx, &urls, baseURL, from, to); err != nil {
			return nil, err
		}
	}
	groupStart += counts.terms
	if from, to, ok := sitemapRange(start, end, groupStart, counts.pages); ok {
		if err := s.appendPageSitemapURLs(ctx, &urls, baseURL, from, to); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return urls, nil
}

func sitemapRange(start, end, groupStart, groupCount int) (int, int, bool) {
	from := sitemapMax(start, groupStart)
	to := sitemapMin(end, groupStart+groupCount)
	if from >= to {
		return 0, 0, false
	}
	return from - groupStart, to - groupStart, true
}

func (s *Server) appendPostSitemapURLs(ctx context.Context, urls *[]sitemapURL, baseURL string, start, end int) error {
	for offset := start; offset < end; {
		if err := ctx.Err(); err != nil {
			return err
		}
		pageNumber, pageOffset := offset/100+1, offset%100
		posts, err := s.app.ListPostsContext(ctx, "", false, pageNumber, 100)
		if err != nil {
			return fmt.Errorf("read sitemap posts: %w", err)
		}
		if pageOffset >= len(posts.Items) {
			return errors.New("sitemap post set changed during generation")
		}
		count := sitemapMin(end-offset, len(posts.Items)-pageOffset)
		for _, post := range posts.Items[pageOffset : pageOffset+count] {
			lastMod := post.UpdatedAt
			if lastMod.IsZero() {
				lastMod = post.PublishedAt
			}
			entry := sitemapURL{Location: baseURL + "/user/" + url.PathEscape(post.Slug)}
			if !lastMod.IsZero() {
				entry.LastMod = lastMod.UTC().Format("2006-01-02")
			}
			*urls = append(*urls, entry)
		}
		offset += count
	}
	return nil
}

func (s *Server) appendTermSitemapURLs(ctx context.Context, urls *[]sitemapURL, baseURL string, start, end int) error {
	for offset := start; offset < end; {
		if err := ctx.Err(); err != nil {
			return err
		}
		pageNumber, pageOffset := offset/100+1, offset%100
		terms, err := s.app.ListTermsContext(ctx, "", "", false, pageNumber, 100)
		if err != nil {
			return fmt.Errorf("read sitemap terms: %w", err)
		}
		if pageOffset >= len(terms.Items) {
			return errors.New("sitemap term set changed during generation")
		}
		count := sitemapMin(end-offset, len(terms.Items)-pageOffset)
		for _, term := range terms.Items[pageOffset : pageOffset+count] {
			entry := sitemapURL{Location: baseURL + "/user?" + term.Kind + "=" + url.QueryEscape(term.Slug)}
			if !term.UpdatedAt.IsZero() {
				entry.LastMod = term.UpdatedAt.UTC().Format("2006-01-02")
			}
			*urls = append(*urls, entry)
		}
		offset += count
	}
	return nil
}

func (s *Server) appendPageSitemapURLs(ctx context.Context, urls *[]sitemapURL, baseURL string, start, end int) error {
	for offset := start; offset < end; {
		if err := ctx.Err(); err != nil {
			return err
		}
		pageNumber, pageOffset := offset/100+1, offset%100
		pages, err := s.app.ListPagesContext(ctx, "", false, pageNumber, 100)
		if err != nil {
			return fmt.Errorf("read sitemap pages: %w", err)
		}
		if pageOffset >= len(pages.Items) {
			return errors.New("sitemap page set changed during generation")
		}
		count := sitemapMin(end-offset, len(pages.Items)-pageOffset)
		for _, item := range pages.Items[pageOffset : pageOffset+count] {
			entry := sitemapURL{Location: baseURL + "/page/" + url.PathEscape(item.Slug)}
			if !item.UpdatedAt.IsZero() {
				entry.LastMod = item.UpdatedAt.UTC().Format("2006-01-02")
			}
			*urls = append(*urls, entry)
		}
		offset += count
	}
	return nil
}

func sitemapMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sitemapMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
