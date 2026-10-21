package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (r *SQLiteRepository) Initialized() bool {
	initialized, err := r.InitializedContext(context.Background())
	return err == nil && initialized
}

func (r *SQLiteRepository) InitializedContext(ctx context.Context) (bool, error) {
	var initialized bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sites WHERE id = 1) AND EXISTS(SELECT 1 FROM users WHERE role = 'owner' AND active = 1)`).Scan(&initialized)
	if err != nil {
		return false, fmt.Errorf("check repository initialization: %w", err)
	}
	return initialized, nil
}

func (r *SQLiteRepository) Site() (*Site, error) {
	return r.SiteContext(context.Background())
}

func (r *SQLiteRepository) SiteContext(ctx context.Context) (*Site, error) {
	var site Site
	var createdAt, updatedAt string
	var postsPerPage, commentsEnabled, commentsRequireModeration, commentsThreaded int
	err := r.db.QueryRowContext(ctx, `SELECT name, url, admin_mail, timezone, description, keywords, posts_per_page, comments_enabled, comments_require_moderation, comments_threaded, created_at, updated_at FROM sites WHERE id = 1`).Scan(&site.Name, &site.URL, &site.AdminMail, &site.Timezone, &site.Description, &site.Keywords, &postsPerPage, &commentsEnabled, &commentsRequireModeration, &commentsThreaded, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read site: %w", err)
	}
	site.PostsPerPage = postsPerPage
	site.CommentsEnabled = commentsEnabled != 0
	site.CommentsRequireModeration = commentsRequireModeration != 0
	site.CommentsThreaded = commentsThreaded != 0
	if site.PostsPerPage < 1 || site.PostsPerPage > 50 {
		site.PostsPerPage = 10
	}
	site.CreatedAt = parseTime(createdAt)
	site.UpdatedAt = parseTime(updatedAt)
	return &site, nil
}

func (r *SQLiteRepository) Admin() (*Admin, error) {
	return r.AdminContext(context.Background())
}

func (r *SQLiteRepository) AdminContext(ctx context.Context) (*Admin, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, username, password_hash, email, role, active, created_at, updated_at FROM users WHERE role = 'owner' ORDER BY created_at ASC, id ASC LIMIT 1`)
	admin, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read owner: %w", err)
	}
	return &admin, nil
}

func (r *SQLiteRepository) UpdateAdmin(admin Admin) error {
	return r.UpdateAdminContext(context.Background(), admin)
}

