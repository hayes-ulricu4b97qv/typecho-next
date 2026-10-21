package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

type scanner interface {
	Scan(...any) error
}

func scanPost(row scanner) (Post, error) {
	var post Post
	var allowComment int
	var publishedAt, updatedAt sql.NullString
	var customFields string
	err := row.Scan(&post.ID, &post.Title, &post.Slug, &post.Excerpt, &post.Content, &post.Status, &allowComment, &publishedAt, &updatedAt, &post.PassphraseHash, &customFields)
	if err != nil {
		return Post{}, err
	}
	post.CustomFields, err = decodeCustomFields(customFields)
	if err != nil {
		return Post{}, err
	}
	post.AllowComment = allowComment != 0
	if publishedAt.Valid {
		post.PublishedAt = parseTime(publishedAt.String)
	}
	if updatedAt.Valid {
		post.UpdatedAt = parseTime(updatedAt.String)
	}
	return post, nil
}

func decodeCustomFields(raw string) (map[string]string, error) {
	fields := map[string]string{}
	if raw == "" || raw == "null" {
		return fields, nil
	}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("decode post custom fields: %w", err)
	}
	if fields == nil {
		fields = map[string]string{}
	}
	return fields, nil
}

func encodeCustomFields(fields map[string]string) (string, error) {
	if fields == nil {
		fields = map[string]string{}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("encode post custom fields: %w", err)
	}
	return string(encoded), nil
}

func equalCustomFields(left, right map[string]string) bool {
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

func scanPostSummary(row scanner) (Post, error) {
	var post Post
	var allowComment int
	var publishedAt, updatedAt sql.NullString
	err := row.Scan(&post.ID, &post.Title, &post.Slug, &post.Excerpt, &post.Status, &allowComment, &publishedAt, &updatedAt, &post.PassphraseHash)
	if err != nil {
		return Post{}, err
	}
	post.AllowComment = allowComment != 0
	if publishedAt.Valid {
		post.PublishedAt = parseTime(publishedAt.String)
	}
	if updatedAt.Valid {
		post.UpdatedAt = parseTime(updatedAt.String)
	}
	return post, nil
}

func scanPage(row scanner) (Page, error) {
	var page Page
	var publishedAt, updatedAt sql.NullString
	if err := row.Scan(&page.ID, &page.Title, &page.Slug, &page.Excerpt, &page.Content, &page.Status, &publishedAt, &updatedAt); err != nil {
		return Page{}, err
	}
	if publishedAt.Valid {
		page.PublishedAt = parseTime(publishedAt.String)
	}
	if updatedAt.Valid {
		page.UpdatedAt = parseTime(updatedAt.String)
	}
	return page, nil
}

func scanTerm(row scanner) (Term, error) {
	var term Term
	var createdAt, updatedAt string
	if err := row.Scan(&term.ID, &term.Name, &term.Slug, &term.Kind, &term.PostCount, &createdAt, &updatedAt); err != nil {
		return Term{}, err
	}
	term.CreatedAt = parseTime(createdAt)
	term.UpdatedAt = parseTime(updatedAt)
	return term, nil
}

func scanTermWithPostID(row scanner, postID *string) (Term, error) {
	var term Term
	var createdAt, updatedAt string
	if err := row.Scan(postID, &term.ID, &term.Name, &term.Slug, &term.Kind, &term.PostCount, &createdAt, &updatedAt); err != nil {
		return Term{}, err
	}
	term.CreatedAt = parseTime(createdAt)
	term.UpdatedAt = parseTime(updatedAt)
	return term, nil
}

func scanUser(row scanner) (User, error) {
	var user User
	var active int
	var createdAt, updatedAt string
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Email, &user.Role, &active, &createdAt, &updatedAt); err != nil {
		return User{}, err
	}
	user.Active = active != 0
	user.CreatedAt = parseTime(createdAt)
	user.UpdatedAt = parseTime(updatedAt)
	return user, nil
}

func scanComment(row scanner) (Comment, error) {
	var comment Comment
	var createdAt, updatedAt string
	if err := row.Scan(&comment.ID, &comment.PostID, &comment.PostTitle, &comment.PostSlug, &comment.AuthorName, &comment.AuthorEmail, &comment.AuthorURL, &comment.Content, &comment.Status, &comment.ParentID, &createdAt, &updatedAt); err != nil {
		return Comment{}, err
	}
	comment.CreatedAt = parseTime(createdAt)
	comment.UpdatedAt = parseTime(updatedAt)
	return comment, nil
}

func scanMedia(row scanner) (Media, error) {
	var media Media
	var createdAt string
	if err := row.Scan(&media.ID, &media.Filename, &media.ContentType, &media.Size, &media.StorageKey, &createdAt); err != nil {
		return Media{}, err
	}
	media.CreatedAt = parseTime(createdAt)
	return media, nil
}

func scanAuditEvent(row scanner) (AuditEvent, error) {
	var event AuditEvent
	var details, createdAt string
	if err := row.Scan(&event.ID, &event.ActorUserID, &event.ActorUsername, &event.Action, &event.ResourceType, &event.ResourceID, &details, &createdAt); err != nil {
		return AuditEvent{}, err
	}
	if details != "" {
		if err := json.Unmarshal([]byte(details), &event.Details); err != nil {
			return AuditEvent{}, fmt.Errorf("decode audit details: %w", err)
		}
	}
	event.CreatedAt = parseTime(createdAt)
	return event, nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func newID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("post-%d", time.Now().UnixNano())
	}
	return "post-" + hex.EncodeToString(buffer)
}

func newPageID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("page-%d", time.Now().UnixNano())
	}
	return "page-" + hex.EncodeToString(buffer)
}

func newTermID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("term-%d", time.Now().UnixNano())
	}
	return "term-" + hex.EncodeToString(buffer)
}

func newCommentID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("comment-%d", time.Now().UnixNano())
	}
	return "comment-" + hex.EncodeToString(buffer)
}

func newUserID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("user-%d", time.Now().UnixNano())
	}
	return "user-" + hex.EncodeToString(buffer)
}

func newMediaID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("media-%d", time.Now().UnixNano())
	}
	return "media-" + hex.EncodeToString(buffer)
}

func termSlug(name string) string {
	value := strings.ToLower(strings.TrimSpace(name))
	value = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '-' {
			return unicode.ToLower(r)
		}
		return '-'
	}, value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "term"
	}
	return value
}

func containsPattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return `%` + value + `%`
}

func newAuditID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("audit-%d", time.Now().UnixNano())
	}
	return "audit-" + hex.EncodeToString(buffer)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
