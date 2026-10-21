package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func (s *Service) uniqueSlugContext(ctx context.Context, raw, title, currentID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	base := limitSlug(slugify(raw, title))
	existing, err := s.repo.PostBySlugContext(ctx, base)
	if errors.Is(err, store.ErrNotFound) || (err == nil && existing.ID == currentID) {
		return base, nil
	}
	if err != nil {
		return "", err
	}
	for suffix := 2; suffix < 10000; suffix++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		candidate := slugWithSuffix(base, suffix)
		existing, err := s.repo.PostBySlugContext(ctx, candidate)
		if errors.Is(err, store.ErrNotFound) || (err == nil && existing.ID == currentID) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("%w: 无法分配唯一的固定链接", ErrValidation)
}

func (s *Service) uniquePageSlugContext(ctx context.Context, raw, title, currentID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	base := limitSlug(slugify(raw, title))
	existing, err := s.repo.PageBySlugContext(ctx, base)
	if errors.Is(err, store.ErrNotFound) || (err == nil && existing.ID == currentID) {
		return base, nil
	}
	if err != nil {
		return "", err
	}
	for suffix := 2; suffix < 10000; suffix++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		candidate := slugWithSuffix(base, suffix)
		existing, err := s.repo.PageBySlugContext(ctx, candidate)
		if errors.Is(err, store.ErrNotFound) || (err == nil && existing.ID == currentID) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("%w: 无法分配唯一的页面固定链接", ErrValidation)
}

func validateSetup(input SetupInput) error {
	if strings.TrimSpace(input.SiteName) == "" || len(strings.TrimSpace(input.SiteName)) > 80 {
		return fmt.Errorf("%w: 站点名称需要 1 到 80 个字符", ErrValidation)
	}
	if !validSiteURL(input.SiteURL) {
		return fmt.Errorf("%w: 站点地址只能包含 http 或 https 协议和域名，且最多 512 字节", ErrValidation)
	}
	if !usernamePattern.MatchString(input.AdminUsername) {
		return fmt.Errorf("%w: 用户名需要 3 到 32 个字符，只能使用字母、数字、_ 或 -", ErrValidation)
	}
	if len(input.AdminPassword) < 8 {
		return fmt.Errorf("%w: 密码至少 8 个字符", ErrValidation)
	}
	if err := validateBcryptInput(input.AdminPassword, "管理员密码"); err != nil {
		return err
	}
	if !validEmail(input.AdminEmail) {
		return fmt.Errorf("%w: 邮箱格式不正确", ErrValidation)
	}
	return nil
}

func validatePost(post store.Post) error {
	if err := validateContentFields(post.Title, post.Slug, post.Excerpt, post.Content); err != nil {
		return err
	}
	if _, err := normalizeCustomFields(post.CustomFields); err != nil {
		return err
	}
	if post.Status != "" && post.Status != "draft" && post.Status != "published" {
		return fmt.Errorf("%w: 状态只能是草稿或已发布", ErrValidation)
	}
	if err := validateTermNames(post.Categories, "category"); err != nil {
		return err
	}
	if err := validateTermNames(post.Tags, "tag"); err != nil {
		return err
	}
	return nil
}

func normalizeCustomFields(fields map[string]string) (map[string]string, error) {
	const (
		maxFields     = 50
		maxKeyRunes   = 80
		maxValueRunes = 2000
		maxTotalRunes = 20_000
	)
	if len(fields) > maxFields {
		return nil, fmt.Errorf("%w: 自定义字段最多 %d 项", ErrValidation, maxFields)
	}
	normalized := make(map[string]string, len(fields))
	totalRunes := 0
	for rawKey, value := range fields {
		key := strings.TrimSpace(rawKey)
		keyRunes := runeLength(key)
		valueRunes := runeLength(value)
		if keyRunes == 0 || keyRunes > maxKeyRunes {
			return nil, fmt.Errorf("%w: 自定义字段名称需要 1 到 %d 个字符", ErrValidation, maxKeyRunes)
		}
		if valueRunes > maxValueRunes {
			return nil, fmt.Errorf("%w: 自定义字段值最多 %d 个字符", ErrValidation, maxValueRunes)
		}
		if _, exists := normalized[key]; exists {
			return nil, fmt.Errorf("%w: 自定义字段名称不能重复", ErrValidation)
		}
		totalRunes += keyRunes + valueRunes
		if totalRunes > maxTotalRunes {
			return nil, fmt.Errorf("%w: 自定义字段合计最多 %d 个字符", ErrValidation, maxTotalRunes)
		}
		normalized[key] = value
	}
	return normalized, nil
}

func validatePage(page store.Page) error {
	if err := validateContentFields(page.Title, page.Slug, page.Excerpt, page.Content); err != nil {
		return err
	}
	if page.Status != "" && page.Status != "draft" && page.Status != "published" {
		return fmt.Errorf("%w: 状态只能是草稿或已发布", ErrValidation)
	}
	return nil
}

func validateContentFields(title, slug, excerpt, content string) error {
	titleLength := runeLength(strings.TrimSpace(title))
	if titleLength < 1 || titleLength > 200 {
		return fmt.Errorf("%w: 标题需要 1 到 200 个字符", ErrValidation)
	}
	if runeLength(strings.TrimSpace(slug)) > 120 {
		return fmt.Errorf("%w: 固定链接最多 120 个字符", ErrValidation)
	}
	if runeLength(excerpt) > 500 {
		return fmt.Errorf("%w: 摘要最多 500 个字符", ErrValidation)
	}
	if runeLength(content) > MaxContentRunes {
		return fmt.Errorf("%w: 正文最多 %d 个字符", ErrValidation, MaxContentRunes)
	}
	return nil
}

