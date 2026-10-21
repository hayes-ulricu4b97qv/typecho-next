package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const authenticationTimingHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

func (s *Service) Initialize(input SetupInput) (*store.Site, error) {
	return s.InitializeContext(context.Background(), input)
}

func (s *Service) InitializeContext(ctx context.Context, input SetupInput) (*store.Site, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSetup(input); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Timezone) == "" {
		input.Timezone = "Asia/Shanghai"
	}
	input.Timezone = strings.TrimSpace(input.Timezone)
	if _, err := loadSiteLocation(input.Timezone); err != nil {
		return nil, fmt.Errorf("%w: 时区必须是有效的 IANA 名称", ErrValidation)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash administrator password: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	site := store.Site{
		Name:      strings.TrimSpace(input.SiteName),
		URL:       strings.TrimRight(strings.TrimSpace(input.SiteURL), "/"),
		AdminMail: strings.TrimSpace(input.AdminEmail),
		Timezone:  input.Timezone,
	}
	admin := store.Admin{
		Username:     input.AdminUsername,
		PasswordHash: string(hash),
		Email:        strings.TrimSpace(input.AdminEmail),
	}
	if err := s.repo.SetupContext(ctx, site, admin); err != nil {
		if errors.Is(err, store.ErrAlreadyInitialized) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: initialize site: %w", ErrRepositoryUnavailable, err)
	}
	return &site, nil
}

func (s *Service) Authenticate(username, password string) (*store.Admin, error) {
	return s.AuthenticateContext(context.Background(), username, password)
}

func (s *Service) AuthenticateContext(ctx context.Context, username, password string) (*store.Admin, error) {
	user, err := s.repo.UserContext(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// Keep unknown-user failures close to wrong-password failures without
		// allowing the dummy hash to authenticate any account.
		_ = bcrypt.CompareHashAndPassword([]byte(authenticationTimingHash), []byte(password))
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read user for authentication: %w", ErrRepositoryUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	passwordErr := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if user.Username != username || !user.Active || passwordErr != nil {
		return nil, ErrInvalidCredentials
	}
	return &user, nil
}

func (s *Service) Admin() (*store.Admin, error) {
	admin, err := s.repo.Admin()
	if err != nil {
		return nil, err
	}
	admin.Role = "owner"
	admin.Active = true
	return admin, nil
}

func (s *Service) User(username string) (*store.User, error) {
	user, err := s.repo.User(username)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Service) UserContext(ctx context.Context, username string) (*store.User, error) {
	user, err := s.repo.UserContext(ctx, username)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Service) UpdateAdminAccount(input AccountInput) (*store.Admin, error) {
	admin, _, _, err := s.UpdateAdminAccountWithChange(input)
	return admin, err
}

func (s *Service) UpdateAdminAccountWithChange(input AccountInput) (*store.Admin, bool, bool, error) {
	return s.UpdateAdminAccountWithChangeContext(context.Background(), input)
}

