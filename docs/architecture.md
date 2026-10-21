# Typecho Next 架构

这个仓库的产品是使用 Go API 和 Vue 客户端的个人博客。服务打开自己的 SQLite 数据库，并提供下面列出的路由。运行、备份与恢复见 [operations.md](operations.md)。

## 目录

```text
cmd/server/          Go 服务入口、优雅退出
internal/config/     环境配置
internal/httpapi/    HTTP 路由、鉴权与 JSON API；auth.go、content.go、settings.go 按领域承载认证、内容和设置处理
internal/service/    业务规则；account.go、content.go、engagement.go、audit_sessions.go、validation.go 按领域拆分
internal/store/      SQLite 仓储；schema.go、accounts.go、content.go、engagement.go、audit_sessions.go、scan.go 分离建表、领域查询和扫描辅助
web/src/             Vue 3 + TypeScript 前端
web/dist/            Vite 构建产物（运行前生成）
```

## 路由

| 路径 | 用途 |
| --- | --- |
| `/setting` | 首次部署初始化、站点设置 |
| `/admin` | 管理端登录、统计、文章编辑 |
| `/user` | 用户端公开文章列表、搜索与 taxonomy 筛选 |
| `/user/{slug}` | 用户端文章详情与评论 |
| `/page/{slug}` | 用户端独立页面 |
| `/feed.xml`、`/sitemap.xml`、`/robots.txt` | Go 服务提供的公开订阅与索引入口 |
| `/api/v1/*` | 版本化 JSON API |
| `/healthz` | 存活检查，初始化前也返回正常 |
| `/readyz` | 就绪检查，初始化完成前返回 503 |

服务启动时打开 `APP_DATA_DIR/app.db`。空数据库会在单个事务中创建当前完整 schema，并写入 `PRAGMA user_version=2`。现有数据库必须带有当前版本号且通过完整结构校验；未版本化的非空数据库和其他 schema 版本会在修改前拒绝启动。程序不升级早期 Go 数据库，也不导入 PHP 或 `app.json` 数据；创建新站点时应配置新的空 `APP_DATA_DIR`。没有初始化记录时，页面请求转到 `/setting`，初始化成功后才开放 `/admin` 和 `/user`；检查初始化状态时若 SQLite 读取失败，API 返回 `503 not_ready`，普通前端页面也返回 `503`，不会伪装成尚未初始化。

文章接口支持 `q`、`category`、`tag`、`month`、`order`、`status`、`page` 和 `pageSize` 参数。`order` 默认按 `published_at`，传入 `updated` 时按更新时间。`status` 可指定 `draft` 或 `published`；草稿筛选只向内容编辑者开放，并在 SQL 分页前应用，因此筛选总数与页码一致。显式传入 1 到 100 之外的 `pageSize` 会返回校验错误。公开接口只返回已发布且发布时间不晚于当前时刻的文章，内容编辑者也可以带 `includeDraft=true` 读取全部草稿和排期文章。文章列表只从 SQLite 读取摘要字段，不加载正文；详情接口读取完整正文，RSS 只从 SQLite 读取摘要或正文前 281 个字符。登录 cookie 只携带随机令牌，数据库中保存 SHA-256 哈希、`users.id` 和过期时间，因此服务重启后，同一数据库上的登录会话仍然有效。会话寿命由 `APP_SESSION_TTL_MIN` 决定，缺省 1440 分钟；鉴权读取只校验有效期，过期行由启动和每日维护任务清理。
唯一键冲突统一转为 HTTP 409；文章、页面、分类/标签和用户名分别返回稳定的 `post_taken`、`page_taken`、`term_taken`、`username_taken` 错误码。错误分类依据 SQLite 约束码，不解析驱动错误文本。
未知 `/api/v1` 路径和不支持的 HTTP 方法也返回统一的 `{error:{code,message}}` JSON；方法错误保留 `Allow` 响应头，便于客户端诊断路由问题。
所有 `/api/v1/*` 响应和服务端渲染的 HTML shell 都带 `Cache-Control: no-store`，避免 owner 数据、会话信息或草稿预览被浏览器及共享缓存保留。公开媒体 URL 使用稳定 ID，并按一年不可变资源缓存；`/assets/` 下带 Vite 内容哈希的文件按一年不可变资源缓存，未版本化静态文件和 favicon 使用 `no-cache`。
文章摘要列表将当前页文章的分类、标签和公开文章计数批量查询，避免对每篇文章单独读取关联。文章和页面搜索使用词元索引，并按字面匹配用户输入，`%` 与 `_` 不会被当作通配符；所有 `q` 参数最多接受 200 个 Unicode 字符，避免超长输入触发无界搜索工作。候选词元在一次分组查询中读取，分类和标签名称仍用带转义的 `LIKE`。

