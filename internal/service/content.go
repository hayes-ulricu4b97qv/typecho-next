package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func (s *Service) ListPosts(search string, includeDrafts bool, page, pageSize int) (store.PostPage, error) {
	return s.ListPostsContext(context.Background(), search, includeDrafts, page, pageSize)
}

func (s *Service) ListPostsContext(ctx context.Context, search string, includeDrafts bool, page, pageSize int) (store.PostPage, error) {
	search, err := normalizeSearchQuery(search)
	if err != nil {
		return store.PostPage{}, err
	}
	return s.repo.SearchPostsContext(ctx, store.PostQuery{Search: search, IncludeDrafts: includeDrafts, Page: page, PageSize: pageSize})
}

func (s *Service) ListPostsForFeed(ctx context.Context, page, pageSize int) (store.PostPage, error) {
	return s.repo.SearchPostsContext(ctx, store.PostQuery{IncludeContent: true, ContentPreview: true, Page: page, PageSize: pageSize})
}

func (s *Service) ListPostsByTerm(search, kind, slug string, includeDrafts bool, page, pageSize int) (store.PostPage, error) {
	search, err := normalizeSearchQuery(search)
	if err != nil {
		return store.PostPage{}, err
	}
	kind = strings.TrimSpace(kind)
	slug = strings.TrimSpace(slug)
	if (kind == "") != (slug == "") || (kind != "" && kind != "category" && kind != "tag") {
		return store.PostPage{}, fmt.Errorf("%w: 需要有效的分类或标签筛选", ErrValidation)
	}
	return s.repo.SearchPosts(store.PostQuery{Search: search, IncludeDrafts: includeDrafts, TermKind: kind, TermSlug: slug, Page: page, PageSize: pageSize})
}

func (s *Service) ListTerms(kind, search string, includeDrafts bool, page, pageSize int) (store.TermPage, error) {
	return s.ListTermsContext(context.Background(), kind, search, includeDrafts, page, pageSize)
}

func (s *Service) ListTermsContext(ctx context.Context, kind, search string, includeDrafts bool, page, pageSize int) (store.TermPage, error) {
	kind = strings.TrimSpace(kind)
	if kind != "" && kind != "category" && kind != "tag" {
		return store.TermPage{}, fmt.Errorf("%w: 类型只能是分类或标签", ErrValidation)
	}
	search, err := normalizeSearchQuery(search)
	if err != nil {
		return store.TermPage{}, err
	}
	return s.repo.ListTermsContext(ctx, store.TermQuery{Kind: kind, Search: search, IncludeDrafts: includeDrafts, Page: page, PageSize: pageSize})
}

func (s *Service) CreateTerm(input TermInput) (*store.Term, error) {
	return s.CreateTermContext(context.Background(), input)
}

func (s *Service) CreateTermContext(ctx context.Context, input TermInput) (*store.Term, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateTermInput(input); err != nil {
		return nil, err
	}
	term, _, err := s.repo.SaveTermWithChangeContext(ctx, store.Term{Name: strings.TrimSpace(input.Name), Kind: strings.TrimSpace(input.Kind)})
	if err != nil {
		return nil, err
	}
	return &term, nil
}

func (s *Service) UpdateTerm(id string, input TermInput) (*store.Term, error) {
	term, _, err := s.UpdateTermWithChange(id, input)
	return term, err
}

func (s *Service) UpdateTermWithChange(id string, input TermInput) (*store.Term, bool, error) {
	return s.UpdateTermWithChangeContext(context.Background(), id, input)
}

func (s *Service) UpdateTermWithChangeContext(ctx context.Context, id string, input TermInput) (*store.Term, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := validateTermInput(input); err != nil {
		return nil, false, err
	}
	term, changed, err := s.repo.SaveTermWithChangeContext(ctx, store.Term{ID: id, Name: strings.TrimSpace(input.Name), Kind: strings.TrimSpace(input.Kind)})
	if err != nil {
		return nil, false, err
	}
	return &term, changed, nil
}

func (s *Service) DeleteTerm(id string) error {
	return s.DeleteTermContext(context.Background(), id)
}

func (s *Service) DeleteTermContext(ctx context.Context, id string) error {
	return s.repo.DeleteTermContext(ctx, id)
}

