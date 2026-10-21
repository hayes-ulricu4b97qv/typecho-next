package store

type schemaTable struct {
	name    string
	columns []string
}

var currentSchemaTables = []schemaTable{
	{"sites", []string{
		"id", "name", "url", "admin_mail", "timezone", "description", "keywords",
		"posts_per_page", "comments_enabled", "comments_require_moderation", "comments_threaded",
		"created_at", "updated_at",
	}},
	{"users", []string{
		"id", "username", "password_hash", "email", "role", "active", "created_at", "updated_at",
	}},
	{"posts", []string{
		"id", "title", "slug", "excerpt", "content", "status", "allow_comment",
		"published_at", "updated_at", "passphrase_hash", "custom_fields",
	}},
	{"pages", []string{
		"id", "title", "slug", "excerpt", "content", "status", "published_at", "updated_at",
	}},
	{"terms", []string{
		"id", "name", "slug", "kind", "created_at", "updated_at",
	}},
	{"post_terms", []string{"post_id", "term_id"}},
	{"comments", []string{
		"id", "post_id", "parent_id", "author_name", "author_email", "author_url",
		"content", "status", "created_at", "updated_at",
	}},
	{"media", []string{"id", "filename", "content_type", "size", "storage_key", "created_at"}},
	{"sessions", []string{"token_hash", "user_id", "username", "expires_at", "created_at"}},
	{"audit_events", []string{
		"id", "actor_user_id", "actor_username", "action", "resource_type", "resource_id", "details", "created_at",
	}},
	{"search_tokens", []string{"kind", "token", "record_id"}},
	{"post_revisions", []string{
		"id", "post_id", "title", "excerpt", "content", "custom_fields", "created_at",
	}},
	{"outbound_mail", []string{"id", "recipient", "subject", "body", "created_at"}},
	{"password_resets", []string{"token_hash", "username", "expires_at", "created_at"}},
}

var legacySchemaTables = []schemaTable{
	{"schema_migrations", []string{"version", "applied_at"}},
	{"sites", []string{
		"id", "name", "url", "admin_mail", "timezone", "created_at", "updated_at",
	}},
	{"users", []string{
		"id", "username", "password_hash", "email", "role", "active", "created_at", "updated_at",
	}},
	{"posts", []string{
		"id", "title", "slug", "excerpt", "content", "status", "published_at", "updated_at",
	}},
	{"pages", []string{
		"id", "title", "slug", "excerpt", "content", "status", "published_at", "updated_at",
	}},
	{"terms", []string{"id", "name", "slug", "kind", "created_at", "updated_at"}},
	{"post_terms", []string{"post_id", "term_id"}},
	{"comments", []string{
		"id", "post_id", "author_name", "author_email", "content", "status", "created_at", "updated_at",
	}},
	{"media", []string{"id", "filename", "content_type", "size", "storage_key", "created_at"}},
	{"sessions", []string{"token_hash", "username", "expires_at", "created_at"}},
	{"audit_events", []string{
		"id", "actor_user_id", "actor_username", "action", "resource_type", "resource_id", "details", "created_at",
	}},
	{"search_tokens", []string{"kind", "token", "record_id"}},
	{"post_revisions", []string{"id", "post_id", "title", "excerpt", "content", "created_at"}},
	{"outbound_mail", []string{"id", "recipient", "subject", "body", "created_at"}},
	{"password_resets", []string{"token_hash", "username", "expires_at", "created_at"}},
}

var legacyOptionalColumns = map[string]map[string]bool{
	"sites": {
		"description": true, "keywords": true, "posts_per_page": true,
		"comments_enabled": true, "comments_require_moderation": true, "comments_threaded": true,
	},
	"posts":          {"allow_comment": true, "passphrase_hash": true, "custom_fields": true},
	"comments":       {"parent_id": true, "author_url": true},
	"sessions":       {"user_id": true},
	"post_revisions": {"custom_fields": true},
}

var integerSchemaColumns = map[string]map[string]bool{
	"sites": {
		"id": true, "posts_per_page": true, "comments_enabled": true,
		"comments_require_moderation": true, "comments_threaded": true,
	},
	"users":             {"active": true},
	"posts":             {"allow_comment": true},
	"media":             {"size": true},
	"schema_migrations": {"version": true},
}

var schemaColumnDefaults = map[string]map[string]string{
	"sites": {
		"description": "''", "keywords": "''", "posts_per_page": "10",
		"comments_enabled": "1", "comments_require_moderation": "1", "comments_threaded": "1",
	},
	"users": {"role": "'editor'", "active": "1"},
	"posts": {
		"excerpt": "''", "content": "''", "status": "'draft'", "allow_comment": "1",
		"passphrase_hash": "''", "custom_fields": "'{}'",
	},
	"pages": {"excerpt": "''", "content": "''", "status": "'draft'"},
	"comments": {
		"parent_id": "''", "author_email": "''", "author_url": "''", "status": "'pending'",
	},
	"sessions":       {"user_id": "''"},
	"audit_events":   {"actor_user_id": "''", "resource_id": "''", "details": "''"},
	"post_revisions": {"custom_fields": "'{}'"},
}