读者端正文和管理端 Markdown 预览共用 `web/src/markdown.ts`，由 `marked` 转换并经 DOMPurify 净化后再写入 DOM。媒体插入保留编辑器中的光标或选区，可用于文章和独立页面。

`/feed.xml` 只收录已发布文章，按发布时间排序，描述使用摘要或截断后的正文，并逐页编码输出以限制内存用量。公开文章、页面和词条列表，以及 RSS 与 Sitemap 的列表查询均把 HTTP 请求 context 传入 SQLite；客户端中止请求后会取消当前查询并停止后续编码。评论、媒体、用户、审计、修订历史、备份和月归档查询也使用请求 context；取消的读取不会继续生成错误响应或完成剩余归档工作。文章修订历史接口返回 `{items,total,page,pageSize}`，每页默认和最大均为 20 条；列表只发送版本 ID、文章 ID、标题和时间，编辑器按需加载更早的版本，恢复操作才读取对应正文快照。`/sitemap.xml` 包含读者首页、已发布文章、公开分类与标签、已发布独立页面；URL 超过 10,000 条时返回索引，并按每份最多 10,000 条提供分片。`/robots.txt` 允许 `/`，禁止 `/admin` 和 `/setting`，站点 URL 存在时附上 `Sitemap`。Vue 用户端不生成这些文件。

审计事件写入 SQLite 的 `audit_events` 表，并通过 owner-only 的 `GET /api/v1/audit` 查询；文章、评论、媒体、用户、账户和站点的成功变更都会记录操作者、资源和必要的变更详情。

写请求采用双提交 CSRF 令牌：Go 服务设置非 HttpOnly 的 `app_csrf` cookie，Vue API 客户端复制到 `X-CSRF-Token`；初始化和登录是仅有的无需既有令牌的写入口，匿名评论仍不要求登录。

所有响应带有资源来源策略：脚本、连接、表单和基础 URL 限定本站，禁止对象嵌入和页面被框架加载；本站 Markdown 图片仍可引用远程 HTTP/HTTPS 图片，Vue 动态样式继续可用。

管理员会话还可以通过 `PUT /api/v1/settings/account` 更新账户邮箱，或在提交 `currentPassword` 与 `newPassword` 时修改密码。密码修改沿用 bcrypt 哈希，会话按 `users.id` 关联并在换密时撤销其他令牌。邮箱和密码都写在所有者的 `users` 记录上。

文章写入接口接受 `title`、`slug`、`excerpt`、`content`、`status`、`allowComment`、`categories`、`tags`，以及可选的 `publishAt` 和 `passphrase`。`categories` 与 `tags` 是 JSON 字符串数组；管理端表单里的逗号只是录入时的拆分。省略 `allowComment` 时，新文章默认允许评论，编辑文章则保留原设置。首次发布写入 `published_at`，之后再次发布保持该时间。`publishAt` 晚于当前时间时，匿名读者在到达该时间前看不到文章。`passphrase` 只写入哈希，响应不返回口令；未提供正确口令的匿名读者看不到正文。ID 和更新时间由后端生成或维护，存储模型不会直接成为 HTTP 请求模型。
文章和独立页面的 `PUT` 更新必须提交详情响应里的 `updatedAt` 作为 `expectedUpdatedAt`。版本缺失时返回 428 `precondition_required`，版本已过期时返回 409 `conflict`，客户端应重新读取后再决定是否保存。
编辑文章时如果省略 `slug`，服务会保留原有固定链接；新文章才会根据标题生成 slug，避免改标题时破坏已有链接。自动 slug 会保留 Unicode 字母和数字，因此中文标题、分类和标签可以生成稳定的可读路径。
管理端可从文章和独立页面列表打开预览；服务只会向当前有内容编辑权限的会话返回草稿，匿名用户仍会得到不存在响应。
文章和独立页面共享内容长度约束：标题最多 200 个字符、slug 最多 120 个字符、摘要最多 500 个字符、正文最多 500,000 个字符。