func (s *Service) GetPost(key string, includeDraft bool) (store.Post, error) {
	return s.GetPostContext(context.Background(), key, includeDraft)
}

func (s *Service) GetPostContext(ctx context.Context, key string, includeDraft bool) (store.Post, error) {
	post, err := s.repo.PostBySlugContext(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		post, err = s.repo.PostContext(ctx, key)
	}
	if err != nil {
		return store.Post{}, err
	}
	if !includeDraft && !visibleToPublic(post, time.Now().UTC()) {
		return store.Post{}, store.ErrNotFound
	}
	return post, nil
}

func visibleToPublic(post store.Post, now time.Time) bool {
	return visiblePublishedContentToPublic(post.Status, post.PublishedAt, now)
}

func visiblePageToPublic(page store.Page, now time.Time) bool {
	return visiblePublishedContentToPublic(page.Status, page.PublishedAt, now)
}

func visiblePublishedContentToPublic(status string, publishedAt, now time.Time) bool {
	if status != "published" || publishedAt.IsZero() {
		return false
	}
	return !publishedAt.After(now)
}

func (s *Service) RevealPost(post store.Post, editor bool, passphrase string) bool {
	if editor || post.PassphraseHash == "" {
		return true
	}
	return bcrypt.CompareHashAndPassword([]byte(post.PassphraseHash), []byte(passphrase)) == nil
}

func (s *Service) CreatePost(post store.Post) (store.Post, error) {
	return s.CreatePostContext(context.Background(), post)
}

func (s *Service) CreatePostContext(ctx context.Context, post store.Post) (store.Post, error) {
	if err := ctx.Err(); err != nil {
		return store.Post{}, err
	}
	if err := validatePost(post); err != nil {
		return store.Post{}, err
	}
	customFields, err := normalizeCustomFields(post.CustomFields)
	if err != nil {
		return store.Post{}, err
	}
	post.CustomFields = customFields
	post.SetCustomFields = true
	post.Title = strings.TrimSpace(post.Title)
	if err := normalizePostTerms(&post); err != nil {
		return store.Post{}, err
	}
	requestedPublish := post.RequestedPublishAt
	post.PublishedAt = time.Time{}
	if post.Status == "published" && !requestedPublish.IsZero() {
		post.PublishedAt = requestedPublish.UTC()
	}
	if err := hashPassphrase(&post); err != nil {
		return store.Post{}, err
	}
	post.Slug, err = s.uniqueSlugContext(ctx, post.Slug, post.Title, "")
	if err != nil {
		return store.Post{}, err
	}
	return s.repo.SavePostContext(ctx, post)
}

func (s *Service) UpdatePost(id string, input store.Post) (store.Post, error) {
	updated, _, err := s.UpdatePostWithChange(id, input)
	return updated, err
}

func (s *Service) UpdatePostWithChange(id string, input store.Post) (store.Post, bool, error) {
	return s.UpdatePostWithChangeContext(context.Background(), id, input)
}

func (s *Service) UpdatePostWithChangeContext(ctx context.Context, id string, input store.Post) (store.Post, bool, error) {
	current, err := s.repo.PostContext(ctx, id)
	if err != nil {
		return store.Post{}, false, err
	}
	expectedUpdatedAt := input.ExpectedUpdatedAt
	if expectedUpdatedAt.IsZero() {
		expectedUpdatedAt = current.UpdatedAt
	}
	if err := validatePost(input); err != nil {
		return store.Post{}, false, err
	}
	if input.Status == "" {
		input.Status = "draft"
	}
	if !input.SetCustomFields {
		input.CustomFields = current.CustomFields
	}
	input.CustomFields, err = normalizeCustomFields(input.CustomFields)
	if err != nil {
		return store.Post{}, false, err
	}
	input.SetCustomFields = true
	input.ID = current.ID
	input.Title = strings.TrimSpace(input.Title)
	if err := normalizePostTerms(&input); err != nil {
		return store.Post{}, false, err
	}
	requestedSlug := strings.TrimSpace(input.Slug)
	if requestedSlug == "" {
		requestedSlug = current.Slug
	}
	input.Slug, err = s.uniqueSlugContext(ctx, requestedSlug, input.Title, input.ID)
	if err != nil {
		return store.Post{}, false, err
	}
	if !current.PublishedAt.IsZero() {
		input.PublishedAt = current.PublishedAt
	} else if input.Status == "published" && !input.RequestedPublishAt.IsZero() {
		input.PublishedAt = input.RequestedPublishAt.UTC()
	} else {
		input.PublishedAt = time.Time{}
	}
	if input.SetPassphrase && passphraseMatches(current.PassphraseHash, input.PassphrasePlain) {
		input.SetPassphrase = false
		input.PassphrasePlain = ""
	}
	if !input.SetPassphrase {
		input.PassphraseHash = current.PassphraseHash
	}
	input.ExpectedUpdatedAt = expectedUpdatedAt
	if err := ctx.Err(); err != nil {
		return store.Post{}, false, err
	}
	if err := hashPassphrase(&input); err != nil {
		return store.Post{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return store.Post{}, false, err
	}
	if samePostContent(current, input) {
		return current, false, nil
	}
	updated, err := s.repo.SavePostContext(ctx, input)
	return updated, err == nil, err
}

func passphraseMatches(currentHash, requestedPlain string) bool {
	if requestedPlain == "" {
		return currentHash == ""
	}
	return currentHash != "" && bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(requestedPlain)) == nil
}

