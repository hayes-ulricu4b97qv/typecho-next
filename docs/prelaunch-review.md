# 上线前审查：改进、完善与拓展

> 本文是改进前的历史审查记录，条目中的“当前行为”不代表最新代码。现在仅支持 Windows，原有部署方式和 Makefile 已移除，使用 `tasks.ps1`。现行运行、备份和恢复流程见 [operations.md](operations.md)。验收应以当前代码和测试为准。

本文记录早期对 Go API 与 Vue 客户端的审查结论。站点当时尚未上线，因此审查建议可直接调整 JSON 字段、默认排序、校验和新库 schema。本文不定义现行数据库兼容范围；当前 SQLite 初始化要求见 [operations.md](operations.md) 和 [architecture.md](architecture.md)。旧 PHP Typecho 数据库与 `app.json` 导入仍不受支持。

审查读过 `cmd/server`、`internal/config`、`internal/httpapi`、`internal/service`、`internal/store`、`web/src`、`docs/architecture.md`、`README.md`和 `Makefile`。本文不是这些文件的重写。

## 表面核对

每一项要么指出具体缺口，要么说明该表面已经存在。

| 表面 | 结论 |
| --- | --- |
| 认证、CSRF、角色 | 机制已经接通，见「改进」第 6 条。会话 cookie、双提交 CSRF 和三种角色都在代码里，不是缺失功能。 |
| 文章、页面、分类、评论、媒体 | 读写、审核和上传都已接通，见「改进」第 1–3 条和「完善」第 1–4 条。 |
| Vue 读者端与管理端 | 六个视图覆盖阅读、写作、设置和审核，见「改进」第 4、9 条和「完善」第 1–7 条。 |
| SQLite schema | 审查时空数据库创建完整 schema，不匹配的旧库会在启动阶段拒绝；目前只有空库初始化和当前版本校验，不升级旧 schema，详见 [operations.md](operations.md)。 |
| 可运维性 | `/healthz`、`/readyz`、`/feed.xml`、`/sitemap.xml`、`/robots.txt` 和 `tasks.ps1` 都在，见「改进」第 3、5、8 条和「拓展」第 6 条。 |

已经存在、本文不把它说成缺失的行为：

- 登录 cookie 名 `app_session`，`HttpOnly`，库里只存 SHA-256。见 `internal/httpapi/server.go` 的 `issueSession`。
- 写请求核对 `app_csrf` cookie 与 `X-CSRF-Token`。初始化和登录除外。见同文件 `routeGuard`、`validCSRF`。Vue 在 `web/src/api.ts` 里从 cookie 抄令牌。
- `owner` 管站点、账户和用户；`editor` 管文章和媒体；`moderator` 管评论。见 `requireOwner`、`requireContentEditor`、`requireModerator`。
- 公开评论 JSON 不带邮箱；管理列表才带。见 `publicComment`。
- 文章和页面正文上限 500000 个字符，超限走校验而不是把 JSON 体限制在 1 MiB。见 `maxJSONBody` 与 `internal/service/service.go` 的 `MaxContentRunes`。
- 邻居、月份归档和最近已通过评论已经由 API 和读者端侧栏使用。见 `internal/service/archive.go` 与 `web/src/views/UserView.vue`。

## 路由对照

下表覆盖 `internal/httpapi/server.go` 里 `Handler()` 注册的全部路由。`/api/` 路径与 `web/src` 的字符串搜索一致。

