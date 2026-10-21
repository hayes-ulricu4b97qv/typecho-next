package service

import (
	"testing"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestVisibleToPublicUsesFullTimestampPrecision(t *testing.T) {
	now := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)
	post := store.Post{Status: "published", PublishedAt: now.Add(time.Nanosecond)}
	if visibleToPublic(post, now) {
		t.Fatal("post scheduled one nanosecond after now is visible")
	}
	post.PublishedAt = now
	if !visibleToPublic(post, now) {
		t.Fatal("post published exactly now is not visible")
	}
	post.PublishedAt = now.Add(-time.Nanosecond)
	if !visibleToPublic(post, now) {
		t.Fatal("post published before now is not visible")
	}
	post.Status = "draft"
	if visibleToPublic(post, now) {
		t.Fatal("draft is visible to the public")
	}
}

func TestVisiblePageToPublicUsesFullTimestampPrecision(t *testing.T) {
	now := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)
	page := store.Page{Status: "published", PublishedAt: now.Add(time.Nanosecond)}
	if visiblePageToPublic(page, now) {
		t.Fatal("page scheduled one nanosecond after now is visible")
	}
	page.PublishedAt = now
	if !visiblePageToPublic(page, now) {
		t.Fatal("page published exactly now is not visible")
	}
	page.PublishedAt = now.Add(-time.Nanosecond)
	if !visiblePageToPublic(page, now) {
		t.Fatal("page published before now is not visible")
	}
	page.Status = "draft"
	if visiblePageToPublic(page, now) {
		t.Fatal("draft page is visible to the public")
	}
	page.Status = "published"
	page.PublishedAt = time.Time{}
	if visiblePageToPublic(page, now) {
		t.Fatal("published page without a publication time is visible")
	}
}
