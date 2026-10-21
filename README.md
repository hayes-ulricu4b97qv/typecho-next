# Typecho Next

Typecho Next is a personal publishing platform with a Go API and a Vue 3 + TypeScript client.
Native development and packaged releases target Windows x64. Linux servers can deploy from source with the provided Docker Compose stack.

## Product entry points

- `/setting` — first deployment setup and site settings
- `/admin` — administrator login, dashboard and writing
- `/user` — public reader experience
- `/user/{slug}` — public article detail and comments
- `/page/{slug}` — public static page
- `/feed.xml` / `/sitemap.xml` / `/robots.txt` — public feed, sitemap, and crawler rules
- `/api/v1` — versioned JSON API
- `/healthz` / `/readyz` — liveness and readiness checks

The service redirects every page to `/setting` until setup creates the site and the owner account. Runtime data is stored in SQLite at `APP_DATA_DIR/app.db`. Opening an empty data directory creates a database with the current schema. Older database schemas are not upgraded: the application refuses unversioned or unsupported databases without changing them. Use a new empty `APP_DATA_DIR` to initialize a site. Importing old PHP or `app.json` data is not supported.

## Install a release

When a release is available, download the Windows x64 ZIP from the [Releases page](https://github.com/hayes-ulricu4b97qv/tyoechonext/releases) and extract it to a stable application directory. Run `start.cmd`, then open <http://127.0.0.1:8081/setting> to create the owner account and configure the site. If the page has no published package yet, use the development quick start below to run the app from source.

The launcher stores the database and uploads under `%LOCALAPPDATA%\TypechoNext\data` by default, separately from the application files. Back up that data directory before replacing an installation. For public access, keep the app bound to `127.0.0.1` and put a TLS reverse proxy in front of it. Upgrade and rollback steps are in [docs/operations.md](docs/operations.md).

## Deploy with Docker

For a Linux server, install Docker Engine and the Compose plugin, point your domain to the server, and allow inbound TCP ports 80 and 443. Copy `.env.example` to `.env` and set `SITE_DOMAIN` to the public hostname without a scheme or path. Then run from the repository root:

```sh
cp .env.example .env
docker compose --env-file .env -f deploy/compose.yaml up -d --build
```

Open `https://your-domain/setting` to create the owner account. Caddy obtains TLS certificates automatically. Only Caddy publishes host ports; the app stays on the private Compose network. SQLite, uploads, and Caddy certificates are stored in named volumes. This stack builds the image locally; the project does not publish container images.

## Development quick start

```powershell
cd web
npm ci
npm run build
cd ..
go run ./cmd/server
```

Open <http://127.0.0.1:8081/setting> and complete the setup form. The HTTP service binds to `127.0.0.1` by default. For development, run Vite with `cd web && npm run dev`; its API, media, health and readiness proxies use `APP_PORT` or `VITE_API_PROXY_TARGET`.

See [docs/architecture.md](docs/architecture.md) for the module boundaries.
See [docs/operations.md](docs/operations.md) for native and container deployment, configuration, backups and recovery.
See [docs/productization.md](docs/productization.md) for the supported product scope and release expectations.
Password recovery requires SMTP configuration; see the operations guide for the supported TLS modes and environment variables.

## Requirements

Running a Windows release requires Windows x64 and writable data and media directories. Go, Node.js, and GCC are not needed to run the release package. Docker deployment requires a Linux host with Docker Engine and the Compose plugin; the Compose file builds the image locally.

Building from source requires Go 1.27.3 or newer, Windows with PowerShell, Node.js 22 or newer, and MinGW-w64 GCC on PATH for CGO SQLite builds. Release packaging also requires `objdump` from MinGW-w64 Binutils to check executable imports.

The service needs a writable `APP_DATA_DIR`; `APP_MEDIA_DIR` defaults to `APP_DATA_DIR/uploads` and only needs a separate value when media should be stored elsewhere.

SQLite stores this personal blog's content, accounts and sessions. Media files are stored in `APP_MEDIA_DIR`.

Article listing accepts `q`, `category`, `tag`, `month`, `order` (`published` by default, or `updated`), `status` (`draft` or `published`), `page`, and `pageSize`. The draft status filter requires a content editor and is applied before pagination. Search terms are limited to 200 Unicode characters; an explicit `pageSize` outside 1–100 is rejected. Sessions are persisted in SQLite with only a SHA-256 token hash stored server side. Each session references `users.id`, so a login remains valid for a later request against the same database. `APP_SESSION_TTL_MIN` sets the session lifetime in minutes (default 1440); values outside the representable duration range fall back to the default.
Search terms are treated literally, including `%` and `_`.

Posts accept `categories` and `tags` as JSON string arrays. The Go service stores them in normalized taxonomy tables and exposes published term counts through `GET /api/v1/content/terms`. Content editors can manage terms with `POST/PUT/DELETE /api/v1/content/terms`. The admin form splits a comma-separated field into those arrays before sending the request.
Post lists can be filtered with `category=<slug>` and/or `tag=<slug>`; when both are present, the API returns their intersection. `month=YYYY-MM` limits the list to that archive month.
The Vue reader exposes published categories, tags, and search terms as shareable URL filters.
Article details render Markdown content in Vue and sanitize generated HTML before insertion.
The Go service validates content limits: titles up to 200 characters, slugs up to 120, excerpts up to 500, and bodies up to 500,000 characters.
Articles can include a `customFields` string map (up to 50 fields and 20,000 combined characters). Fields are saved with revision snapshots, displayed as text on article details, and returned only when the reader can see the body; a post passphrase protects both together.
Content editors can create independent pages through `GET/POST /api/v1/content/pages` and update or delete them through `/api/v1/content/pages/{id}`.
When editing an article without a slug, the existing permalink is preserved; only new articles derive a slug from the title. Automatic slugs preserve Unicode letters and digits, so non-Latin titles and taxonomy names keep stable readable URLs.
Editors can preview drafts from the admin list; draft routes remain unavailable to anonymous readers.

Go serves `/feed.xml` from published posts only. Each item uses the post's published time, and the description is the excerpt or a truncated body. The feed is encoded page by page to keep server memory independent of the total post count. `/sitemap.xml` lists the reader home, published posts, published terms, and published pages. `/robots.txt` allows `/`, disallows `/admin` and `/setting`, and adds a `Sitemap` line when the site URL is set.

Owner accounts can inspect management history through `GET /api/v1/audit`; successful content, media, comment, user, account, and site changes record the actor and resource.
Write requests use a double-submit CSRF token; the Vue API client copies the token from the non-HttpOnly cookie into `X-CSRF-Token`. Initial setup and login are the only unauthenticated write entry points that do not require a prior token.

文章详情支持评论：默认先进入待审核状态，管理员在 `/admin` 审核后才会公开显示。所有者可以在 `/setting` 调整每页文章数、关闭评论、改为直接公开，或允许最多三层回复。单篇文章也可以关闭评论。

管理员也可以在 `/admin` 上传 JPEG、PNG、GIF 或 WebP 图片，文件保存在 `APP_MEDIA_DIR`，单文件上限为 10 MiB。服务会检查图片头和尺寸；最长边不得超过 16,384 像素，总像素不得超过 4,000 万。

所有者可以在 `/setting` 创建编辑者和审核员账户；权限由 Go API 校验，停用账户会立即阻止后续请求。更换所有者密码时会保留当前会话并撤销其他会话；工作人员被停用或删除时会清理其会话。
所有者也可以在用户列表中为工作人员重置密码，旧会话会立即失效。

管理员登录后可在 `/setting` 更新账户邮箱；提交当前密码和新密码即可轮换管理员密码。账户变更写入 `users` 表中的所有者记录。

## Contributing

Please see [CONTRIBUTING](CONTRIBUTING.md) for details.
