package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func (s *Server) listPosts(w http.ResponseWriter, r *http.Request) {
	includeDrafts := r.URL.Query().Get("includeDraft") == "true"
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	if (includeDrafts || statusFilter == "draft") && !s.requireContentEditor(w, r) {
		return
	}
	if statusFilter == "draft" {
		includeDrafts = true
	}
	page, pageSize, err := s.readingPage(r)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceReadError(err)
		writeError(w, status, code, err.Error())
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	posts, err := s.app.ListPostsByTermsContext(r.Context(), r.URL.Query().Get("q"), category, tag, r.URL.Query().Get("month"), r.URL.Query().Get("order"), includeDrafts, statusFilter, page, pageSize)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceReadError(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicPostPage(posts))
}

func (s *Server) listArchive(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.MonthArchiveContext(r.Context())
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceReadError(err)
		writeError(w, status, code, err.Error())
		return
	}
	if items == nil {
		items = []store.MonthCount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listRecentComments(w http.ResponseWriter, r *http.Request) {
	page, pageSize, ok := s.pages(w, r)
	if !ok {
		return
	}
	comments, err := s.app.RecentApprovedCommentsContext(r.Context(), page, pageSize)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read recent comments")
		return
	}
	writeJSON(w, http.StatusOK, publicCommentPage(comments, false))
}

func (s *Server) listPages(w http.ResponseWriter, r *http.Request) {
	includeDrafts := r.URL.Query().Get("includeDraft") == "true"
	if includeDrafts && !s.requireContentEditor(w, r) {
		return
	}
	page, pageSize, ok := s.pages(w, r)
	if !ok {
		return
	}
	pages, err := s.app.ListPagesContext(r.Context(), r.URL.Query().Get("q"), includeDrafts, page, pageSize)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceReadError(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicPagePage(pages))
}

func (s *Server) listTerms(w http.ResponseWriter, r *http.Request) {
	includeDrafts := r.URL.Query().Get("includeDraft") == "true"
	if includeDrafts && !s.requireContentEditor(w, r) {
		return
	}
	page, pageSize, ok := s.pages(w, r)
	if !ok {
		return
	}
	terms, err := s.app.ListTermsContext(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("q"), includeDrafts, page, pageSize)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceReadError(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicTermPage(terms))
}

type termRequest struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func (s *Server) createTerm(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	var input termRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	term, err := s.app.CreateTermContext(r.Context(), service.TermInput{Name: input.Name, Kind: input.Kind})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "term_taken", "已有同名分类或标签")
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	s.recordAudit(r, "term.created", "term", term.ID, map[string]string{"name": term.Name, "kind": term.Kind})
	writeJSON(w, http.StatusCreated, publicTerm(*term))
}

func (s *Server) updateTerm(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	var input termRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	term, changed, err := s.app.UpdateTermWithChangeContext(r.Context(), r.PathValue("id"), service.TermInput{Name: input.Name, Kind: input.Kind})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "term_taken", "已有同名分类或标签")
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	if changed {
		s.recordAudit(r, "term.updated", "term", term.ID, map[string]string{"name": term.Name, "kind": term.Kind})
	}
	writeJSON(w, http.StatusOK, publicTerm(*term))
}

func (s *Server) deleteTerm(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	err := s.app.DeleteTermContext(r.Context(), r.PathValue("id"))
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	s.recordAudit(r, "term.deleted", "term", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getPost(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	canEdit, err := s.contentEditorAccess(r.Context(), r)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		s.writeRepositoryUnavailable(w, r, err)
		return
	}
	post, err := s.app.GetPostContext(r.Context(), slug, canEdit)
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read post")
		return
	}
	previous, next, err := s.app.PublishedNeighborsContext(r.Context(), post)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read neighboring posts")
		return
	}
	withBody := s.app.RevealPost(post, canEdit, r.Header.Get("X-Post-Passphrase"))
	writeJSON(w, http.StatusOK, publicPostWithNeighbors(post, previous, next, withBody))
}

