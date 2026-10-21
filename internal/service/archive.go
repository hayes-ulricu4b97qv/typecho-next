package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func (s *Service) ListPostsByTerms(search, categorySlug, tagSlug, month, order string, includeDrafts bool, status string, page, pageSize int) (store.PostPage, error) {
	return s.ListPostsByTermsContext(context.Background(), search, categorySlug, tagSlug, month, order, includeDrafts, status, page, pageSize)
}

func (s *Service) ListPostsByTermsContext(ctx context.Context, search, categorySlug, tagSlug, month, order string, includeDrafts bool, status string, page, pageSize int) (store.PostPage, error) {
	search, err := normalizeSearchQuery(search)
	if err != nil {
		return store.PostPage{}, err
	}
	categorySlug = strings.TrimSpace(categorySlug)
	tagSlug = strings.TrimSpace(tagSlug)
	order = strings.TrimSpace(order)
	status = strings.TrimSpace(status)
	if order != "" && order != "updated" && order != "published" {
		return store.PostPage{}, fmt.Errorf("%w: 排序只能是 updated 或 published", ErrValidation)
	}
	if status != "" && status != "draft" && status != "published" {
		return store.PostPage{}, fmt.Errorf("%w: 文章状态只能是 draft 或 published", ErrValidation)
	}
	query := store.PostQuery{
		Search:        search,
		IncludeDrafts: includeDrafts,
		Status:        status,
		CategorySlug:  categorySlug,
		TagSlug:       tagSlug,
		Order:         order,
		Page:          page,
		PageSize:      pageSize,
	}
	month = strings.TrimSpace(month)
	if month != "" {
		location, err := s.siteLocationContext(ctx)
		if err != nil {
			return store.PostPage{}, err
		}
		query.PublishedFrom, query.PublishedBefore, err = monthWindow(month, location)
		if err != nil {
			return store.PostPage{}, err
		}
	}
	return s.repo.SearchPostsContext(ctx, query)
}

func FeedBlurb(excerpt, content string) string {
	if text := strings.TrimSpace(excerpt); text != "" {
		return text
	}
	const limit = 280
	runeCount := 0
	for index := range content {
		if runeCount == limit {
			return content[:index] + "…"
		}
		runeCount++
	}
	return content
}

func (s *Service) MonthArchive() ([]store.MonthCount, error) {
	return s.MonthArchiveContext(context.Background())
}

func (s *Service) MonthArchiveContext(ctx context.Context) ([]store.MonthCount, error) {
	loc, err := s.siteLocationContext(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.PublishedMonthCountsContext(ctx, loc)
	if err != nil {
		return nil, err
	}
	sortMonths(items)
	return items, nil
}

func (s *Service) PublishedNeighbors(post store.Post) (*store.Post, *store.Post, error) {
	return s.PublishedNeighborsContext(context.Background(), post)
}

func (s *Service) PublishedNeighborsContext(ctx context.Context, post store.Post) (*store.Post, *store.Post, error) {
	if post.Status != "published" || post.PublishedAt.IsZero() {
		return nil, nil, nil
	}
	return s.repo.PublishedNeighborsContext(ctx, post)
}

func (s *Service) siteLocationContext(ctx context.Context) (*time.Location, error) {
	site, err := s.repo.SiteContext(ctx)
	if err != nil {
		return nil, err
	}
	return SiteLocationOrUTC(site.Timezone), nil
}

// SiteLocationOrUTC applies the site's portable time zone and fallback policy.
func SiteLocationOrUTC(name string) *time.Location {
	name = strings.TrimSpace(name)
	loc, err := loadSiteLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func loadSiteLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "Local" {
		return nil, errors.New("local time zone is not portable")
	}
	return time.LoadLocation(name)
}

func monthWindow(month string, loc *time.Location) (time.Time, time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	parsed, err := time.ParseInLocation("2006-01", month, loc)
	if err != nil || parsed.Format("2006-01") != month {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: 月份格式必须是 YYYY-MM", ErrValidation)
	}
	return parsed, parsed.AddDate(0, 1, 0), nil
}

func sortMonths(items []store.MonthCount) {
	for index := 1; index < len(items); index++ {
		current := items[index]
		place := index
		for place > 0 && items[place-1].Month < current.Month {
			items[place] = items[place-1]
			place--
		}
		items[place] = current
	}
}