| 路由 | 标记 | 依据 |
| --- | --- | --- |
| `GET /healthz` | 非客户端 | 存活检查。 |
| `GET /readyz` | 非客户端 | 未初始化返回 503 `not_ready`。 |
| `GET /feed.xml` | 非客户端 | Go 直接写 RSS。 |
| `GET /sitemap.xml` | 非客户端 | Go 直接写站点地图。 |
| `GET /robots.txt` | 非客户端 | Go 直接写纯文本。 |
| `GET /`（`frontend`，同时匹配 `HEAD`） | 非客户端 | 未初始化时 GET 页面请求 307 到 `/setting`；初始化后 GET `/` 307 到 `/user`。`/user`、`/admin`、`/setting`、`/page/...` 返回静态 `index.html`。`/assets/` 与 `/favicon.svg` 直接读文件。前端兜底以外的方法返回 405。 |
| `GET /api/v1/system/status` | 客户端调用 | `web/src/api/auth.ts` `getStatus`。 |
| `POST /api/v1/setup` | 客户端调用 | `web/src/api/auth.ts`；`SettingView.vue` 也直接调用。 |
| `POST /api/v1/auth/login` | 客户端调用 | `web/src/api/auth.ts`。 |
| `POST /api/v1/auth/forgot` | 客户端调用 | `forgotPassword` 请求邮件重置口令。 |
| `POST /api/v1/auth/reset` | 客户端调用 | `resetPassword` 验证一次性口令并更新密码。 |
| `POST /api/v1/auth/logout` | 客户端调用 | `web/src/api/auth.ts`。 |
| `GET /api/v1/auth/me` | 客户端调用 | `web/src/api/auth.ts`；`AdminView.vue`、`SettingView.vue`。 |
| `GET /api/v1/content/posts` | 客户端调用 | `web/src/api/content.ts` `listPosts`。 |
| `GET /api/v1/content/archive` | 客户端调用 | `listArchive`。 |
| `GET /api/v1/content/comments/recent` | 客户端调用 | `listRecentComments`。 |
| `GET /api/v1/content/pages` | 客户端调用 | `listPages`。 |
| `POST /api/v1/content/pages` | 客户端调用 | `savePage`。 |
| `GET /api/v1/content/pages/{slug}` | 客户端调用 | `getPage`。 |
| `PUT /api/v1/content/pages/{id}` | 客户端调用 | `savePage`。 |
| `DELETE /api/v1/content/pages/{id}` | 客户端调用 | `deletePage`。 |
| `GET /api/v1/content/terms` | 客户端调用 | `listTerms`。 |
| `POST /api/v1/content/terms` | 客户端调用 | `createTerm`。 |
| `PUT /api/v1/content/terms/{id}` | 客户端调用 | 管理端分类与标签行可重命名，关系保留。 |
| `DELETE /api/v1/content/terms/{id}` | 客户端调用 | `deleteTerm`。 |
| `GET /api/v1/content/posts/{slug}` | 客户端调用 | `getPost`。 |
| `GET /api/v1/content/posts/{id}/revisions` | 客户端调用 | 管理端编辑文章时读取历史版本。 |
| `POST /api/v1/content/posts/{id}/revisions/{revisionID}/restore` | 客户端调用 | 管理端恢复选定的文章版本。 |
| `GET /api/v1/content/posts/{slug}/comments` | 客户端调用 | `listPostComments`。 |
| `POST /api/v1/content/posts/{slug}/comments` | 客户端调用 | `createComment`。 |
| `GET /api/v1/content/comments` | 客户端调用 | 管理端按 `pending`、`approved` 或 `spam` 状态分页。 |
| `PUT /api/v1/content/comments/{id}` | 客户端调用 | `moderateComment`。 |
| `DELETE /api/v1/content/comments/{id}` | 客户端调用 | `deleteComment`。 |
| `GET /api/v1/media` | 客户端调用 | `listMedia`。 |
| `POST /api/v1/media` | 客户端调用 | `uploadMedia`。 |
| `DELETE /api/v1/media/{id}` | 客户端调用 | `deleteMedia`。 |
| `GET /media/{id}` | 客户端调用 | 不是 `/api/` 路径。`publicMedia` 把 `url` 写成 `/media/` 加 id；`AdminView.vue` 用该字段做 `<img>` 和链接。`web/src` 里没有字面量 `/media/`。 |
| `POST /api/v1/content/posts` | 客户端调用 | `savePost`。 |
| `PUT /api/v1/content/posts/{id}` | 客户端调用 | `savePost`。 |
| `DELETE /api/v1/content/posts/{id}` | 客户端调用 | `deletePost`。 |
| `GET /api/v1/dashboard/stats` | 客户端调用 | `web/src/api/settings.ts` `getDashboardStats`。 |
| `GET /api/v1/settings/site` | 客户端调用 | `getSite` 只给 owner。设置页通过该接口读取包含管理邮箱的完整站点设置。 |
| `PUT /api/v1/settings/site` | 客户端调用 | `updateSite`；`SettingView.vue` 也直接 `putJSON`。 |
| `GET /api/v1/settings/reading` | 客户端调用 | `getReading`。 |
| `PUT /api/v1/settings/reading` | 客户端调用 | `updateReading`。 |
| `PUT /api/v1/settings/account` | 客户端调用 | `updateAccount`。 |
| `GET /api/v1/settings/users` | 客户端调用 | `listUsers`。 |
| `POST /api/v1/settings/users` | 客户端调用 | `createUser`。 |
| `PUT /api/v1/settings/users/{username}` | 客户端调用 | `updateUser`。 |
| `DELETE /api/v1/settings/users/{username}` | 客户端调用 | `deleteUser`。 |
| `GET /api/v1/audit` | 客户端调用 | `listAudit`；设置页支持翻页并显示总数。 |
| `GET /api/v1/mail` | 客户端调用 | 所有者在管理端查看分页站内通知。 |
| `GET /api/v1/backup` | 客户端调用 | 所有者下载包含数据库快照和登记媒体的 ZIP 备份。 |