func samePostContent(left, right store.Post) bool {
	return left.Title == right.Title &&
		left.Slug == right.Slug &&
		left.Excerpt == right.Excerpt &&
		left.Content == right.Content &&
		left.Status == right.Status &&
		left.AllowComment == right.AllowComment &&
		left.PublishedAt.Equal(right.PublishedAt) &&
		left.PassphraseHash == right.PassphraseHash &&
		equalStringFields(left.CustomFields, right.CustomFields) &&
		equalTermNames(left.Categories, right.Categories) &&
		equalTermNames(left.Tags, right.Tags)
}

func equalStringFields(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if other, ok := right[key]; !ok || other != value {
			return false
		}
	}
	return true
}

func equalTermNames(left, right []store.Term) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, term := range left {
		counts[term.Name]++
	}
	for _, term := range right {
		if counts[term.Name] == 0 {
			return false
		}
		counts[term.Name]--
	}
	return true
}

func hashPassphrase(post *store.Post) error {
	if !post.SetPassphrase {
		return nil
	}
	plain := post.PassphrasePlain
	post.PassphrasePlain = ""
	if plain == "" {
		post.PassphraseHash = ""
		return nil
	}
	if err := validateBcryptInput(plain, "阅读口令"); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	post.PassphraseHash = string(hash)
	return nil
}

func (s *Service) ListPostRevisionPageContext(ctx context.Context, postID string, page, pageSize int) (store.PostRevisionPage, error) {
	if _, err := s.repo.PostContext(ctx, postID); err != nil {
		return store.PostRevisionPage{}, err
	}
	return s.repo.ListPostRevisionPageContext(ctx, postID, page, pageSize)
}

func (s *Service) RestorePostRevision(postID, revisionID string) (store.Post, error) {
	restored, _, err := s.RestorePostRevisionWithChange(postID, revisionID)
	return restored, err
}

func (s *Service) RestorePostRevisionWithChange(postID, revisionID string) (store.Post, bool, error) {
	return s.RestorePostRevisionWithChangeContext(context.Background(), postID, revisionID)
}

func (s *Service) RestorePostRevisionWithChangeContext(ctx context.Context, postID, revisionID string) (store.Post, bool, error) {
	current, err := s.repo.PostContext(ctx, postID)
	if err != nil {
		return store.Post{}, false, err
	}
	revision, err := s.repo.PostRevisionContext(ctx, revisionID)
	if err != nil {
		return store.Post{}, false, err
	}
	if revision.PostID != current.ID {
		return store.Post{}, false, store.ErrNotFound
	}
	original := current
	current.Title = revision.Title
	current.Excerpt = revision.Excerpt
	current.Content = revision.Content
	current.CustomFields = revision.CustomFields
	current.SetCustomFields = true
	current.SetPassphrase = false
	current.ExpectedUpdatedAt = current.UpdatedAt
	if err := ctx.Err(); err != nil {
		return store.Post{}, false, err
	}
	if err := validatePost(current); err != nil {
		return store.Post{}, false, err
	}
	if samePostContent(original, current) {
		return current, false, nil
	}
	restored, err := s.repo.SavePostContext(ctx, current)
	return restored, err == nil, err
}