func (s *Server) getPage(w http.ResponseWriter, r *http.Request) {
	canEdit, err := s.contentEditorAccess(r.Context(), r)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		s.writeRepositoryUnavailable(w, r, err)
		return
	}
	page, err := s.app.GetPageContext(r.Context(), r.PathValue("slug"), canEdit)
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Page not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read page")
		return
	}
	writeJSON(w, http.StatusOK, publicPage(page))
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	page, pageSize, ok := s.pages(w, r)
	if !ok {
		return
	}
	includePending := r.URL.Query().Get("includePending") == "true"
	if includePending && !s.requireModerator(w, r) {
		return
	}
	cursor, err := decodeCommentCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "评论分页游标无效")
		return
	}
	comments, err := s.app.ListCommentsWithCursorContext(r.Context(), r.PathValue("slug"), includePending, page, pageSize, cursor)
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read comments")
		return
	}
	response := publicCommentPage(comments, false)
	if comments.HasMore && len(comments.Items) > 0 && (cursor != nil || comments.Page == 1) {
		last := comments.Items[len(comments.Items)-1]
		response.NextCursor, err = encodeCommentCursor(store.CommentCursor{
			SnapshotAt: comments.SnapshotAt,
			BeforeAt:   last.CreatedAt,
			BeforeID:   last.ID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "encode_failed", "无法生成评论分页游标")
			return
		}
	}
	writeJSON(w, http.StatusOK, response)
}

type commentRequest struct {
	AuthorName  string `json:"authorName"`
	AuthorEmail string `json:"authorEmail"`
	AuthorURL   string `json:"authorUrl"`
	Content     string `json:"content"`
	ParentID    string `json:"parentId"`
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	var input commentRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	if !s.allowComment(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "尝试过于频繁，请稍后再试")
		return
	}
	comment, err := s.app.CreateCommentContext(r.Context(), r.PathValue("slug"), service.CommentInput{
		AuthorName:  input.AuthorName,
		AuthorEmail: input.AuthorEmail,
		AuthorURL:   input.AuthorURL,
		Content:     input.Content,
		ParentID:    input.ParentID,
	})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	s.recordAudit(r, "comment.created", "comment", comment.ID, map[string]string{"postId": comment.PostID, "status": comment.Status, "parentId": comment.ParentID})
	if r.Context().Err() != nil {
		return
	}
	statusCode := http.StatusAccepted
	if comment.Status == "approved" {
		statusCode = http.StatusCreated
	}
	writeJSON(w, statusCode, publicComment(comment, false))
}

func (s *Server) listAllComments(w http.ResponseWriter, r *http.Request) {
	if !s.requireModerator(w, r) {
		return
	}
	page, pageSize, ok := s.pages(w, r)
	if !ok {
		return
	}
	comments, err := s.app.ListAllCommentsContext(r.Context(), r.URL.Query().Get("status"), page, pageSize)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceReadError(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicCommentPage(comments, true))
}

type commentStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) updateComment(w http.ResponseWriter, r *http.Request) {
	if !s.requireModerator(w, r) {
		return
	}
	var input commentStatusRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	comment, changed, err := s.app.ModerateCommentContext(r.Context(), r.PathValue("id"), input.Status)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	if changed {
		s.recordAudit(r, "comment.moderated", "comment", comment.ID, map[string]string{"status": input.Status})
	}
	writeJSON(w, http.StatusOK, publicComment(comment, true))
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	if !s.requireModerator(w, r) {
		return
	}
	err := s.app.DeleteCommentContext(r.Context(), r.PathValue("id"))
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	s.recordAudit(r, "comment.deleted", "comment", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMedia(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	page, pageSize, ok := s.pages(w, r)
	if !ok {
		return
	}
	media, err := s.app.ListMediaContext(r.Context(), page, pageSize)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read media")
		return
	}
	writeJSON(w, http.StatusOK, publicMediaPage(media))
}

func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		writeError(w, http.StatusBadRequest, "invalid_request", "Content-Type must be multipart/form-data")
		return
	}
	mediaType, parameters, mediaTypeErr := mime.ParseMediaType(contentTypes[0])
	if mediaTypeErr != nil ||
		!strings.EqualFold(mediaType, "multipart/form-data") ||
		strings.TrimSpace(parameters["boundary"]) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Content-Type must be multipart/form-data")
		return
	}
	if !s.mediaUploadMu.TryLock() {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "media_busy", "Another media upload is already in progress")
		return
	}
	defer s.mediaUploadMu.Unlock()
	if err := os.MkdirAll(s.mediaRoot, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to prepare media storage")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+(1<<20))
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		if r.Context().Err() != nil {
			return
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "Media files must be 10 MiB or smaller")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request", "A multipart file field is required")
		}
		return
	}
	form := r.MultipartForm
	if form == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "A single multipart file field named file is required")
		return
	}
	defer func() {
		if err := form.RemoveAll(); err != nil {
			s.logWarnRateLimited(r.Context(), "media-upload-temp-cleanup", "unable to clean multipart upload temporary files", "error", err)
		}
	}()
	if len(form.File) != 1 || len(form.File["file"]) != 1 || len(form.Value) != 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "A single multipart file field named file is required")
		return
	}
	header := form.File["file"][0]
	file, err := header.Open()
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		s.logErrorRateLimited(r.Context(), "media-upload-part-open", "unable to open uploaded media part", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to open uploaded file")
		return
	}
	defer file.Close()
	filename := cleanMediaFilename(header.Filename)
	if filename == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "The uploaded filename is invalid")
		return
	}

	head := make([]byte, 512)
	n, readErr := file.Read(head)
	if n == 0 {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "The uploaded file is empty")
		return
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "Unable to read the uploaded file")
		return
	}
	contentType := http.DetectContentType(head[:n])
	if !allowedMediaType(contentType) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media", "Only JPEG, PNG, GIF and WebP images are supported")
		return
	}

	temporary, err := os.CreateTemp(s.mediaRoot, ".upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to create media file")
		return
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.logWarnRateLimited(r.Context(), "media-upload-cleanup", "remove temporary media upload failed", "path", temporaryPath, "error", err)
		}
	}()
	written, err := temporary.Write(head[:n])
	if err != nil || written != n {
		_ = temporary.Close()
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to write media file")
		return
	}
	remaining := io.LimitReader(file, int64(maxUploadSize-n)+1)
	copied, err := io.Copy(temporary, remaining)
	totalSize := int64(written) + copied
	if r.Context().Err() != nil {
		_ = temporary.Close()
		return
	}
	if err != nil {
		_ = temporary.Close()
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to write media file")
		return
	}
	if totalSize > maxUploadSize {
		_ = temporary.Close()
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "Media files must be 10 MiB or smaller")
		return
	}
	if err := temporary.Close(); err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to finalize media file")
		return
	}
	if r.Context().Err() != nil {
		return
	}
	imageFile, err := os.Open(temporaryPath)
	if err != nil {
		s.logErrorRateLimited(r.Context(), "media-upload-validate-open", "unable to open uploaded media for validation", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to read uploaded image")
		return
	}
	imageErr := validateUploadedImage(imageFile, contentType)
	closeErr := imageFile.Close()
	if r.Context().Err() != nil {
		return
	}
	if errors.Is(imageErr, errMediaImageRead) {
		s.logErrorRateLimited(r.Context(), "media-upload-validate-read", "unable to read uploaded media for validation", "error", imageErr)
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to read uploaded image")
		return
	}
	if imageErr != nil {
		if errors.Is(imageErr, errMediaImageDimensions) {
			writeError(w, http.StatusUnprocessableEntity, "image_dimensions_exceeded", "Image dimensions exceed the supported limit")
		} else {
			writeError(w, http.StatusUnprocessableEntity, "invalid_image", "The uploaded file is not a valid image")
		}
		return
	}
	if closeErr != nil {
		s.logErrorRateLimited(r.Context(), "media-upload-validate-close", "unable to close uploaded media after validation", "error", closeErr)
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to read uploaded image")
		return
	}

	id := newMediaID()
	storageKey := id + mediaExtension(contentType)
	finalPath := filepath.Join(s.mediaRoot, storageKey)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "Unable to finalize media file")
		return
	}
	if r.Context().Err() != nil {
		if err := os.Remove(finalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.logWarnRateLimited(r.Context(), "media-upload-cleanup", "remove unregistered media upload failed", "path", finalPath, "error", err)
		}
		return
	}
	media, err := s.app.RegisterMediaContext(r.Context(), store.Media{ID: id, Filename: filename, ContentType: contentType, Size: totalSize, StorageKey: storageKey})
	if err != nil {
		if cleanupErr := os.Remove(finalPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			s.logWarnRateLimited(r.Context(), "media-upload-cleanup", "remove unregistered media upload failed", "path", finalPath, "error", cleanupErr)
		}
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	s.recordAudit(r, "media.uploaded", "media", media.ID, map[string]string{"filename": media.Filename, "contentType": media.ContentType})
	if r.Context().Err() != nil {
		return
	}
	writeJSON(w, http.StatusCreated, publicMedia(media))
}