文章还可以提交 `categories` 和 `tags` 名称数组；Go 服务维护 `terms` 与 `post_terms` 关系表，`GET /api/v1/content/terms` 返回分类、标签及已发布文章数，内容编辑者可通过 `POST/PUT/DELETE /api/v1/content/terms` 管理词条。
文章列表支持 `category=<slug>` 和/或 `tag=<slug>` 筛选，同时提供两个参数时返回两个词条的交集；草稿仍只会在有内容编辑权限的会话中返回。
Vue 用户端会从公开词条接口生成筛选导航，页面链接与分类/标签按批次载入以限制首屏列表长度；分类、标签和搜索状态写入 URL，因此可以直接分享并支持浏览器前进后退。读者可以同时选择分类和标签，文章结果按两者交集筛选。

文章可以提交 `customFields` 字符串映射，每篇最多 50 项，字段名最多 80 个字符、字段值最多 2,000 个字符，合计最多 20,000 个字符。字段存入 `posts.custom_fields`，文章修订会一并保存字段快照；更新时省略 `customFields` 会保留已有值，提交空对象会清空。公开列表不返回自定义字段，详情只在正文可见时返回，因此阅读口令也同时保护字段。读者端以文本方式展示字段值，不把它作为 HTML 渲染。

独立页面使用 `pages` 表和独立 API：公开端通过 `GET /api/v1/content/pages/{slug}` 读取，内容编辑者通过 `GET/POST /api/v1/content/pages`、`PUT/DELETE /api/v1/content/pages/{id}` 管理。公开页面列表、详情、服务端 HTML 和站点地图只包含 `published_at` 不晚于当前时刻的已发布页面；草稿、无发布时间和未来排期页面都对匿名读者返回不可见。

评论通过文章路由提交：`POST /api/v1/content/posts/{slug}/comments` 按站点阅读设置决定结果。默认新评论进入待审核；所有者关闭“新评论需要审核”后，评论会直接公开。公开列表只返回 `approved` 评论。管理员可以通过 `/api/v1/content/comments?status=pending` 获取待审核列表，并用 `PUT /api/v1/content/comments/{id}` 将评论设为 `approved`、`pending` 或 `spam`，也可以删除评论。
单篇评论分页响应在还有后续楼层时返回 `nextCursor`。读者端携带游标读取同一快照，避免翻页期间新评论或新通过的评论改变后续页边界；旧的 `page` / `pageSize` offset 请求仍可用。

所有者通过 `GET/PUT /api/v1/settings/reading` 管理阅读与评论：`postsPerPage`（1–50，公开文章列表未传 `pageSize` 时使用）、`commentsEnabled`、`commentsRequireModeration` 和 `commentsThreaded`。文章还可以单独设置 `allowComment`；站点或文章关闭评论时，创建评论会被拒绝。开启回复后，评论可以带 `parentId`，父评论必须是同一篇文章里已通过的评论，嵌套最多三层。公开状态接口会带上 `reading`，读者端据此分页、决定是否展示评论表单，并把回复按时间排成线程。

所有者还可以在站点设置里保存 `description` 和 `keywords`。匿名公开状态会带回这两个字段。描述非空时，`/feed.xml` 的频道描述使用这段文字；描述为空时仍回退为站点名加 ` latest posts`。站点地址只接受不超过 512 字节的 http(s) 协议和域名，可带末尾斜线，不接受路径、查询参数、片段或用户信息，因为本站链接和静态资源都从域名根路径提供。编辑和审核员不能修改站点字段。

Go 服务在返回公开文章和独立页面的应用壳前，会把标题、摘要、GFM Markdown 正文和 Open Graph 元数据写入初始 HTML。Goldmark 默认安全渲染器不会输出原始 HTML 或危险链接；口令保护文章不会在初始 HTML 暴露正文。管理页与带 `preview=true` 的页面标记为 `noindex, nofollow`。