func (r *SQLiteRepository) UpdateAdminContext(ctx context.Context, admin Admin) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin owner account update: %w", err)
	}
	defer tx.Rollback()

	expectedUpdatedAt := formatTime(admin.UpdatedAt)
	admin.UpdatedAt = time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, email = ?, updated_at = ?
		WHERE role = 'owner' AND username = ? AND (? = '' OR updated_at = ?)`,
		admin.PasswordHash, admin.Email, formatTime(admin.UpdatedAt), admin.Username, expectedUpdatedAt, expectedUpdatedAt)
	if err != nil {
		return fmt.Errorf("update owner: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated owner count: %w", err)
	}
	if affected == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE role = 'owner' AND username = ?)`, admin.Username).Scan(&exists); err != nil {
			return fmt.Errorf("check owner after failed update: %w", err)
		}
		if exists {
			return ErrStaleWrite
		}
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE username = ?`, admin.Username); err != nil {
		return fmt.Errorf("revoke owner password reset tokens: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit owner account update: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) UpdateAdminAndRevokeOtherSessions(admin Admin, exceptTokenHash string) error {
	return r.UpdateAdminAndRevokeOtherSessionsContext(context.Background(), admin, exceptTokenHash)
}

func (r *SQLiteRepository) UpdateAdminAndRevokeOtherSessionsContext(ctx context.Context, admin Admin, exceptTokenHash string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin owner account update: %w", err)
	}
	defer tx.Rollback()

	expectedUpdatedAt := formatTime(admin.UpdatedAt)
	admin.UpdatedAt = time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, email = ?, updated_at = ?
		WHERE role = 'owner' AND username = ? AND (? = '' OR updated_at = ?)`,
		admin.PasswordHash, admin.Email, formatTime(admin.UpdatedAt), admin.Username, expectedUpdatedAt, expectedUpdatedAt)
	if err != nil {
		return fmt.Errorf("update owner: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated owner count: %w", err)
	}
	if affected == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE role = 'owner' AND username = ?)`, admin.Username).Scan(&exists); err != nil {
			return fmt.Errorf("check owner after failed update: %w", err)
		}
		if exists {
			return ErrStaleWrite
		}
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE (username = ? OR user_id = ?) AND token_hash <> ?`, admin.Username, admin.ID, exceptTokenHash); err != nil {
		return fmt.Errorf("revoke other owner sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE username = ?`, admin.Username); err != nil {
		return fmt.Errorf("revoke owner password reset tokens: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit owner account update: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) Users() ([]User, error) {
	return r.UsersContext(context.Background())
}

func (r *SQLiteRepository) UsersContext(ctx context.Context) ([]User, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, username, password_hash, email, role, active, created_at, updated_at FROM users ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func (r *SQLiteRepository) User(username string) (User, error) {
	return r.UserContext(context.Background(), username)
}

func (r *SQLiteRepository) UserContext(ctx context.Context, username string) (User, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, username, password_hash, email, role, active, created_at, updated_at FROM users WHERE username = ?`, username)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("read user: %w", err)
	}
	return user, nil
}

func (r *SQLiteRepository) CreateUser(user User) (User, error) {
	return r.CreateUserContext(context.Background(), user)
}

func (r *SQLiteRepository) CreateUserContext(ctx context.Context, user User) (User, error) {
	if user.ID == "" {
		user.ID = newUserID()
	}
	if user.Role == "" {
		user.Role = "editor"
	}
	user.Active = true
	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("begin user creation: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO users(id, username, password_hash, email, role, active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, user.ID, user.Username, user.PasswordHash, user.Email, user.Role, boolToInt(user.Active), formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	if err != nil {
		return User{}, wrapSQLiteWriteError("create user", err)
	}
	created, err := scanUser(tx.QueryRowContext(ctx, `SELECT id, username, password_hash, email, role, active, created_at, updated_at FROM users WHERE username = ?`, user.Username))
	if err != nil {
		return User{}, fmt.Errorf("read created user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit user creation: %w", err)
	}
	return created, nil
}

func (r *SQLiteRepository) UpdateUser(user User) (User, error) {
	return r.UpdateUserContext(context.Background(), user)
}

func (r *SQLiteRepository) UpdateUserContext(ctx context.Context, user User) (User, error) {
	expectedUpdatedAt := formatTime(user.UpdatedAt)
	user.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("begin user update: %w", err)
	}
	defer tx.Rollback()

	var result sql.Result
	if user.PasswordHash == "" {
		result, err = tx.ExecContext(ctx, `UPDATE users SET email = ?, role = ?, active = ?, updated_at = ?
			WHERE id = ? AND (? = '' OR updated_at = ?)`,
			user.Email, user.Role, boolToInt(user.Active), formatTime(user.UpdatedAt), user.ID, expectedUpdatedAt, expectedUpdatedAt)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, email = ?, role = ?, active = ?, updated_at = ?
			WHERE id = ? AND (? = '' OR updated_at = ?)`,
			user.PasswordHash, user.Email, user.Role, boolToInt(user.Active), formatTime(user.UpdatedAt), user.ID, expectedUpdatedAt, expectedUpdatedAt)
	}
	if err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return User{}, fmt.Errorf("read updated user count: %w", err)
	}
	if affected == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, user.ID).Scan(&exists); err != nil {
			return User{}, fmt.Errorf("check user after failed update: %w", err)
		}
		if exists {
			return User{}, ErrStaleWrite
		}
		return User{}, ErrNotFound
	}
	updated, err := scanUser(tx.QueryRowContext(ctx, `SELECT id, username, password_hash, email, role, active, created_at, updated_at FROM users WHERE username = ?`, user.Username))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("read updated user: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE username = ?`, user.Username); err != nil {
		return User{}, fmt.Errorf("revoke user password reset tokens: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit user update: %w", err)
	}
	return updated, nil
}

func (r *SQLiteRepository) UpdateUserAndRevokeSessions(user User) (User, error) {
	return r.UpdateUserAndRevokeSessionsContext(context.Background(), user)
}

