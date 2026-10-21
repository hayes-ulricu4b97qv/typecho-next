package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func (s *Service) ListComments(postKey string, includePending bool, page, pageSize int) (store.CommentPage, error) {
	return s.ListCommentsContext(context.Background(), postKey, includePending, page, pageSize)
}

func (s *Service) ListCommentsContext(ctx context.Context, postKey string, includePending bool, page, pageSize int) (store.CommentPage, error) {
	return s.ListCommentsWithCursorContext(ctx, postKey, includePending, page, pageSize, nil)
}

func (s *Service) ListCommentsWithCursorContext(ctx context.Context, postKey string, includePending bool, page, pageSize int, cursor *store.CommentCursor) (store.CommentPage, error) {
	post, err := s.GetPostContext(ctx, postKey, includePending)
	if err != nil {
		return store.CommentPage{}, err
	}
	return s.repo.ListCommentsContext(ctx, store.CommentQuery{
		PostID:         post.ID,
		IncludePending: includePending,
		Page:           page,
		PageSize:       pageSize,
		Cursor:         cursor,
	})
}

func (s *Service) RecentApprovedComments(page, pageSize int) (store.CommentPage, error) {
	return s.RecentApprovedCommentsContext(context.Background(), page, pageSize)
}

func (s *Service) RecentApprovedCommentsContext(ctx context.Context, page, pageSize int) (store.CommentPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}
	return s.repo.ListCommentsContext(ctx, store.CommentQuery{
		Status:             "approved",
		PublishedPostsOnly: true,
		Page:               page,
		PageSize:           pageSize,
	})
}

func (s *Service) ListAllComments(status string, page, pageSize int) (store.CommentPage, error) {
	return s.ListAllCommentsContext(context.Background(), status, page, pageSize)
}

func (s *Service) ListAllCommentsContext(ctx context.Context, status string, page, pageSize int) (store.CommentPage, error) {
	if status != "" && !validCommentStatus(status) {
		return store.CommentPage{}, fmt.Errorf("%w: 评论状态只能是待审核、已通过或垃圾", ErrValidation)
	}
	return s.repo.ListCommentsContext(ctx, store.CommentQuery{Status: status, IncludePending: true, Page: page, PageSize: pageSize})
}

func (s *Service) CreateComment(postKey string, input CommentInput) (store.Comment, error) {
	return s.CreateCommentContext(context.Background(), postKey, input)
}

func (s *Service) CreateCommentContext(ctx context.Context, postKey string, input CommentInput) (store.Comment, error) {
	post, err := s.GetPostContext(ctx, postKey, false)
	if err != nil {
		return store.Comment{}, err
	}
	site, err := s.repo.SiteContext(ctx)
	if err != nil {
		return store.Comment{}, err
	}
	var parent *store.Comment
	parentDepth := 0
	if parentID := strings.TrimSpace(input.ParentID); parentID != "" {
		found, err := s.repo.CommentContext(ctx, parentID)
		if errors.Is(err, store.ErrNotFound) {
			return store.Comment{}, fmt.Errorf("%w: 未找到要回复的评论", ErrValidation)
		}
		if err != nil {
			return store.Comment{}, err
		}
		parent = &found
		parentDepth, err = s.commentDepthContext(ctx, found)
		if err != nil {
			return store.Comment{}, err
		}
	}
	comment, err := buildComment(*site, post, input, parent, parentDepth)
	if err != nil {
		return store.Comment{}, err
	}
	saved, err := s.repo.SaveCommentContext(ctx, comment)
	if err != nil {
		return store.Comment{}, err
	}
	if saved.Status == "pending" {
		body := fmt.Sprintf("文章：%s\n作者：%s\n内容：%s\n", post.Title, saved.AuthorName, saved.Content)
		s.notifyOwner(ctx, site, "新评论待审核", body)
	}
	return saved, nil
}

func (s *Service) ModerateComment(id, status string) (store.Comment, bool, error) {
	return s.ModerateCommentContext(context.Background(), id, status)
}

func (s *Service) ModerateCommentContext(ctx context.Context, id, status string) (store.Comment, bool, error) {
	if err := ctx.Err(); err != nil {
		return store.Comment{}, false, err
	}
	if !validCommentStatus(status) {
		return store.Comment{}, false, fmt.Errorf("%w: 评论状态只能是待审核、已通过或垃圾", ErrValidation)
	}
	return s.repo.UpdateCommentStatusContext(ctx, id, status)
}

func (s *Service) DeleteComment(id string) error {
	return s.DeleteCommentContext(context.Background(), id)
}

func (s *Service) DeleteCommentContext(ctx context.Context, id string) error {
	return s.repo.DeleteCommentContext(ctx, id)
}

func (s *Service) ListMedia(page, pageSize int) (store.MediaPage, error) {
	return s.ListMediaContext(context.Background(), page, pageSize)
}

func (s *Service) ListMediaContext(ctx context.Context, page, pageSize int) (store.MediaPage, error) {
	return s.repo.ListMediaContext(ctx, store.MediaQuery{Page: page, PageSize: pageSize})
}

func (s *Service) GetMedia(id string) (store.Media, error) {
	return s.repo.Media(id)
}

func (s *Service) GetMediaContext(ctx context.Context, id string) (store.Media, error) {
	return s.repo.MediaContext(ctx, id)
}

func (s *Service) RegisterMedia(media store.Media) (store.Media, error) {
	return s.RegisterMediaContext(context.Background(), media)
}

func (s *Service) RegisterMediaContext(ctx context.Context, media store.Media) (store.Media, error) {
	media.Filename = strings.TrimSpace(media.Filename)
	media.ContentType = strings.TrimSpace(media.ContentType)
	if media.Filename == "" || media.ContentType == "" || media.Size < 1 || media.StorageKey == "" {
		return store.Media{}, fmt.Errorf("%w: 媒体信息不完整", ErrValidation)
	}
	return s.repo.SaveMediaContext(ctx, media)
}

func (s *Service) DeleteMedia(id string) error {
	return s.repo.DeleteMedia(id)
}