读者首页 `/user` 的初始 HTML 还会输出公开文章摘要、分类/标签链接、搜索表单、结果总数和分页链接，使关闭 JavaScript 时仍能搜索并继续浏览。服务端分页使用站点 `postsPerPage`（无效设置回退到 10，最大 50），按 URL 中的 `q`、`category`、`tag`、`month` 和 `page` 查询；列表不读取文章正文，草稿与未来发布时间文章由公开查询排除。Vue 端从相同 URL 状态加载内容，筛选与分页链接可复制，也支持浏览器前进和后退。

公开文章详情带有按发布时间排序的上一篇和下一篇，同一发布时间用文章 ID 做稳定次序，草稿不会成为邻居。`GET /api/v1/content/archive` 按站点 IANA 时区把已发布文章收成月份计数；`GET /api/v1/content/posts?month=YYYY-MM` 返回该月的公开列表。`order=published` 按发布时间返回文章。`GET /api/v1/content/comments/recent` 只返回已发布文章下的已通过评论。Vue 读者端用这些接口渲染站点描述、相邻文章、月份归档、最新文章和最近回复；文章发布时间、页面更新时间和评论日期也使用站点时区，保持显示日期与月份归档边界一致。设置界面可输入任意有效 IANA 时区名称，并提供常用值建议；服务端负责校验且拒绝依赖运行机器时区的 `Local`。若历史站点时区无效，前端与服务端都回退到 UTC。

媒体由 Go 服务保存到 `APP_MEDIA_DIR`，元数据写入 SQLite。管理员通过 `POST /api/v1/media` 上传图片，服务只接受可解析为 JPEG、PNG、GIF 或 WebP 的图片头，单文件上限 10 MiB，最长边不得超过 16,384 像素且总像素不得超过 4,000 万；上传校验只读取图片配置，不完整解码像素数据。公开文件通过 `/media/{id}` 读取，删除时同时清理元数据和文件。

用户保存在 `users` 表。初始化直接创建 `owner`；其他账户可以是 `editor` 或 `moderator`。编辑者负责文章和媒体，审核员负责评论，站点、账户和用户管理只对所有者开放。停用用户后，其持久会话会在下一次请求时失效。密码和文章阅读口令由 bcrypt 处理，输入最多 72 个 UTF-8 字节；超出时 API 返回 422 校验错误，不会截断输入。

所有者用户管理接口为 `GET/POST /api/v1/settings/users`、`PUT/DELETE /api/v1/settings/users/{username}`；密码只在服务端生成 bcrypt 哈希，HTTP 响应不会返回密码字段。更换所有者密码时会保留当前会话并撤销其他会话；工作人员被停用或删除时会清理其会话。
所有者通过用户更新接口提交新密码即可重置工作人员密码，服务会撤销该用户已有会话。

密码找回通过 `POST /api/v1/auth/forgot` 和 `POST /api/v1/auth/reset` 完成。找回请求需要安全 SMTP 投递，口令只发到请求账户登记的邮箱，数据库只保存口令哈希；对外响应不泄露账户是否存在。SMTP 未配置时，找回接口会对所有账户返回相同的服务不可用响应。请求同时按来源和有效账户限流，账户限流配置见 [operations.md](operations.md)。待审核评论进入 `outbound_mail` 站内通知历史，所有者通过分页的 `GET /api/v1/mail` 查看；记录最多保留 90 天和 1000 条。SMTP 配置项及 TLS 要求见 [operations.md](operations.md)。

## Windows 本地运行

```powershell
cd web
npm ci
npm run build
cd ..
go run ./cmd/server
```

开发时可以并行运行 `go run ./cmd/server` 与 `cd web && npm run dev`，Vite 会把 API、媒体、存活和就绪请求代理到 `127.0.0.1:<APP_PORT>`（默认 `8081`）。设置 `VITE_API_PROXY_TARGET` 可覆盖代理目标，例如服务运行在另一台主机时。

## DELETE 幂等性

文章、页面、分类/标签、评论、媒体和职员用户的删除接口在资源已不存在或本次删除成功时都返回 `204 No Content`。只有实际删除成功才写审计事件；重试已完成的删除不会重复产生审计记录。