func normalizePostTerms(post *store.Post) error {
	post.Categories = normalizeTerms(post.Categories, "category")
	post.Tags = normalizeTerms(post.Tags, "tag")
	return nil
}

func normalizeTerms(input []store.Term, kind string) []store.Term {
	seen := make(map[string]struct{}, len(input))
	terms := make([]store.Term, 0, len(input))
	for _, term := range input {
		name := strings.TrimSpace(term.Name)
		key := strings.ToLower(name)
		if name == "" || key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		terms = append(terms, store.Term{Name: name, Kind: kind})
	}
	return terms
}

func validateTermNames(terms []store.Term, kind string) error {
	if len(terms) > 20 {
		return fmt.Errorf("%w: 最多 20 个%s", ErrValidation, termKindLabel(kind))
	}
	for _, term := range terms {
		if runeLength(strings.TrimSpace(term.Name)) < 1 || runeLength(strings.TrimSpace(term.Name)) > 60 {
			return fmt.Errorf("%w: %s名称需要 1 到 60 个字符", ErrValidation, termKindLabel(kind))
		}
	}
	return nil
}

func validateTermInput(input TermInput) error {
	kind := strings.TrimSpace(input.Kind)
	name := strings.TrimSpace(input.Name)
	if kind != "category" && kind != "tag" {
		return fmt.Errorf("%w: 类型只能是分类或标签", ErrValidation)
	}
	if runeLength(name) < 1 || runeLength(name) > 60 {
		return fmt.Errorf("%w: 名称需要 1 到 60 个字符", ErrValidation)
	}
	return nil
}

func validCommentStatus(status string) bool {
	return status == "pending" || status == "approved" || status == "spam"
}

func validateUserInput(input UserInput, creating bool) error {
	if creating && !usernamePattern.MatchString(input.Username) {
		return fmt.Errorf("%w: 用户名需要 3 到 32 个字符，只能使用字母、数字、_ 或 -", ErrValidation)
	}
	if !validEmail(input.Email) {
		return fmt.Errorf("%w: 邮箱格式不正确", ErrValidation)
	}
	if creating && len(input.Password) < 8 {
		return fmt.Errorf("%w: 密码至少 8 个字符", ErrValidation)
	}
	if creating {
		if err := validateBcryptInput(input.Password, "密码"); err != nil {
			return err
		}
	}
	if !validStaffRole(input.Role) {
		return fmt.Errorf("%w: 角色只能是编辑或审核员", ErrValidation)
	}
	return nil
}

func validateBcryptInput(value, label string) error {
	if len(value) > maxBcryptInputBytes {
		return fmt.Errorf("%w: %s最多 72 个 UTF-8 字节（bcrypt 限制）", ErrValidation, label)
	}
	return nil
}

func termKindLabel(kind string) string {
	switch kind {
	case "category":
		return "分类"
	case "tag":
		return "标签"
	default:
		return "分类或标签"
	}
}

func validStaffRole(role string) bool {
	return role == "editor" || role == "moderator"
}

func runeLength(value string) int {
	return len([]rune(value))
}

func normalizeSearchQuery(value string) (string, error) {
	if utf8.RuneCountInString(value) > MaxSearchRunes {
		return "", fmt.Errorf("%w: 搜索词最多 %d 个字符", ErrValidation, MaxSearchRunes)
	}
	return strings.TrimSpace(value), nil
}

func validURL(raw string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	return err == nil && isHTTPScheme(parsed.Scheme) && parsed.User == nil && validHTTPAuthority(parsed)
}

func isHTTPScheme(scheme string) bool {
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}

func validHTTPAuthority(parsed *url.URL) bool {
	if parsed == nil || parsed.Hostname() == "" || strings.HasSuffix(parsed.Host, ":") {
		return false
	}
	port := parsed.Port()
	if port == "" {
		return true
	}
	value, err := strconv.ParseUint(port, 10, 16)
	return err == nil && value > 0
}

func validSiteURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 || len(trimmed) > MaxSiteURLBytes {
		return false
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || !isHTTPScheme(parsed.Scheme) || !validHTTPAuthority(parsed) ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(trimmed, "#") ||
		(parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	return true
}

func validEmail(raw string) bool {
	parsed, err := mail.ParseAddress(strings.TrimSpace(raw))
	return err == nil && parsed.Address == strings.TrimSpace(raw) && strings.Contains(parsed.Address, "@")
}

func slugify(raw, title string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = strings.TrimSpace(title)
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '-' {
			return unicode.ToLower(r)
		}
		return '-'
	}, value)
	value = strings.Trim(value, "-")
	if value == "" {
		return fmt.Sprintf("post-%d", time.Now().UnixNano())
	}
	return value
}

func limitSlug(value string) string {
	runes := []rune(strings.Trim(strings.TrimSpace(value), "-"))
	if len(runes) > 120 {
		runes = runes[:120]
	}
	return strings.Trim(string(runes), "-")
}

func slugWithSuffix(base string, suffix int) string {
	suffixText := fmt.Sprintf("-%d", suffix)
	baseRunes := []rune(strings.Trim(base, "-"))
	maxBaseLength := 120 - len([]rune(suffixText))
	if len(baseRunes) > maxBaseLength {
		baseRunes = baseRunes[:maxBaseLength]
	}
	baseValue := strings.Trim(string(baseRunes), "-")
	if baseValue == "" {
		baseValue = "post"
	}
	return baseValue + suffixText
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