var schemaPrimaryKeys = map[string][]string{
	"schema_migrations": {"version"},
	"sites":             {"id"},
	"users":             {"id"},
	"posts":             {"id"},
	"pages":             {"id"},
	"terms":             {"id"},
	"post_terms":        {"post_id", "term_id"},
	"comments":          {"id"},
	"media":             {"id"},
	"sessions":          {"token_hash"},
	"audit_events":      {"id"},
	"search_tokens":     {"kind", "token", "record_id"},
	"post_revisions":    {"id"},
	"outbound_mail":     {"id"},
	"password_resets":   {"token_hash"},
}

var nullableSchemaColumns = map[string]map[string]bool{
	"posts": {"published_at": true},
	"pages": {"published_at": true},
}

var implicitNotNullPrimaryKeyColumns = map[string]map[string]bool{
	"schema_migrations": {"version": true},
	"sites":             {"id": true},
	"users":             {"id": true},
	"posts":             {"id": true},
	"pages":             {"id": true},
	"terms":             {"id": true},
	"comments":          {"id": true},
	"media":             {"id": true},
	"sessions":          {"token_hash": true},
	"audit_events":      {"id": true},
	"post_revisions":    {"id": true},
	"outbound_mail":     {"id": true},
	"password_resets":   {"token_hash": true},
}

type schemaIndexColumn struct {
	name       string
	descending bool
}

type schemaIndex struct {
	table   string
	name    string
	columns []schemaIndexColumn
}

var currentSchemaIndexes = []schemaIndex{
	{"users", "users_role_active_idx", []schemaIndexColumn{{"role", false}, {"active", false}}},
	{"posts", "posts_status_updated_idx", []schemaIndexColumn{{"status", false}, {"updated_at", true}}},
	{"posts", "posts_status_published_idx", []schemaIndexColumn{{"status", false}, {"published_at", true}, {"id", true}}},
	{"pages", "pages_status_updated_idx", []schemaIndexColumn{{"status", false}, {"updated_at", true}}},
	{"terms", "terms_kind_name_idx", []schemaIndexColumn{{"kind", false}, {"name", false}}},
	{"post_terms", "post_terms_term_idx", []schemaIndexColumn{{"term_id", false}, {"post_id", false}}},
	{"comments", "comments_post_status_created_idx", []schemaIndexColumn{{"post_id", false}, {"status", false}, {"created_at", true}}},
	{"comments", "comments_parent_idx", []schemaIndexColumn{{"parent_id", false}, {"post_id", false}}},
	{"media", "media_created_idx", []schemaIndexColumn{{"created_at", true}}},
	{"sessions", "sessions_expires_idx", []schemaIndexColumn{{"expires_at", false}}},
	{"sessions", "sessions_user_idx", []schemaIndexColumn{{"user_id", false}}},
	{"audit_events", "audit_events_created_idx", []schemaIndexColumn{{"created_at", true}, {"id", true}}},
	{"audit_events", "audit_events_resource_idx", []schemaIndexColumn{{"resource_type", false}, {"action", false}, {"created_at", true}}},
	{"post_revisions", "post_revisions_post_idx", []schemaIndexColumn{{"post_id", false}, {"created_at", true}}},
	{"outbound_mail", "outbound_mail_created_idx", []schemaIndexColumn{{"created_at", true}, {"id", true}}},
}

type schemaIndexKeyColumn struct {
	name       string
	collation  string
	descending bool
}

var schemaUniqueKeys = map[string][][]schemaIndexKeyColumn{
	"users": {{{name: "username", collation: "BINARY"}}},
	"posts": {{{name: "slug", collation: "BINARY"}}},
	"pages": {{{name: "slug", collation: "BINARY"}}},
	"terms": {
		{{name: "kind", collation: "BINARY"}, {name: "name", collation: "NOCASE"}},
		{{name: "kind", collation: "BINARY"}, {name: "slug", collation: "BINARY"}},
	},
	"media": {{{name: "storage_key", collation: "BINARY"}}},
}

type schemaForeignKey struct {
	table    string
	from     string
	to       string
	onUpdate string
	onDelete string
	match    string
}

var schemaForeignKeys = map[string][]schemaForeignKey{
	"post_terms": {
		{table: "posts", from: "post_id", to: "id", onUpdate: "NO ACTION", onDelete: "CASCADE", match: "NONE"},
		{table: "terms", from: "term_id", to: "id", onUpdate: "NO ACTION", onDelete: "CASCADE", match: "NONE"},
	},
	"comments": {
		{table: "posts", from: "post_id", to: "id", onUpdate: "NO ACTION", onDelete: "CASCADE", match: "NONE"},
	},
}

var schemaCheckExpressions = map[string][]string{
	"sites": {
		"id = 1",
		"posts_per_page BETWEEN 1 AND 50",
		"comments_enabled IN (0, 1)",
		"comments_require_moderation IN (0, 1)",
		"comments_threaded IN (0, 1)",
	},
	"users":    {"role IN ('owner', 'editor', 'moderator')", "active IN (0, 1)"},
	"posts":    {"status IN ('draft', 'published')", "allow_comment IN (0, 1)"},
	"pages":    {"status IN ('draft', 'published')"},
	"terms":    {"kind IN ('category', 'tag')"},
	"comments": {"status IN ('pending', 'approved', 'spam')"},
}

var legacyAddedColumnChecks = map[string]map[string]bool{
	"sites": {
		"posts_per_page between 1 and 50":       true,
		"comments_enabled in (0, 1)":            true,
		"comments_require_moderation in (0, 1)": true,
		"comments_threaded in (0, 1)":           true,
	},
	"posts": {"allow_comment in (0, 1)": true},
}
