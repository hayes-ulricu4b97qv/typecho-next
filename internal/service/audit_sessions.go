package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func (s *Service) RecordAudit(event store.AuditEvent) error {
	return s.repo.RecordAudit(event)
}

func (s *Service) ListAudit(action, resourceType string, page, pageSize int) (store.AuditPage, error) {
	return s.ListAuditContext(context.Background(), action, resourceType, page, pageSize)
}

func (s *Service) ListAuditContext(ctx context.Context, action, resourceType string, page, pageSize int) (store.AuditPage, error) {
	return s.repo.ListAuditContext(ctx, store.AuditQuery{Action: action, ResourceType: resourceType, Page: page, PageSize: pageSize})
}

func (s *Service) Stats() (store.PostStats, error) {
	return s.repo.Stats()
}

func (s *Service) StatsContext(ctx context.Context) (store.PostStats, error) {
	return s.repo.StatsContext(ctx)
}

func (s *Service) CreateSession(username, token string, expiresAt time.Time) error {
	return s.CreateSessionContext(context.Background(), username, token, expiresAt)
}

func (s *Service) CreateSessionContext(ctx context.Context, username, token string, expiresAt time.Time) error {
	err := s.repo.CreateSessionContext(ctx, username, hashToken(token), expiresAt)
	if err == nil || errors.Is(err, store.ErrNotFound) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: create session: %w", ErrRepositoryUnavailable, err)
}

func (s *Service) CreateAuthenticatedSessionContext(ctx context.Context, username, passwordHash, token string, expiresAt time.Time) error {
	err := s.repo.CreateAuthenticatedSessionContext(ctx, username, passwordHash, hashToken(token), expiresAt)
	if err == nil || errors.Is(err, store.ErrNotFound) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: create authenticated session: %w", ErrRepositoryUnavailable, err)
}

func (s *Service) ResolveSession(token string, now time.Time) (string, error) {
	return s.repo.ResolveSession(hashToken(token), now)
}

func (s *Service) ResolveSessionContext(ctx context.Context, token string, now time.Time) (string, error) {
	return s.repo.ResolveSessionContext(ctx, hashToken(token), now)
}

func (s *Service) RevokeSession(token string) error {
	return s.repo.DeleteSession(hashToken(token))
}

func (s *Service) RevokeUserSessions(username string) error {
	return s.repo.DeleteUserSessions(username)
}

func (s *Service) RevokeOtherSessions(username, token string) error {
	if token == "" {
		return s.repo.DeleteUserSessions(username)
	}
	return s.repo.DeleteOtherUserSessions(username, hashToken(token))
}

func (s *Service) PurgeSessions(now time.Time) error {
	return s.repo.PurgeSessions(now)
}

func (s *Service) PurgeSessionsContext(ctx context.Context, now time.Time) error {
	return s.repo.PurgeSessionsContext(ctx, now)
}