func (r *SQLiteRepository) UpdateUserAndRevokeSessionsContext(ctx context.Context, user User) (User, error) {
	expectedUpdatedAt := formatTime(user.UpdatedAt)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("begin user update: %w", err)
	}
	defer tx.Rollback()

	user.UpdatedAt = time.Now().UTC()
	var result sql.Result
	if user.PasswordHash == "" {
		result, err = tx.ExecContext(ctx, `UPDATE users SET email = ?, role = ?, active = ?, updated_at = ?
			WHERE id = ? AND (? = '' OR updated_at = ?)`,
			user.Email, user.Role, boolToInt(user.Active), formatTime(user.UpdatedAt), user.ID, expectedUpdatedAt, expectedUpdatedAt)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, email = ?, role = ?, active = ?, updated_at = ?
			WHERE id = ? AND (? = '' OR updated_at = ?)`,
			user.PasswordHash, user.Email, user.Role, boolToInt(user.Active), formatTime(user.UpdatedAt), user.ID, expectedUpdatedAt, expectedUpdatedAt)
	}
	if err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return User{}, fmt.Errorf("read updated user count: %w", err)
	}
	if affected == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, user.ID).Scan(&exists); err != nil {
			return User{}, fmt.Errorf("check user after failed update: %w", err)
		}
		if exists {
			return User{}, ErrStaleWrite
		}
		return User{}, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE username = ? OR user_id = ?`, user.Username, user.ID); err != nil {
		return User{}, fmt.Errorf("revoke user sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE username = ?`, user.Username); err != nil {
		return User{}, fmt.Errorf("revoke user password reset tokens: %w", err)
	}
	updated, err := scanUser(tx.QueryRowContext(ctx, `SELECT id, username, password_hash, email, role, active, created_at, updated_at FROM users WHERE id = ?`, user.ID))
	if err != nil {
		return User{}, fmt.Errorf("read updated user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit user update: %w", err)
	}
	return updated, nil
}

func (r *SQLiteRepository) DeleteUser(username string) error {
	return r.DeleteUserContext(context.Background(), username)
}

func (r *SQLiteRepository) DeleteUserContext(ctx context.Context, username string) error {
	return r.DeleteUserAndRevokeSessionsContext(ctx, username)
}

func (r *SQLiteRepository) DeleteUserAndRevokeSessions(username string) error {
	return r.DeleteUserAndRevokeSessionsContext(context.Background(), username)
}

func (r *SQLiteRepository) DeleteUserAndRevokeSessionsContext(ctx context.Context, username string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin user deletion: %w", err)
	}
	defer tx.Rollback()

	var role string
	err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE username = ?`, username).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read user before deletion: %w", err)
	}
	if role == "owner" {
		return ErrProtectedAccount
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE username = ? OR user_id = (SELECT id FROM users WHERE username = ?)`, username, username); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE username = ?`, username); err != nil {
		return fmt.Errorf("revoke user password reset tokens: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM users WHERE username = ? AND role <> 'owner'`, username)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted user count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user deletion: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) Setup(site Site, admin Admin) error {
	return r.SetupContext(context.Background(), site, admin)
}

func (r *SQLiteRepository) SetupContext(ctx context.Context, site Site, admin Admin) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start setup transaction: %w", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sites`).Scan(&count); err != nil {
		return fmt.Errorf("check setup state: %w", err)
	}
	if count > 0 {
		return ErrAlreadyInitialized
	}
	now := time.Now().UTC()
	site.CreatedAt = now
	site.UpdatedAt = now
	admin.CreatedAt = now
	admin.UpdatedAt = now
	admin.ID = "user-owner"
	admin.Role = "owner"
	admin.Active = true
	if _, err := tx.ExecContext(ctx, `INSERT INTO sites(id, name, url, admin_mail, timezone, created_at, updated_at) VALUES (1, ?, ?, ?, ?, ?, ?)`, site.Name, site.URL, site.AdminMail, site.Timezone, formatTime(site.CreatedAt), formatTime(site.UpdatedAt)); err != nil {
		return fmt.Errorf("save site: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, username, password_hash, email, role, active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, admin.ID, admin.Username, admin.PasswordHash, admin.Email, admin.Role, boolToInt(admin.Active), formatTime(admin.CreatedAt), formatTime(admin.UpdatedAt)); err != nil {
		return fmt.Errorf("save owner user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit setup transaction: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) UpdateSite(site Site) error {
	return r.UpdateSiteContext(context.Background(), site)
}

func (r *SQLiteRepository) UpdateSiteContext(ctx context.Context, site Site) error {
	site.UpdatedAt = time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `UPDATE sites SET name = ?, url = ?, admin_mail = ?, timezone = ?, description = ?, keywords = ?, updated_at = ? WHERE id = 1`, site.Name, site.URL, site.AdminMail, site.Timezone, site.Description, site.Keywords, formatTime(site.UpdatedAt))
	if err != nil {
		return fmt.Errorf("update site: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated site row count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) UpdateReadingSettings(site Site) error {
	return r.UpdateReadingSettingsContext(context.Background(), site)
}

func (r *SQLiteRepository) UpdateReadingSettingsContext(ctx context.Context, site Site) error {
	site.UpdatedAt = time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `UPDATE sites SET posts_per_page = ?, comments_enabled = ?, comments_require_moderation = ?, comments_threaded = ?, updated_at = ? WHERE id = 1`, site.PostsPerPage, boolToInt(site.CommentsEnabled), boolToInt(site.CommentsRequireModeration), boolToInt(site.CommentsThreaded), formatTime(site.UpdatedAt))
	if err != nil {
		return fmt.Errorf("update reading settings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated reading settings row count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