匿名和登录用户从公开的 `system/status` 读取站点展示信息与阅读设置；管理邮箱只通过 owner-only 的 `GET /api/v1/settings/site` 返回，避免从公开状态接口泄露。

## 文档与代码不一致（历史问题，已复核修复）

原审查发现以下说法不准确；截至 2026-10-12，文档已与当前实现一致：

1. RSS 只包含已发布文章，当前会按每页 100 条读取全部结果；站点地图包含首页、文章、公开分类/标签和独立页面；`robots.txt` 禁止 `/admin` 与 `/setting`，并在站点 URL 已配置时附上站点地图地址。实现位于 `internal/httpapi/server.go`，`TestPublishedOrderDraftsFeedAndRobots` 验证 RSS 跨过 100 条分页边界并返回全部 103 篇文章。README 和架构说明描述了这些边界。
2. 文章接口的 `categories`、`tags` 是 JSON 字符串数组；逗号拆分仅在管理端表单执行。README 与架构说明已经明确写出该请求格式。
3. README 的文章查询参数现列出 `q`、`category`、`tag`、`month`、`order`、`page` 和 `pageSize`，与 API 和 Vue 客户端一致。

## 改进

### 1. 读者端的“最新”与发布日期不是同一套时间

当前行为：`GET /api/v1/content/posts` 在未传 `order` 时按 `updated_at DESC, id DESC` 排序（`internal/store/content.go` 的 `SearchPosts`）。读者首页 `PostListView.vue` 不传 `order`，卡片日期用 `post.updatedAt`。文章页 `PostDetailView.vue` 同样显示 `updatedAt`。侧栏“最新文章”才传 `order=published`（`UserView.vue`）。邻居和月份归档用 `published_at`（`internal/service/archive.go`）。编辑已发布文章会刷新 `updated_at`，但保留原来的 `published_at`（`SavePost` 只在发布且发布时间为空时写入发布时间）。

改完后可见结果：首页列表、卡片日期和侧栏按同一次发布时间排列；只改正文不再把旧文顶到首页第一位。

路径：`internal/store/content.go`，`web/src/views/PostListView.vue`，`web/src/views/PostDetailView.vue`，`web/src/views/UserView.vue`。

截至 2026-10-12，公开文章默认按发布时间和 ID 排序，读者列表、文章日期及“最新文章”侧栏使用同一发布时间；编辑正文不会把旧文章顶到首位。`TestPublishedOrderDraftsFeedAndRobots` 覆盖排序和更新时间不覆盖首次发布时间。

未上线可接受的破坏性变更：若改掉服务端默认排序，现有调用方会看到不同顺序。只改 Vue、让首页传 `order=published`，则 API 默认值不变。

### 2. 文章列表不看 `includeDraft`，页面和词条要看

当前行为：`listPosts` 把 `s.canEditContent(r)` 直接当成 `includeDrafts`（`internal/httpapi/content.go`）。owner 或 editor 的会话访问读者首页时，草稿会出现在列表和侧栏里。匿名会话看不到草稿。`listPages` 和 `listTerms` 只有查询参数 `includeDraft=true` 且调用者是内容编辑时才含草稿。`getPost` 对内容编辑返回草稿，这和架构文档里的预览约定一致，应保留，否则管理端的“预览”链接会变成 404。

改完后可见结果：登录的编辑打开 `/user` 时只看到已发布文章；`/admin` 因为已经传 `includeDraft=true`，仍然看得到草稿。预览链接继续能打开草稿。

截至 2026-10-12，公开列表默认不向 owner/editor 暴露草稿；明确带 `includeDraft=true` 时才返回管理预览所需的草稿，草稿详情仍允许内容编辑者预览。API 回归覆盖匿名、owner 和 editor 的可见性。

路径：`internal/httpapi/content.go` 的 `listPosts`、`getPost`；对照 `listPages`。

### 3. 列表和 RSS 带出整篇正文，RSS 只有 20 条

当前行为：`SearchPosts` 的 SELECT 包含 `content`，`publicPost` 原样放进列表 JSON。一篇正文最多 500000 个字符。读者卡片只用摘要，没有摘要时截取原文前 120 个字符（`PostListView.vue`），Markdown 记号会露在摘要里。`feed` 固定取更新时间排序的第 1 页 20 篇；摘要为空时把整篇 `Content` 放进 `<description>`。

