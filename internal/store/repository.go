package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrNotFound           = errors.New("record not found")
	ErrConflict           = errors.New("unique constraint conflict")
	ErrStaleWrite         = errors.New("record changed since it was read")
	ErrAlreadyInitialized = errors.New("application is already initialized")
	ErrProtectedAccount   = errors.New("account is protected")
)

type Site struct {
	Name                      string    `json:"name"`
	URL                       string    `json:"url"`
	AdminMail                 string    `json:"-"`
	Timezone                  string    `json:"timezone"`
	Description               string    `json:"description"`
	Keywords                  string    `json:"keywords"`
	PostsPerPage              int       `json:"postsPerPage"`
	CommentsEnabled           bool      `json:"commentsEnabled"`
	CommentsRequireModeration bool      `json:"commentsRequireModeration"`
	CommentsThreaded          bool      `json:"commentsThreaded"`
	CreatedAt                 time.Time `json:"createdAt"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Admin is the owner account stored in the users table.
type Admin = User

type Post struct {
	ID                 string            `json:"id"`
	Title              string            `json:"title"`
	Slug               string            `json:"slug"`
	Excerpt            string            `json:"excerpt"`
	Content            string            `json:"content"`
	CustomFields       map[string]string `json:"customFields,omitempty"`
	Status             string            `json:"status"`
	AllowComment       bool              `json:"allowComment"`
	Categories         []Term            `json:"categories,omitempty"`
	Tags               []Term            `json:"tags,omitempty"`
	PublishedAt        time.Time         `json:"publishedAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
	PassphraseHash     string            `json:"-"`
	SetPassphrase      bool              `json:"-"`
	PassphrasePlain    string            `json:"-"`
	RequestedPublishAt time.Time         `json:"-"`
	SetCustomFields    bool              `json:"-"`
	ExpectedUpdatedAt  time.Time         `json:"-"`
}