func (s *Service) UpdateAdminAccountWithChangeContext(ctx context.Context, input AccountInput) (*store.Admin, bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, false, err
	}
	admin, err := s.repo.AdminContext(ctx)
	if err != nil {
		return nil, false, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, false, err
	}

	email := strings.TrimSpace(input.Email)
	if email == "" {
		email = admin.Email
	}
	if !validEmail(email) {
		return nil, false, false, fmt.Errorf("%w: 邮箱格式不正确", ErrValidation)
	}

	passwordHash := admin.PasswordHash
	passwordChanged := false
	if input.NewPassword != "" {
		if err := ctx.Err(); err != nil {
			return nil, false, false, err
		}
		currentPasswordErr := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(input.CurrentPassword))
		if err := ctx.Err(); err != nil {
			return nil, false, false, err
		}
		if currentPasswordErr != nil {
			return nil, false, false, ErrInvalidCredentials
		}
		if len(input.NewPassword) < 8 {
			return nil, false, false, fmt.Errorf("%w: 新密码至少 8 个字符", ErrValidation)
		}
		if err := validateBcryptInput(input.NewPassword, "新密码"); err != nil {
			return nil, false, false, err
		}
		newPasswordErr := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(input.NewPassword))
		if err := ctx.Err(); err != nil {
			return nil, false, false, err
		}
		if newPasswordErr != nil {
			passwordHashBytes, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
			if err != nil {
				return nil, false, false, fmt.Errorf("hash administrator password: %w", err)
			}
			passwordHash = string(passwordHashBytes)
			passwordChanged = true
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, false, err
	}

	changed := admin.Email != email || passwordChanged
	if !changed {
		admin.Role = "owner"
		admin.Active = true
		return admin, false, false, nil
	}
	admin.Email = email
	admin.PasswordHash = passwordHash
	if passwordChanged {
		if err := s.repo.UpdateAdminAndRevokeOtherSessionsContext(ctx, *admin, hashToken(input.SessionToken)); err != nil {
			return nil, false, false, err
		}
	} else if err := s.repo.UpdateAdminContext(ctx, *admin); err != nil {
		return nil, false, false, err
	}
	admin.Role = "owner"
	admin.Active = true
	return admin, true, passwordChanged, nil
}

func (s *Service) ListUsers() ([]store.User, error) {
	return s.ListUsersContext(context.Background())
}

func (s *Service) ListUsersContext(ctx context.Context) ([]store.User, error) {
	return s.repo.UsersContext(ctx)
}

func (s *Service) CreateUser(input UserInput) (*store.User, error) {
	return s.CreateUserContext(context.Background(), input)
}

func (s *Service) CreateUserContext(ctx context.Context, input UserInput) (*store.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.Role == "" {
		input.Role = "editor"
	}
	if err := validateUserInput(input, true); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash user password: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	user, err := s.repo.CreateUserContext(ctx, store.User{Username: input.Username, Email: strings.TrimSpace(input.Email), PasswordHash: string(hash), Role: input.Role, Active: true})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Service) UpdateUser(username string, input UserInput) (*store.User, error) {
	user, _, err := s.UpdateUserWithChange(username, input)
	return user, err
}

func (s *Service) UpdateUserWithChange(username string, input UserInput) (*store.User, bool, error) {
	return s.UpdateUserWithChangeContext(context.Background(), username, input)
}