改完后可见结果：文章列表响应不再带正文，首页传输量与正文长度脱钩。RSS 按发布时间给出摘要，空摘要时截断，并且不只锁在 20 篇更新序的第一页。

路径：`internal/store/content.go`，`internal/httpapi/server.go` 的 `publicPost` 与 `feed`，`web/src/views/PostListView.vue`。

未上线可接受的破坏性变更：从列表 JSON 去掉 `content` 会破坏依赖该字段的调用方。详情接口 `GET /api/v1/content/posts/{slug}` 应继续返回正文。

截至 2026-10-12，文章列表只返回摘要而不加载正文；正文搜索使用词元索引，RSS 按发布时间读取所有分页并优先使用摘要、为空时截断正文。`TestPublishedOrderDraftsFeedAndRobots` 验证列表不带正文以及 RSS 跨过 100 条边界。

### 4. 关键词和文档标题没有进入 HTML

当前行为：站点 `keywords` 能保存，并出现在 `system/status` 的 `site` 里（`SettingView.vue` 有输入框）。`web/index.html` 的 `<title>` 固定为 `Typecho Next`，没有 description 或 keywords。Vue 没有改 `document.title`。`keywords` 在 `web/src` 里只出现在类型、设置表单和保存请求中。

改完后可见结果：读者打开首页和文章时，浏览器标题、描述和关键词随站点和文章变化；查看源代码或禁用脚本时也能看到站点名，而不是固定的 “Typecho Next”。

路径：`web/index.html`，`web/src/views/UserView.vue`，`web/src/views/SettingView.vue`，`internal/httpapi/server.go` 的 `publicSite`。

截至 2026-10-12，服务端按站点、文章或页面填充 title、description、keywords、Open Graph、canonical 和 robots 元数据；客户端挂载后继续同步标题。公开 HTML 回归覆盖特殊字符转义、站点元数据及草稿预览的 `noindex`。

### 5. robots.txt 允许抓取管理和设置页

当前行为：`robots` 对所有用户代理写 `Allow: /`。有站点 URL 时再加一行 Sitemap。`/admin` 和 `/setting` 因此也在允许范围内。

改完后可见结果：公开的 robots 文本拒绝 `/admin` 和 `/setting`，同时保留 `/user`、`/page/` 和站点地图。

路径：`internal/httpapi/server.go` 的 `robots`。

截至 2026-10-12，`robots.txt` 保留公开抓取、禁止 `/admin` 与 `/setting`，并在站点 URL 已配置时附上 sitemap；`TestPublishedOrderDraftsFeedAndRobots` 覆盖这些规则。

### 6. 认证主体已在，登录与会话的运维旋钮不够

当前行为：密码用 bcrypt，失败返回同一个 `invalid_credentials`（`internal/httpapi/auth.go` 的 `login`）。没有失败次数或间隔限制。`SessionTTLMin` 在 `internal/config/config.go` 里固定为 `60 * 24`，`.env.example` 不能配置它。过期会话只在签发新会话时清理（`issueSession` 调用 `PurgeSessions`）。`requestLog` 记录方法和路径，不记录状态码。响应没有 `X-Content-Type-Options` 一类安全头。CSRF、HttpOnly 会话和角色检查已经存在，见本文开头的表面核对。

改完后可见结果：连续错误密码在一段时间内收到 429。会话寿命可以由环境变量改变。访问日志能看出 4xx 和 5xx。浏览器收到 nosniff 等响应头。现有登录成功路径仍然签发 `app_session`。

路径：`internal/httpapi/auth.go`，`internal/config/config.go`，`internal/httpapi/server.go` 的 `requestLog` 与 `issueSession`，`.env.example`。

截至 2026-10-12，已加入登录失败限流、可配置会话期限、带状态码的请求日志和浏览器安全响应头；API 错误继续提供稳定 `code`，已知英文消息会本地化，未映射消息按错误码返回中文兜底。`messages_test.go` 覆盖已映射与未映射错误。

### 7. 非法时区会被保存，归档再悄悄改用 UTC

当前行为：`UpdateSite` 只在时区非空时原样写入（`internal/service/account.go`）。`locationOrUTC` 在 `time.LoadLocation` 失败时返回 UTC。设置页的下拉框只有四个合法时区，但 API 接受任意字符串。`internal/httpapi/public_reading_test.go` 把 `Not/ARealZone` 存进去，再按 UTC 断言归档。