func (s *Service) DeletePost(id string) error {
	return s.DeletePostContext(context.Background(), id)
}

func (s *Service) DeletePostContext(ctx context.Context, id string) error {
	return s.repo.DeletePostContext(ctx, id)
}

func (s *Service) ListPages(search string, includeDrafts bool, page, pageSize int) (store.PagePage, error) {
	return s.ListPagesContext(context.Background(), search, includeDrafts, page, pageSize)
}

func (s *Service) ListPagesContext(ctx context.Context, search string, includeDrafts bool, page, pageSize int) (store.PagePage, error) {
	search, err := normalizeSearchQuery(search)
	if err != nil {
		return store.PagePage{}, err
	}
	return s.repo.SearchPagesContext(ctx, store.PageQuery{Search: search, IncludeDrafts: includeDrafts, Page: page, PageSize: pageSize})
}

func (s *Service) GetPage(key string, includeDraft bool) (store.Page, error) {
	return s.GetPageContext(context.Background(), key, includeDraft)
}

func (s *Service) GetPageContext(ctx context.Context, key string, includeDraft bool) (store.Page, error) {
	page, err := s.repo.PageBySlugContext(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		page, err = s.repo.PageContext(ctx, key)
	}
	if err != nil {
		return store.Page{}, err
	}
	if !includeDraft && !visiblePageToPublic(page, time.Now().UTC()) {
		return store.Page{}, store.ErrNotFound
	}
	return page, nil
}

func (s *Service) CreatePage(page store.Page) (store.Page, error) {
	return s.CreatePageContext(context.Background(), page)
}

func (s *Service) CreatePageContext(ctx context.Context, page store.Page) (store.Page, error) {
	if err := ctx.Err(); err != nil {
		return store.Page{}, err
	}
	if err := validatePage(page); err != nil {
		return store.Page{}, err
	}
	page.Title = strings.TrimSpace(page.Title)
	var err error
	page.Slug, err = s.uniquePageSlugContext(ctx, page.Slug, page.Title, "")
	if err != nil {
		return store.Page{}, err
	}
	return s.repo.SavePageContext(ctx, page)
}

func (s *Service) UpdatePage(id string, input store.Page) (store.Page, error) {
	updated, _, err := s.UpdatePageWithChange(id, input)
	return updated, err
}

func (s *Service) UpdatePageWithChange(id string, input store.Page) (store.Page, bool, error) {
	return s.UpdatePageWithChangeContext(context.Background(), id, input)
}

func (s *Service) UpdatePageWithChangeContext(ctx context.Context, id string, input store.Page) (store.Page, bool, error) {
	current, err := s.repo.PageContext(ctx, id)
	if err != nil {
		return store.Page{}, false, err
	}
	expectedUpdatedAt := input.ExpectedUpdatedAt
	if expectedUpdatedAt.IsZero() {
		expectedUpdatedAt = current.UpdatedAt
	}
	if err := validatePage(input); err != nil {
		return store.Page{}, false, err
	}
	if input.Status == "" {
		input.Status = "draft"
	}
	input.ID = current.ID
	input.Title = strings.TrimSpace(input.Title)
	requestedSlug := strings.TrimSpace(input.Slug)
	if requestedSlug == "" {
		requestedSlug = current.Slug
	}
	input.Slug, err = s.uniquePageSlugContext(ctx, requestedSlug, input.Title, input.ID)
	if err != nil {
		return store.Page{}, false, err
	}
	input.PublishedAt = current.PublishedAt
	input.ExpectedUpdatedAt = expectedUpdatedAt
	if err := ctx.Err(); err != nil {
		return store.Page{}, false, err
	}
	if samePageContent(current, input) {
		return current, false, nil
	}
	updated, err := s.repo.SavePageContext(ctx, input)
	return updated, err == nil, err
}

func samePageContent(left, right store.Page) bool {
	return left.Title == right.Title &&
		left.Slug == right.Slug &&
		left.Excerpt == right.Excerpt &&
		left.Content == right.Content &&
		left.Status == right.Status &&
		left.PublishedAt.Equal(right.PublishedAt)
}

func (s *Service) DeletePage(id string) error {
	return s.DeletePageContext(context.Background(), id)
}

func (s *Service) DeletePageContext(ctx context.Context, id string) error {
	return s.repo.DeletePageContext(ctx, id)
}