func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	s.mediaDeleteMu.Lock()
	defer s.mediaDeleteMu.Unlock()
	if r.Context().Err() != nil {
		return
	}
	mediaID := r.PathValue("id")
	media, err := s.app.GetMediaContext(r.Context(), r.PathValue("id"))
	if r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		if cleanupErr := removeStagedMedia(mediaDeleteStagingPath(s.mediaRoot, mediaID)); cleanupErr != nil {
			s.logWarnRateLimited(r.Context(), "media-delete-stage-cleanup", "unable to remove staged file for deleted media", "media_id", mediaID, "error", cleanupErr)
		}
		if r.Context().Err() != nil {
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read media")
		return
	}
	originalPath, stagedPath, staged, err := stageMediaFile(s.mediaRoot, media.ID, media.StorageKey)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "delete_failed", "Unable to stage media file for deletion")
		return
	}
	if r.Context().Err() != nil {
		if staged {
			if restoreErr := restoreStagedMedia(originalPath, stagedPath); restoreErr != nil {
				s.logErrorRateLimited(r.Context(), "media-delete-stage-restore", "unable to restore media file after canceled deletion", "media_id", media.ID, "error", restoreErr)
			}
		}
		return
	}
	// Complete the database delete after this boundary so cancellation cannot leave its staged file behind.
	err = s.app.DeleteMedia(media.ID)
	if errors.Is(err, store.ErrNotFound) {
		if staged {
			if cleanupErr := removeStagedMedia(stagedPath); cleanupErr != nil {
				s.logWarnRateLimited(r.Context(), "media-delete-stage-cleanup", "unable to remove staged file for deleted media", "media_id", media.ID, "error", cleanupErr)
			}
		}
		if r.Context().Err() != nil {
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		if staged {
			if restoreErr := restoreStagedMedia(originalPath, stagedPath); restoreErr != nil {
				s.logErrorRateLimited(r.Context(), "media-delete-stage-restore", "unable to restore media file after database deletion failed", "media_id", media.ID, "error", restoreErr)
			}
		}
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	s.recordAudit(r, "media.deleted", "media", media.ID, map[string]string{"filename": media.Filename})
	if staged {
		if cleanupErr := removeStagedMedia(stagedPath); cleanupErr != nil {
			s.logWarnRateLimited(r.Context(), "media-delete-stage-cleanup", "unable to remove staged file for deleted media", "media_id", media.ID, "error", cleanupErr)
		}
	}
	if r.Context().Err() != nil {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request) {
	media, file, err := s.openMediaForServing(r.Context(), r.PathValue("id"))
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read media")
		return
	}
	if file == nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", media.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, media.Filename, media.CreatedAt, file)
}

func (s *Server) openMediaForServing(ctx context.Context, mediaID string) (store.Media, *os.File, error) {
	s.mediaDeleteMu.Lock()
	defer s.mediaDeleteMu.Unlock()

	media, err := s.app.GetMediaContext(ctx, mediaID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
			if cleanupErr := removeStagedMedia(mediaDeleteStagingPath(s.mediaRoot, mediaID)); cleanupErr != nil {
				s.logWarnRateLimited(ctx, "media-read-stage-cleanup", "unable to remove staged file for deleted media", "media_id", mediaID, "error", cleanupErr)
			}
		}
		return store.Media{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return store.Media{}, nil, err
	}
	if !allowedMediaType(media.ContentType) {
		s.logWarnRateLimited(ctx, "media-read-content-type", "refusing media with unsupported content type", "media_id", media.ID, "content_type", media.ContentType)
		return media, nil, errors.New("media content type is unsupported")
	}
	if !validMediaStorageKey(media.StorageKey) {
		return media, nil, nil
	}
	if err := restoreStagedMediaIfMissing(s.mediaRoot, media.ID, media.StorageKey); err != nil {
		s.logErrorRateLimited(ctx, "media-read-stage-restore", "unable to restore staged media file before serving", "media_id", media.ID, "error", err)
		return media, nil, err
	}
	if err := ctx.Err(); err != nil {
		return media, nil, err
	}
	file, _, err := openRegularFile(filepath.Join(s.mediaRoot, media.StorageKey))
	return media, file, err
}

func (s *Server) createPost(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	var input postRequest
	if err := decodeContentJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	post, err := input.post()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	created, err := s.app.CreatePostContext(r.Context(), applyAllowComment(post, input, true))
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "post_taken", "已有使用该固定链接的文章")
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	s.recordAudit(r, "post.created", "post", created.ID, map[string]string{"status": created.Status})
	writeJSON(w, http.StatusCreated, publicPost(created, true))
}