改完后可见结果：保存站点时，无法加载的 IANA 名称返回 422 `validation_error`，库里的时区字段保持上一次的合法值。空时区继续表示 UTC。

路径：`internal/service/account.go`，`internal/service/archive.go` 的 `locationOrUTC`。

未上线可接受的破坏性变更：这是收紧校验。本文不给出改写已有库里非法时区字符串的步骤。

截至 2026-10-12，初始化和站点更新都会校验 IANA 时区名称，初始化时把时区输入去除首尾空格后再保存；更新接口对无效时区返回 422，并保留原有设置。

### 8. 就绪检查与 HTTPS cookie 配置

当前行为：`/healthz` 在未初始化时也返回 200 `status=ok`（`health`）。`/readyz` 在初始化前返回 503。`FromEnv` 仅在该变量等于 `true` 时给会话和 CSRF cookie 加 `Secure`。

改完后可见结果：服务在初始化前会显示未就绪。经过 HTTPS 终结的部署发出的 cookie 带 `Secure`。`/healthz` 仍表示进程活着。

路径：`internal/httpapi/server.go` 的 `health` 与 `ready`，`internal/config/config.go`，`.env.example`。

截至 2026-10-12，`/healthz` 表示进程存活，`/readyz` 在初始化前返回 503；`APP_COOKIE_SECURE` 控制会话和 CSRF cookie 的 Secure 属性，配置测试覆盖精确值边界，TLS HTTP 测试验证两个 cookie 的实际 Secure/HttpOnly 属性。

### 9. 读者首页的主文案与导航选中态是写死的

当前行为：站点描述已经渲染在 `UserView.vue` 的 `.site-description`。其下 `PostListView.vue` 的英雄区固定为 “A NEW PUBLISHING CORE” 和 “这是全新 Go + Vue 架构的用户端”。顶栏“阅读”始终带 `active`，文章页和独立页面也一样。

改完后可见结果：首页主文案使用站点名称和描述。人在 `/page/...` 或某篇文章时，“阅读”不再保持选中，当前页面链接可以辨认。

路径：`web/src/views/PostListView.vue`，`web/src/views/UserView.vue`。

截至 2026-10-12，读者首页主标题与说明来自站点设置；“文章”链接只在 `/user` 列表页处于当前态，独立页面会标记对应页面链接。Playwright 阅读首页及页面详情流程覆盖这些路由状态。

### 10. 新库仍创建空的 `admins` 表；分页上限写错时会缩成 20 条

当前行为：`migrate()` 仍执行 `CREATE TABLE IF NOT EXISTS admins`。初始化只向 `users` 插入 owner（`internal/store/accounts.go` 的 `Setup`）。`Admin()` 只读 `users` 里的 owner。`pagination` 在 `pageSize` 大于 100 时改成 20，而不是拒绝或钳到 100。版本 3 到 8 以及 `ensureReadingColumns`、`ensureSiteProfileColumns`、`ensureSessionUserColumn` 会在打开数据库时执行；这是本应用自己的升级，应保留。

改完后可见结果：新创建的数据库文件里没有 `admins` 表，owner 仍然在 `users`。请求 `pageSize=101` 得到 422，或稳定地得到 100 条，而不是 20 条。

路径：`internal/store/schema.go`，`internal/store/accounts.go`，`internal/httpapi/server.go` 的 `pagination`。

未上线可接受的破坏性变更：从新库建表语句去掉 `admins`。本文不给出改写已有数据库文件的步骤，也不建议删除列升级代码。

截至 2026-10-12，当前 schema 不创建 `admins` 表，空数据库一次性创建所有当前列并记录 `user_version`；旧的非空无版本数据库会被明确拒绝。`pageSize` 超出范围、页码为 0/负数/非整数或组合后 offset 溢出时返回 422；`pagination_test.go` 覆盖边界。

## 完善

这些能力在 API 里已经有，客户端只用了其中一截。

### 1. 管理端列表停在第一页，每类最多 100 条

当前行为：`AdminView.vue` 的 `loadDashboard` 对文章、页面、待审核评论、媒体和词条都请求 `page=1`、`pageSize=100`，界面不读 `total`，没有下一页。第 101 篇文章无法在管理端编辑或删除。API 的 `page` 和 `total` 已经返回（`publicPostPage` 等）。

改完后可见结果：管理端可以翻到第 2 页及以后，并显示总数。

路径：`web/src/views/AdminView.vue`，`web/src/api/content.ts`。

