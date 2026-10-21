package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func openSite() store.Site {
	return store.Site{CommentsEnabled: true, CommentsRequireModeration: true, CommentsThreaded: true, PostsPerPage: 10}
}

func openPost() store.Post {
	return store.Post{ID: "post-1", AllowComment: true}
}

func TestBuildCommentUsesReadingPolicy(t *testing.T) {
	parent := store.Comment{ID: "comment-1", PostID: "post-1", Status: "approved"}
	tests := []struct {
		name    string
		site    store.Site
		post    store.Post
		input   CommentInput
		parent  *store.Comment
		depth   int
		status  string
		wantErr string
	}{
		{name: "moderation", site: openSite(), post: openPost(), input: CommentInput{AuthorName: "Ada", Content: "hello"}, status: "pending"},
		{name: "auto approve", site: store.Site{CommentsEnabled: true, CommentsThreaded: true}, post: openPost(), input: CommentInput{AuthorName: "Ada", Content: "hello"}, status: "approved"},
		{name: "closed site", site: store.Site{CommentsThreaded: true}, post: openPost(), input: CommentInput{AuthorName: "Ada", Content: "hello"}, wantErr: "评论已关闭"},
		{name: "closed post", site: openSite(), post: store.Post{ID: "post-1"}, input: CommentInput{AuthorName: "Ada", Content: "hello"}, wantErr: "评论已关闭"},
		{name: "reply", site: openSite(), post: openPost(), input: CommentInput{AuthorName: "Ada", AuthorURL: "https://example.com", Content: "reply"}, parent: &parent, status: "pending"},
		{name: "replies disabled", site: store.Site{CommentsEnabled: true, CommentsRequireModeration: true}, post: openPost(), input: CommentInput{AuthorName: "Ada", Content: "reply"}, parent: &parent, wantErr: "已关闭回复"},
		{name: "pending parent", site: openSite(), post: openPost(), input: CommentInput{AuthorName: "Ada", Content: "reply"}, parent: &store.Comment{ID: "comment-2", PostID: "post-1", Status: "pending"}, wantErr: "已通过的评论"},
		{name: "other post", site: openSite(), post: openPost(), input: CommentInput{AuthorName: "Ada", Content: "reply"}, parent: &store.Comment{ID: "comment-3", PostID: "post-2", Status: "approved"}, wantErr: "同一篇文章"},
		{name: "too deep", site: openSite(), post: openPost(), input: CommentInput{AuthorName: "Ada", Content: "reply"}, parent: &parent, depth: 2, wantErr: "回复层级过多"},
		{name: "bad url", site: openSite(), post: openPost(), input: CommentInput{AuthorName: "Ada", AuthorURL: "javascript:alert(1)", Content: "hello"}, wantErr: "网址"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			comment, err := buildComment(test.site, test.post, test.input, test.parent, test.depth)
			if test.wantErr != "" {
				if err == nil || !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("expected validation error containing %q, got %v", test.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if comment.Status != test.status {
				t.Fatalf("status = %s, want %s", comment.Status, test.status)
			}
			if test.parent != nil && comment.ParentID != test.parent.ID {
				t.Fatalf("parent = %s, want %s", comment.ParentID, test.parent.ID)
			}
		})
	}
}

func TestCommentDepthFrom(t *testing.T) {
	comments := map[string]store.Comment{
		"a": {ID: "a"},
		"b": {ID: "b", ParentID: "a"},
		"c": {ID: "c", ParentID: "b"},
	}
	lookup := func(id string) (store.Comment, error) {
		comment, ok := comments[id]
		if !ok {
			return store.Comment{}, store.ErrNotFound
		}
		return comment, nil
	}
	depth, err := commentDepthFrom(comments["c"], lookup)
	if err != nil || depth != 2 {
		t.Fatalf("depth = %d, err = %v", depth, err)
	}
	comments["a"] = store.Comment{ID: "a", ParentID: "c"}
	if _, err := commentDepthFrom(comments["c"], lookup); err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("expected invalid thread, got %v", err)
	}
}