func (s *Server) updatePost(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	post, err := s.app.GetPostContext(r.Context(), r.PathValue("id"), true)
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", err.Error())
		return
	}
	var input postRequest
	if err := decodeContentJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	if input.ExpectedUpdatedAt == nil || input.ExpectedUpdatedAt.IsZero() {
		writeError(w, http.StatusPreconditionRequired, "precondition_required", "更新内容必须提交 expectedUpdatedAt")
		return
	}
	nextPost, err := input.post()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	nextPost.ExpectedUpdatedAt = *input.ExpectedUpdatedAt
	updated, changed, err := s.app.UpdatePostWithChangeContext(r.Context(), post.ID, applyAllowComment(nextPost, input, post.AllowComment))
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "post_taken", "已有使用该固定链接的文章")
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	if changed {
		s.recordAudit(r, "post.updated", "post", updated.ID, map[string]string{"status": updated.Status})
	}
	writeJSON(w, http.StatusOK, publicPost(updated, true))
}

func (s *Server) deletePost(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	err := s.app.DeletePostContext(r.Context(), r.PathValue("id"))
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	s.recordAudit(r, "post.deleted", "post", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	var input pageRequest
	if err := decodeContentJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	created, err := s.app.CreatePageContext(r.Context(), input.page())
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "page_taken", "已有使用该固定链接的页面")
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	s.recordAudit(r, "page.created", "page", created.ID, map[string]string{"status": created.Status})
	writeJSON(w, http.StatusCreated, publicPage(created))
}

func (s *Server) updatePage(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	page, err := s.app.GetPageContext(r.Context(), r.PathValue("id"), true)
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Page not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", err.Error())
		return
	}
	var input pageRequest
	if err := decodeContentJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	if input.ExpectedUpdatedAt == nil || input.ExpectedUpdatedAt.IsZero() {
		writeError(w, http.StatusPreconditionRequired, "precondition_required", "更新内容必须提交 expectedUpdatedAt")
		return
	}
	nextPage := input.page()
	nextPage.ExpectedUpdatedAt = *input.ExpectedUpdatedAt
	updated, changed, err := s.app.UpdatePageWithChangeContext(r.Context(), page.ID, nextPage)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "page_taken", "已有使用该固定链接的页面")
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	if changed {
		s.recordAudit(r, "page.updated", "page", updated.ID, map[string]string{"status": updated.Status})
	}
	writeJSON(w, http.StatusOK, publicPage(updated))
}

func (s *Server) deletePage(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	err := s.app.DeletePageContext(r.Context(), r.PathValue("id"))
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	s.recordAudit(r, "page.deleted", "page", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}