截至 2026-10-12，文章、页面、媒体、词条和评论列表都有按接口 `total` 翻页的控件。文章状态筛选由 API/SQL 在分页前执行，`status=draft` 要求内容编辑权限；`post_filter_test.go` 覆盖筛选、非法状态和匿名权限，Playwright 用 101 篇草稿验证过滤后的总数与第二页内容。

### 2. 词条可以改名的接口没有界面

当前行为：`PUT /api/v1/content/terms/{id}` 由 `updateTerm` 处理，并写 `term.updated` 审计。`web/src/api/content.ts` 只有 `createTerm` 和 `deleteTerm`。管理端每一行只有删除。`post_terms.term_id` 引用 `terms(id)` 且 `ON DELETE CASCADE`（`internal/store/schema.go`）。删掉一个分类会去掉文章上的这个关联，文章本身还在。

改完后可见结果：编辑可以改分类或标签的名称，已关联的文章仍然带着该词条。删除仍要确认。

路径：`internal/httpapi/content.go` 的 `updateTerm`，`web/src/api/content.ts`，`web/src/views/AdminView.vue`，`internal/store/schema.go`。

### 3. 媒体库和 Markdown 编辑器没有接上

当前行为：编辑者可以上传 JPEG、PNG、GIF、WebP，单文件 10 MiB，公开地址是 `/media/{id}`（`uploadMedia`、`publicMedia`）。管理端网格能看能删。文章和页面编辑器是纯 `<textarea>`，没有预览，也没有把媒体地址插入正文的动作。读者端用 `marked` 加 `DOMPurify` 渲染（`PostDetailView.vue`、`PageDetailView.vue`）。

改完后可见结果：在编辑器里插入一张已上传图片后，正文出现指向 `/media/{id}` 的 Markdown 图片；保存前能看到与读者端相同的渲染结果。

路径：`web/src/views/AdminView.vue`，`internal/httpapi/content.go` 的 `uploadMedia`，`internal/httpapi/server.go` 的 `publicMedia`。

截至 2026-10-12，文章和页面预览共用净化后的 Markdown 渲染器；媒体可以在光标或选中文本处插入到文章/页面正文，并选中图片说明供编辑。Playwright 验证插入位置、文章和页面预览中的图片地址与替代文本。

### 4. 评论审核和读者评论都只拿第一页

当前行为：管理端 `listPendingComments(1, 100)`，只显示待审核。`GET /api/v1/content/comments` 接受 `status=pending|approved|spam`（`ListAllComments`）。垃圾和已通过的评论没有管理列表。读者端 `listPostComments(postSlug, 1, 100)`，标题用 `comments.length`，不使用 `total`。超过 100 条已通过评论时，后面的楼层不出现，回复线程也不完整。

改完后可见结果：审核员可以按三种状态翻页。文章页在评论超过 100 条时可以继续加载，计数与 `total` 一致。

路径：`web/src/views/AdminView.vue`，`web/src/views/PostDetailView.vue`，`web/src/api/content.ts`，`internal/service/engagement.go` 的 `ListAllComments`。

截至 2026-10-12，管理端按状态分页，读者端支持“加载更多”并按 `total` 显示进度。`TestPublicCommentPaginationKeepsPageBoundariesAndReplies` 用 205 条评论验证三页边界、无重复以及跨页父评论 ID。

### 5. 审计、导航页面和筛选词条也被第一页截断（已实现）

当前行为：`SettingView.vue` 的 `loadAudit` 请求 `/api/v1/audit?page=1&pageSize=20`，`listAudit` 虽接受页码，界面不用。读者导航 `listPages(false, 1, 50)`，第 51 个已发布页面没有入口。筛选条 `listTerms(false, 1, 100)`，更早的词条不能从首页点到。

改完后可见结果：操作记录、顶部页面导航和分类筛选都能到达第一页之后的记录。

路径：`web/src/views/SettingView.vue`，`web/src/views/UserView.vue`，`web/src/views/PostListView.vue`，`web/src/api/settings.ts` 的 `listAudit`。

截至 2026-10-12，操作记录有翻页控件；读者端的页面导航与分类/标签筛选按批次加载，使用者可以继续载入下一批，不会把大列表一次性铺满页面或静默截断。Playwright 通过 API 第 6 页和分类第 2 页的记录验证继续加载和链接可达。

### 6. 接口错误是英文，界面是中文

当前行为：`writeError` 的 `message` 来自 Go 里的英文字符串，例如 “Sign in to continue”、“Owner permission is required”。Vue 在 `ApiError` 时直接显示 `reason.message`（`AdminView.vue`、`PostDetailView.vue`、`SettingView.vue`）。网络失败才用中文兜底。