func (s *Service) UpdateUserWithChangeContext(ctx context.Context, username string, input UserInput) (*store.User, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	user, err := s.repo.UserContext(ctx, username)
	if err != nil {
		return nil, false, err
	}
	if user.Role == "owner" {
		return nil, false, fmt.Errorf("%w: 所有者账户请在账户设置中修改", ErrValidation)
	}
	current := user
	if input.Email != "" {
		if !validEmail(input.Email) {
			return nil, false, fmt.Errorf("%w: 邮箱格式不正确", ErrValidation)
		}
		user.Email = strings.TrimSpace(input.Email)
	}
	if input.Role != "" {
		if !validStaffRole(input.Role) {
			return nil, false, fmt.Errorf("%w: 角色只能是编辑或审核员", ErrValidation)
		}
		user.Role = input.Role
	}
	if input.Active != nil {
		user.Active = *input.Active
	}
	if input.Password != "" {
		if len(input.Password) < 8 {
			return nil, false, fmt.Errorf("%w: 密码至少 8 个字符", ErrValidation)
		}
		if err := validateBcryptInput(input.Password, "密码"); err != nil {
			return nil, false, err
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		passwordErr := bcrypt.CompareHashAndPassword([]byte(current.PasswordHash), []byte(input.Password))
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if passwordErr != nil {
			hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
			if err != nil {
				return nil, false, fmt.Errorf("hash user password: %w", err)
			}
			user.PasswordHash = string(hash)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	passwordChanged := user.PasswordHash != current.PasswordHash
	changed := user.Email != current.Email || user.Role != current.Role || user.Active != current.Active || passwordChanged
	if !changed {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		return &current, false, nil
	}
	var updated store.User
	if passwordChanged || current.Active != user.Active {
		updated, err = s.repo.UpdateUserAndRevokeSessionsContext(ctx, user)
	} else {
		updated, err = s.repo.UpdateUserContext(ctx, user)
	}
	if err != nil {
		return nil, false, err
	}
	return &updated, true, nil
}

func (s *Service) DeleteUser(username string) error {
	return s.DeleteUserContext(context.Background(), username)
}

func (s *Service) DeleteUserContext(ctx context.Context, username string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	user, err := s.repo.UserContext(ctx, username)
	if err != nil {
		return err
	}
	if user.Role == "owner" {
		return fmt.Errorf("%w: 不能删除所有者账户", ErrValidation)
	}
	err = s.repo.DeleteUserAndRevokeSessionsContext(ctx, username)
	if errors.Is(err, store.ErrProtectedAccount) {
		return fmt.Errorf("%w: 不能删除所有者账户", ErrValidation)
	}
	return err
}

func (s *Service) Site() (*store.Site, error) {
	return s.repo.Site()
}

func (s *Service) SiteContext(ctx context.Context) (*store.Site, error) {
	return s.repo.SiteContext(ctx)
}

func (s *Service) UpdateSite(site store.Site) (*store.Site, error) {
	updated, _, err := s.UpdateSiteWithChange(site)
	return updated, err
}

func (s *Service) UpdateSiteWithChange(site store.Site) (*store.Site, bool, error) {
	return s.UpdateSiteWithChangeContext(context.Background(), site)
}

func (s *Service) UpdateSiteWithChangeContext(ctx context.Context, site store.Site) (*store.Site, bool, error) {
	if strings.TrimSpace(site.Name) == "" || !validSiteURL(site.URL) || !validEmail(site.AdminMail) {
		return nil, false, fmt.Errorf("%w: 需要站点名称、不超过 512 字节且只含协议和域名的站点地址，以及有效的管理员邮箱", ErrValidation)
	}
	current, err := s.repo.SiteContext(ctx)
	if err != nil {
		return nil, false, err
	}
	original := *current
	description := strings.TrimSpace(site.Description)
	keywords := strings.TrimSpace(site.Keywords)
	if runeLength(description) > 2000 {
		return nil, false, fmt.Errorf("%w: 描述最多 2000 个字符", ErrValidation)
	}
	if runeLength(keywords) > 500 {
		return nil, false, fmt.Errorf("%w: 关键词最多 500 个字符", ErrValidation)
	}
	current.Name = strings.TrimSpace(site.Name)
	current.URL = strings.TrimRight(strings.TrimSpace(site.URL), "/")
	current.AdminMail = strings.TrimSpace(site.AdminMail)
	current.Description = description
	current.Keywords = keywords
	if timezone := strings.TrimSpace(site.Timezone); timezone != "" {
		if _, err := loadSiteLocation(timezone); err != nil {
			return nil, false, fmt.Errorf("%w: 时区必须是有效的 IANA 名称", ErrValidation)
		}
		current.Timezone = timezone
	}
	if sameSiteSettings(original, *current) {
		return current, false, nil
	}
	if err := s.repo.UpdateSiteContext(ctx, *current); err != nil {
		return nil, false, err
	}
	return current, true, nil
}

func sameSiteSettings(left, right store.Site) bool {
	return left.Name == right.Name &&
		left.URL == right.URL &&
		left.AdminMail == right.AdminMail &&
		left.Timezone == right.Timezone &&
		left.Description == right.Description &&
		left.Keywords == right.Keywords &&
		left.PostsPerPage == right.PostsPerPage &&
		left.CommentsEnabled == right.CommentsEnabled &&
		left.CommentsRequireModeration == right.CommentsRequireModeration &&
		left.CommentsThreaded == right.CommentsThreaded
}