type Page struct {
	ID                string    `json:"id"`
	Title             string    `json:"title"`
	Slug              string    `json:"slug"`
	Excerpt           string    `json:"excerpt"`
	Content           string    `json:"content"`
	Status            string    `json:"status"`
	PublishedAt       time.Time `json:"publishedAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	ExpectedUpdatedAt time.Time `json:"-"`
}

type Term struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Kind      string    `json:"kind"`
	PostCount int       `json:"postCount"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type TermQuery struct {
	Kind          string
	Search        string
	IncludeDrafts bool
	Page          int
	PageSize      int
}

type TermPage struct {
	Items    []Term `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type Comment struct {
	ID          string    `json:"id"`
	PostID      string    `json:"postId"`
	ParentID    string    `json:"parentId,omitempty"`
	PostTitle   string    `json:"postTitle,omitempty"`
	PostSlug    string    `json:"postSlug,omitempty"`
	AuthorName  string    `json:"authorName"`
	AuthorEmail string    `json:"-"`
	AuthorURL   string    `json:"authorUrl,omitempty"`
	Content     string    `json:"content"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Media struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	StorageKey  string    `json:"-"`
	CreatedAt   time.Time `json:"createdAt"`
}

type PostStats struct {
	Posts           int `json:"posts"`
	PublishedPosts  int `json:"publishedPosts"`
	Drafts          int `json:"drafts"`
	Comments        int `json:"comments"`
	PendingComments int `json:"pendingComments"`
}

type PostQuery struct {
	Search          string
	IncludeDrafts   bool
	IncludeContent  bool
	ContentPreview  bool
	Status          string
	TermKind        string
	TermSlug        string
	CategorySlug    string
	TagSlug         string
	Order           string
	PublishedFrom   time.Time
	PublishedBefore time.Time
	Page            int
	PageSize        int
}

type MonthCount struct {
	Month string `json:"month"`
	Count int    `json:"count"`
}

type PostPage struct {
	Items    []Post `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type PageQuery struct {
	Search        string
	IncludeDrafts bool
	Page          int
	PageSize      int
}

type PagePage struct {
	Items    []Page `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type CommentQuery struct {
	PostID             string
	Status             string
	IncludePending     bool
	PublishedPostsOnly bool
	Page               int
	PageSize           int
	Cursor             *CommentCursor
}

type CommentCursor struct {
	SnapshotAt time.Time
	BeforeAt   time.Time
	BeforeID   string
}

type CommentPage struct {
	Items      []Comment `json:"items"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	PageSize   int       `json:"pageSize"`
	HasMore    bool      `json:"-"`
	SnapshotAt time.Time `json:"-"`
}

type MediaQuery struct {
	Page     int
	PageSize int
}

type MediaPage struct {
	Items    []Media `json:"items"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"pageSize"`
}

type AuditEvent struct {
	ID            string            `json:"id"`
	ActorUserID   string            `json:"actorUserId,omitempty"`
	ActorUsername string            `json:"actorUsername"`
	Action        string            `json:"action"`
	ResourceType  string            `json:"resourceType"`
	ResourceID    string            `json:"resourceId,omitempty"`
	Details       map[string]string `json:"details,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
}

type AuditQuery struct {
	Action       string
	ResourceType string
	Page         int
	PageSize     int
}

type AuditPage struct {
	Items    []AuditEvent `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
}

type Repository interface {
	Initialized() bool
	InitializedContext(context.Context) (bool, error)
	Site() (*Site, error)
	SiteContext(context.Context) (*Site, error)
	Admin() (*Admin, error)
	AdminContext(context.Context) (*Admin, error)
	UpdateAdmin(Admin) error
	UpdateAdminContext(context.Context, Admin) error
	UpdateAdminAndRevokeOtherSessions(Admin, string) error
	UpdateAdminAndRevokeOtherSessionsContext(context.Context, Admin, string) error
	Users() ([]User, error)
	UsersContext(context.Context) ([]User, error)
	User(string) (User, error)
	UserContext(context.Context, string) (User, error)
	CreateUser(User) (User, error)
	CreateUserContext(context.Context, User) (User, error)
	UpdateUser(User) (User, error)
	UpdateUserContext(context.Context, User) (User, error)
	UpdateUserAndRevokeSessions(User) (User, error)
	UpdateUserAndRevokeSessionsContext(context.Context, User) (User, error)
	DeleteUser(string) error
	DeleteUserContext(context.Context, string) error
	DeleteUserAndRevokeSessions(string) error
	DeleteUserAndRevokeSessionsContext(context.Context, string) error
	Setup(Site, Admin) error
	SetupContext(context.Context, Site, Admin) error
	UpdateSite(Site) error
	UpdateSiteContext(context.Context, Site) error
	UpdateReadingSettings(Site) error
	UpdateReadingSettingsContext(context.Context, Site) error
	Posts() ([]Post, error)
	SearchPosts(PostQuery) (PostPage, error)
	SearchPostsContext(context.Context, PostQuery) (PostPage, error)
	ListPublishedMeta() ([]Post, error)
	ListPublishedMetaContext(context.Context) ([]Post, error)
	PublishedMonthCountsContext(context.Context, *time.Location) ([]MonthCount, error)
	PublishedNeighbors(Post) (*Post, *Post, error)
	PublishedNeighborsContext(context.Context, Post) (*Post, *Post, error)
	Post(string) (Post, error)
	PostContext(context.Context, string) (Post, error)
	PostBySlug(string) (Post, error)
	PostBySlugContext(context.Context, string) (Post, error)
	SavePost(Post) (Post, error)
	SavePostContext(context.Context, Post) (Post, error)
	DeletePost(string) error
	DeletePostContext(context.Context, string) error
	SearchPages(PageQuery) (PagePage, error)
	SearchPagesContext(context.Context, PageQuery) (PagePage, error)
	Page(string) (Page, error)
	PageContext(context.Context, string) (Page, error)
	PageBySlug(string) (Page, error)
	PageBySlugContext(context.Context, string) (Page, error)
	SavePage(Page) (Page, error)
	SavePageContext(context.Context, Page) (Page, error)
	DeletePage(string) error
	DeletePageContext(context.Context, string) error
	ListTerms(TermQuery) (TermPage, error)
	ListTermsContext(context.Context, TermQuery) (TermPage, error)
	SaveTerm(Term) (Term, error)
	SaveTermWithChange(Term) (Term, bool, error)
	SaveTermWithChangeContext(context.Context, Term) (Term, bool, error)
	DeleteTerm(string) error
	DeleteTermContext(context.Context, string) error
	ListComments(CommentQuery) (CommentPage, error)
	ListCommentsContext(context.Context, CommentQuery) (CommentPage, error)
	Comment(string) (Comment, error)
	CommentContext(context.Context, string) (Comment, error)
	SaveComment(Comment) (Comment, error)
	SaveCommentContext(context.Context, Comment) (Comment, error)
	UpdateCommentStatus(string, string) (Comment, bool, error)
	UpdateCommentStatusContext(context.Context, string, string) (Comment, bool, error)
	DeleteComment(string) error
	DeleteCommentContext(context.Context, string) error
	ListMedia(MediaQuery) (MediaPage, error)
	ListMediaContext(context.Context, MediaQuery) (MediaPage, error)
	Media(string) (Media, error)
	MediaContext(context.Context, string) (Media, error)
	SaveMedia(Media) (Media, error)
	SaveMediaContext(context.Context, Media) (Media, error)
	DeleteMedia(string) error
	RecordAudit(AuditEvent) error
	ListAudit(AuditQuery) (AuditPage, error)
	ListAuditContext(context.Context, AuditQuery) (AuditPage, error)
	Stats() (PostStats, error)
	StatsContext(context.Context) (PostStats, error)
	CreateSession(string, string, time.Time) error
	CreateSessionContext(context.Context, string, string, time.Time) error
	CreateAuthenticatedSessionContext(context.Context, string, string, string, time.Time) error
	ResolveSession(string, time.Time) (string, error)
	ResolveSessionContext(context.Context, string, time.Time) (string, error)
	DeleteSession(string) error
	DeleteUserSessions(string) error
	DeleteOtherUserSessions(string, string) error
	PurgeSessions(time.Time) error
	PurgeSessionsContext(context.Context, time.Time) error
	SaveOutboundMail(string, string, string) error
	SaveOutboundMailContext(context.Context, string, string, string) error
	ListOutboundMail(int, int) (OutboundMailPage, error)
	ListOutboundMailContext(context.Context, int, int) (OutboundMailPage, error)
	CleanupOutboundMail(context.Context, time.Time) error
	SavePasswordReset(string, string, time.Time) error
	SavePasswordResetContext(context.Context, string, string, time.Time) error
	CreatePasswordResetForActiveUserContext(context.Context, string, string, time.Time) (User, error)
	DeletePasswordReset(string) error
	DeletePasswordResetContext(context.Context, string) error
	ConsumePasswordReset(string, time.Time) (string, error)
	CheckPasswordResetContext(context.Context, string, time.Time) error
	CompletePasswordReset(context.Context, string, time.Time, string) error
	ListPostRevisionPageContext(context.Context, string, int, int) (PostRevisionPage, error)
	PostRevision(string) (PostRevision, error)
	PostRevisionContext(context.Context, string) (PostRevision, error)
	BackupDatabase(string) error
	BackupDatabaseContext(context.Context, string) error
	Close() error
}

type PostRevision struct {
	ID           string            `json:"id"`
	PostID       string            `json:"postId"`
	Title        string            `json:"title"`
	Excerpt      string            `json:"excerpt"`
	Content      string            `json:"content"`
	CustomFields map[string]string `json:"customFields,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
}

type PostRevisionPage struct {
	Items    []PostRevisionSummary `json:"items"`
	Total    int                   `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
}

type PostRevisionSummary struct {
	ID        string    `json:"id"`
	PostID    string    `json:"postId"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

type OutboundMail struct {
	ID        string    `json:"id"`
	Recipient string    `json:"recipient"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type OutboundMailPage struct {
	Items    []OutboundMail
	Total    int
	Page     int
	PageSize int
}

type SQLiteRepository struct {
	db *sql.DB
}