改完后可见结果：读者和管理员看到的校验、权限和未登录提示与界面语言一致。`error.code` 保持稳定，调用方仍能分支。

路径：`internal/httpapi/server.go` 的 `writeError`，`web/src/api.ts`。

截至 2026-10-12，常见英文错误映射为中文，未映射消息按稳定错误码提供中文兜底；调用方仍可按 `error.code` 分支。`messages_test.go` 覆盖映射和兜底。

未上线可接受的破坏性变更：改掉英文 `message` 会影响对照该字符串的客户端。保留 `code` 则机器可识别的部分不变。

## 拓展

下列都是现在没有的产品能力。涉及表结构的条目只面向新库，不附带迁移步骤。

### 1. 公开页在脚本运行前是空壳

当前行为：`frontend` 对 `/user`、`/page/...` 返回同一份 `index.html`。该文件的 body 只有 `<div id="app">`。文章正文要等 Vue 请求 API 后才出现。站点地图已经列出这些 URL，抓取器若只看 HTML 则看不到正文。

改完后可见结果：对 `/user/{slug}` 和 `/page/{slug}` 的一次 GET 在 HTML 里包含标题和正文。管理端仍可以是登录后的客户端。

路径：`internal/httpapi/server.go` 的 `frontend`，`web/index.html`。

未上线可接受的破坏性变更：公开路由从“始终返回同一壳”改成服务端输出文章 HTML，会改变这些 URL 的响应体。`/api/v1` 可以保持不动。

截至 2026-10-12，公开文章与页面的初始 HTML 已由 Go 服务渲染 Markdown 标题、段落、链接和图片，浏览器脚本运行前即可读取格式化正文。Goldmark 默认安全渲染器过滤原始 HTML 和危险 URL；`TestPublicShellRendersSafeMarkdownWithoutJavaScript` 验证正文语义标记与恶意输入边界。口令文章和未来文章继续通过服务层公开可见性检查，不会在匿名 HTML 中暴露正文。

### 2. 搜索扫整篇正文

当前行为：搜索词被转成带转义的 `LIKE`，同时匹配标题、摘要、正文和 slug（`SearchPosts`、`SearchPages`）。`%` 和 `_` 不当作通配符，这是当前约定，应保留。正文列最长 500000 字符，没有全文索引。SQLite 连接 `MaxOpenConns` 为 1（`NewSQLiteRepository`），适合当前单进程。

改完后可见结果：按词搜索文章时不需要每行扫描整篇 `content`，含特殊字符的搜索词仍然按字面匹配。

路径：`internal/store/content.go`，`internal/store/schema.go` 的 `NewSQLiteRepository`。

未上线可接受的破坏性变更：为新库增加全文索引是 schema 增量。本文不给出把现有数据库文件改造成该索引的步骤。

截至 2026-10-12，文章和页面搜索使用 `search_tokens` 索引；新建文章和页面时会写入对应词元，正文搜索由词元索引执行。候选词元在一次分组查询中读取；所有搜索接口把 `q` 限制为最多 200 个 Unicode 字符。旧数据库不回填索引，部署 schema 不兼容版本时应重新初始化数据库。

### 3. 管理员邮箱只被保存

当前行为：`sites.admin_mail` 在初始化时写入，设置页可以修改，`UpdateSite` 检查它是合法邮箱。仓库里没有发信代码；评论、审核和密码重置都不发邮件。

改完后可见结果：新评论进入待审核时，所有者邮箱收到一封通知。忘记密码可以通过该邮箱完成重置，而不是只能由另一位 owner 在用户表里改密码。

路径：`internal/service/account.go` 的 `UpdateSite`，`internal/store/schema.go` 的 `sites`，`web/src/views/SettingView.vue`。

截至 2026-10-12，待审核评论会持久化到所有者的站内通知历史；SMTP 已配置时也会发送邮件。SMTP 发送失败不会撤回已保存评论，通知仍保留，失败原因写入服务日志。`TestPendingCommentSurvivesSMTPFailureAndKeepsInboxNotice` 覆盖发送与失败路径。

### 4. 发布只有草稿和立即公开

当前行为：文章 `status` 只能是 `draft` 或 `published`（建表 CHECK 与 `validatePost`）。撤回到草稿会把 `published_at` 清掉（`SavePost`）。没有定时公开，也没有修订记录。

改完后可见结果：可以选择一个将来的发布时间，到点之前匿名读者看不到该文；再次公开同一篇文章时，第一次公开的时间还在。历史版本可以对比或恢复。

路径：`internal/store/schema.go` 的 `posts`，`internal/store/content.go` 的 `SavePost`，`internal/service/validation.go` 的 `validatePost`。

未上线可接受的破坏性变更：放宽 `status` 的 CHECK 或增加修订表都是 schema 变更。本文不给出改写已有库的步骤。

截至 2026-10-12，文章可设置未来 `publishAt`，公开查询会按完整时间精度隐藏尚未到期的内容；编辑历史会保存并支持管理端恢复。`TestFuturePublishRevisionsPassphraseCommentsMailAndBackup`、发布时间边界测试和 Playwright 的修订恢复流程覆盖这些行为。

### 5. 文章自定义字段与口令保护（已实现）

当前行为：文章可通过 `postRequest.customFields` 保存字符串映射，单篇最多 50 项，字段名最多 80 个字符、字段值最多 2,000 个字符，合计最多 20,000 个字符。修订保存字段快照；更新省略该属性时保留原值，提交空对象时清空。文章列表不返回字段，文章详情与正文共享可见性；未通过口令校验的响应同时隐藏 `content` 和自定义字段。管理端可以增删字段并与修订一起恢复，读者端以文本展示字段值。

验证包括真实 SQLite/API 测试和 Playwright 编辑、展示及修订恢复流程。

路径：`internal/httpapi/server.go` 的 `postRequest` 与 `publicPost`，`internal/store/schema.go` 的 `posts` 和 `post_revisions`，`web/src/views/AdminView.vue`、`web/src/views/PostDetailView.vue`。

本次调整后不再升级早期 Go schema；旧数据库会在修改前被拒绝。新站点使用新的空 `APP_DATA_DIR`，当前数据库要求 `user_version=2` 并通过结构校验，具体条件见 [operations.md](operations.md)。

### 6. 没有持续集成，也没有备份入口

当前行为：`.github/workflows/` 是空目录。`Makefile` 有 `dev`、`web-build`、`api-run`、`build`、`clean`。测试在 `internal/httpapi`、`internal/service`、`internal/store`，要靠人手执行 `go test`。`clean` 会删除本地 `data`。没有导出或备份 API。数据就是 `APP_DATA_DIR/app.db` 和 `APP_MEDIA_DIR` 下的文件。

改完后可见结果：推送后自动跑 `go test ./...` 和 `web` 下的 `npm run build`。所有者可以下载一份包含数据库和媒体的备份，而 `make clean` 仍只影响本机目录。

路径：`.github/workflows/`，`Makefile`，`cmd/server/main.go`。

截至 2026-10-12，Windows CI 会运行 Go 测试、竞态检查、静态检查、漏洞扫描、Web 依赖审计及 Playwright 验收；所有者可下载包含一致性数据库快照和已登记媒体的 ZIP 备份。`tasks.ps1 clean` 保留博客数据，备份失败与恢复流程有集成测试。

### 7. 评论没有自动节流

当前行为：匿名评论要 CSRF，内容 1–4000 字符，父评论必须是同一篇文章里已通过的评论，深度最多三层（`internal/service/discussion.go` 的 `buildComment`）。默认进入 `pending`。没有按 IP 或作者的频率限制，也没有自动标成 `spam` 的规则。审核状态本身已经存在。

改完后可见结果：同一来源在短时间内大量发表评论会收到 429，或新评论被标成 `spam` 而不会出现在公开列表。正常的一条评论仍按站点的审核开关进入 `pending` 或 `approved`。

路径：`internal/service/discussion.go`，`internal/httpapi/server.go` 的 `routeGuard`。

截至 2026-10-12，匿名评论按客户端来源每分钟限流，默认上限 30 次，可由 `APP_COMMENT_RATE_LIMIT` 调整；可信代理来源按配置解析。超限返回 429 `rate_limited`，现有 API 回归覆盖限流边界。

## 原审查时点的范围建议

- 不设计任意旧 SQLite、旧 PHP Typecho 或 `app.json` 的通用导入兼容层；旧 schema 不升级，新站点使用空目录初始化，详见 [operations.md](operations.md)。
- 不要求插件、主题运行时、XML-RPC，或与旧版固定链接一致。
- 不替换 Go、Vue 和 SQLite 这套栈。`Repository` 接口在 `internal/store/repository.go`。第二套数据库适配器不在本文范围内。
- 未知或不兼容的 SQLite schema 不在支持范围；程序会拒绝启动且不改动数据库。不要据此删除现有数据库，应按 [operations.md](operations.md) 中的步骤检查、备份或创建新数据目录。
