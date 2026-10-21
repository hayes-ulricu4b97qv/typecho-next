# 运行、备份与恢复

Typecho Next 是使用 Go、Vue 和 SQLite 的个人博客。原生开发与 Windows 发布包面向 Windows x64；Linux 服务器可使用仓库中的 Docker Compose 配置。数据和媒体保存在本地目录或 Docker 命名卷中。

## 构建与运行

### 下载包安装

从 GitHub Releases 下载 Windows x64 ZIP，解压到固定的应用目录（例如 `C:\TypechoNext\app`），运行 `start.cmd`。首次打开 `http://127.0.0.1:8081/setting` 创建所有者账户并完成站点设置。启动器默认将数据库和上传媒体分别放在 `%LOCALAPPDATA%\TypechoNext\data` 与其 `uploads` 子目录；两者位于应用目录之外，替换程序文件不会覆盖站点数据。需要迁移到其他磁盘或为服务账户指定数据位置时，可在启动前设置 `APP_DATA_DIR` 和 `APP_MEDIA_DIR`。

生产访问应由反向代理提供 HTTPS，并将流量转发到本机 `127.0.0.1:8081`。配置 `APP_COOKIE_SECURE=true`，并按反向代理说明设置可信代理地址。不要直接把应用的明文 HTTP 端口暴露到互联网。仓库提供了 [Caddyfile.example](../deploy/Caddyfile.example)；Caddy 与应用运行在同一台主机时，可使用以下配置：

```caddyfile
blog.example.com {
    encode zstd gzip
    header Strict-Transport-Security "max-age=31536000"
    reverse_proxy 127.0.0.1:8081
}
```

将 `blog.example.com` 换成已解析到服务器的域名，允许公网访问 TCP 80 和 443，让 Caddy 自动申请并续期 TLS 证书。应用进程设置 `APP_LISTEN_HOST=127.0.0.1`、`APP_PORT=8081`、`APP_COOKIE_SECURE=true` 和 `APP_TRUSTED_PROXIES=127.0.0.1/32`；若代理运行在另一台机器，只信任代理实际连接到应用时使用的固定地址或 CIDR。站点设置中的 URL 应填写 `https://blog.example.com`。反代配置不应把 `/api/`、`/media/` 或静态资源转发到其他服务。

### Linux Docker Compose 部署

仓库中的 [Dockerfile](../deploy/Dockerfile) 构建 Vue 静态文件和 Go 服务；[compose.yaml](../deploy/compose.yaml) 在 Linux 上启动应用与 Caddy。该方式从源码本机构建镜像，不依赖已发布的容器镜像。先将域名解析到服务器，并允许公网访问 TCP 80 和 443；在仓库根目录复制 `.env.example` 为 `.env`，将 `SITE_DOMAIN` 改成实际主机名（不带协议或路径），然后运行：

```sh
cp .env.example .env
docker compose --env-file .env -f deploy/compose.yaml up -d --build
```

首次打开 `https://实际域名/setting` 创建站点。应用只监听容器网卡，不发布宿主机端口；公网请求只进入 Caddy。Compose 默认使用 `172.29.240.0/24`，Caddy 和应用分别使用 `172.29.240.2` 与 `172.29.240.3`；应用只信任 Caddy 的单个容器地址。若该网段与宿主机冲突，在 `.env` 中设置 `TYPECHO_NETWORK_SUBNET`、`TYPECHO_CADDY_IP` 和 `TYPECHO_APP_IP`。两个容器地址必须位于所选子网内且互不重复；`APP_TRUSTED_PROXIES` 会随 Caddy 地址自动更新。可先运行 `docker compose --env-file .env -f deploy/compose.yaml config --quiet` 检查配置，再启动服务。

如需启用密码找回，将 SMTP 变量填写在 `.env` 中，再运行上述 Compose 命令。Compose 将 `APP_SMTP_*` 变量传入应用容器，并将 `SITE_DOMAIN` 传给 Caddy；Go 程序不会自行读取 `.env` 文件。保管好 `.env`，其中可能包含 SMTP 凭据。

数据库、上传媒体与应用日志保存在 `typecho-next-data` 命名卷；Caddy 的证书和配置分别保存在 `typecho-next-caddy-data`、`typecho-next-caddy-config`。`docker compose --env-file .env -f deploy/compose.yaml down` 会保留这些卷。删除卷会永久移除站点数据；升级前仍应从管理端下载备份，并将备份保存在服务器之外。设置 Compose 变量 `TYPECHO_DATA_VOLUME` 可让应用改用其他数据卷，默认仍为 `typecho-next-data`；可在仓库根目录的 `.env` 中保存该值。该变量只用于 Compose 卷名插值，不会传入应用容器。应用容器以非 root 的 UID 10001 运行，根文件系统只读，移除全部 Linux capabilities，并启用 `no-new-privileges`。备份快照和 ZIP 临时文件写入 `/var/lib/typecho` 数据卷，因此备份期间该卷需要额外容纳数据库快照和归档文件。

### 从源码构建

需要 Windows、PowerShell、Go 1.27.3 及以上版本、Node.js 22，以及已加入 PATH 的 MinGW-w64 GCC。SQLite 构建需要开启 CGO（`$env:CGO_ENABLED = '1'`）；发布打包还需要 MinGW-w64 Binutils 提供的 `objdump` 来检查可执行文件导入。在项目根目录执行：

```powershell
$env:CGO_ENABLED = '1'
.\tasks.ps1 build
.\bin\typecho-next.exe
```

构建脚本输出 `bin/typecho-next.exe` 和 `web/dist`。在项目根目录启动程序，访问 `http://127.0.0.1:8081/setting` 完成初始化。创建可分发的版本包时运行 `./tasks.ps1 package -Version 1.2.3`；命令生成版本化 ZIP，并在打包前构建前端和 Windows x64 程序。发布程序会静态链接 MinGW 运行库，因此目标机器无需安装 GCC。报告成功前会重新解压临时 ZIP，校验所需文件、`VERSION`、`BUILD.txt`、PE 架构为 Windows x64 且导入表没有 MinGW 运行库；随后在 `PATH` 仅包含 Windows `System32` 的环境下运行包内 `start.cmd`，初始化站点并登录，发布文章、上传图片、验证缺失媒体时备份失败、下载备份，再将备份恢复到新的目录并启动第二个包实例，确认会话、文章和图片仍可读取。全部检查通过后才移动到正式文件名，失败会清理临时包并报错。此 API 冒烟检查不替代正式标签工作流中的完整浏览器 E2E 和其他备份失败场景验收。

PowerShell 任务：`dev` 启动 Vite，`api-run` 启动 Go 服务，`web-build` 构建前端，`build` 构建前后端，`clean` 清理 `bin` 和 `web/dist`。Vite 开发和预览服务器默认只监听 `127.0.0.1`；需要局域网访问时，可在 `web` 目录运行 `npm run dev -- --host 0.0.0.0`。例如 `.\tasks.ps1 api-run`。开发时在另一个终端执行 `.\tasks.ps1 dev`。

程序从环境变量读取配置，**不会自动加载 `.env` 文件**。`.env.example` 是配置参考；通过 PowerShell 或进程管理工具设置变量，例如 `$env:APP_DATA_DIR = 'C:\Typecho\data'`。可使用 Windows 任务计划程序配置启动与重启，并设置正确工作目录。程序不是直接注册到 Windows 服务控制管理器的服务，需要使用兼容的服务包装器才能按 Windows 服务运行。

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `SITE_DOMAIN` | 未设置（Docker Compose 必填） | Caddy 对外提供 HTTPS 的主机名，不含协议和路径 |
| `TYPECHO_DATA_VOLUME` | `typecho-next-data` | Docker Compose 的应用数据卷名称 |
| `APP_PORT` | `8081` | HTTP 监听端口 |
| `APP_LISTEN_HOST` | `127.0.0.1` | HTTP 监听网卡；同机反向代理可使用默认值，显式设为 `0.0.0.0` 才接受外部网卡连接 |
| `APP_DATA_DIR` | `./data` | SQLite 数据库目录 |
| `APP_MEDIA_DIR` | `APP_DATA_DIR/uploads` | 上传媒体目录 |
| `APP_WEB_DIR` | `./web/dist` | 前端构建目录 |
| `APP_VERSION` | 构建版本（本地源码默认为 `0.1.0-dev`） | 状态接口和日志显示的版本；发布包会嵌入包版本 |
| `APP_COOKIE_SECURE` | `false` | HTTPS 部署设置为精确的小写 `true`；其他非空值会告警并按 `false` 处理 |
| `APP_TRUSTED_PROXIES` | 未设置 | 可信反向代理的逗号分隔 CIDR；仅来自这些地址的 `X-Forwarded-For` 会参与来源识别 |
| `APP_SESSION_TTL_MIN` | `1440` | 会话有效期（分钟）；非正值、格式错误或超出 `time.Duration` 范围时回退到 1440 |
| `APP_WRITE_TIMEOUT` | `5m` | HTTP 响应总时限；应覆盖备份快照生成、压缩和下载所需时间 |
| `APP_LOGIN_FAILURE_LIMIT` | `5` | 同一客户端地址 10 分钟内的登录失败上限；成功登录会清零，超限后会在校验密码前拒绝请求 |
| `APP_COMMENT_RATE_LIMIT` | `30` | 同一来源每分钟的评论数上限 |
| `APP_PASSWORD_RESET_LIMIT` | `5` | 同一来源 15 分钟内的密码找回请求上限 |
| `APP_PASSWORD_RESET_ACCOUNT_LIMIT` | `1` | 同一有效账户 15 分钟内可发送的找回邮件上限；抑制跨来源邮件轰炸，超限时仍返回与未知账户相同的响应 |
| `APP_SMTP_HOST` | 未设置 | SMTP 主机；未设置时密码找回不可用 |
| `APP_SMTP_PORT` | `587`（STARTTLS）或 `465`（隐式 TLS） | SMTP 端口 |
| `APP_SMTP_USERNAME` / `APP_SMTP_PASSWORD` | 未设置 | 可选 SMTP 登录凭据，必须同时设置 |
| `APP_SMTP_FROM` | 未设置 | 发件地址；配置 SMTP 时必填 |
| `APP_SMTP_TLS_MODE` | `starttls` | `starttls` 或 `implicit`，连接必须使用 TLS |

数据库使用 `PRAGMA user_version` 标记 schema 版本。首次启动会在事务中创建当前 schema 并设定 `user_version=2`。已存在的数据库必须带有当前版本号并通过完整结构校验；旧 Go schema、未版本化的非空数据库和其他 schema 版本都不受支持，程序会在修改前拒绝启动。创建新站点时，将 `APP_DATA_DIR` 指向新的空目录并重新运行程序，再打开 `/setting` 完成站点与所有者初始化。不要为了绕过版本检查而删除或覆盖现有数据库。PHP 数据库和 `app.json` 导入也不受支持。数据库和媒体目录分开存储；新站点不会自动登记旧媒体文件。

媒体上传只接受图片头可解析为 JPEG、PNG、GIF 或 WebP 的文件。单文件上限为 10 MiB，最长边上限为 16,384 像素，总像素上限为 4,000 万；校验读取图片尺寸配置，不会完整解码像素数据。公开媒体 URL 只提供这四种 MIME；数据库中的媒体类型若不受支持，服务会拒绝读取并写入限频警告。同一服务实例同时只处理一个媒体上传，其他上传请求会返回 `503 media_busy` 和 `Retry-After: 5`，以限制 multipart 解析的并发内存占用。

删除媒体时，文件先暂存后删除数据库记录。若服务在两步之间退出，后续读取媒体 URL 时会根据数据库记录恢复文件，或清理已删除记录对应的暂存文件。

服务管理器的工作目录可能与项目目录不同，生产环境建议为三个目录变量设置绝对路径。运行账户需要读写数据库与媒体目录、读取前端目录。服务本身只提供明文 HTTP，默认仅监听 `127.0.0.1`；生产环境应由反向代理终止 HTTPS，再转发到 `127.0.0.1:8081`。不要把服务端口直接暴露到不可信网络。通过反向代理提供 HTTPS 时，设置 `APP_COOKIE_SECURE=true`，代理应保留请求的 Cookie 和 CSRF 请求头。仅在反向代理运行于其他主机或确实需要外部网卡连接时，将 `APP_LISTEN_HOST` 改为适当接口地址；网络 ACL 仍应限制来源。直接通过 HTTP 开发时使用 `false`。

服务以 JSON 格式将运行日志写入标准输出，同时追加到 `APP_DATA_DIR/app.log`。日志文件达到 5 MiB 后轮换为 `app.log.1`，仅保留这一份轮换文件；轮换通过重命名直接替换旧副本，失败时旧副本仍保留。日志不包含请求查询字符串。每个 HTTP 响应带有服务端生成的 `X-Request-ID`，对应的请求日志和实际输出的限频诊断日志也记录该 ID；如果故障在限频间隔后仍再次出现，下一条诊断日志会附带期间被抑制的数量。5xx 请求和处理器 panic 使用 `ERROR` 级别，服务端错误会在界面消息中显示编号，便于从错误提示定位日志行。panic 日志记录请求状态、耗时和调用堆栈，不记录 panic 内容；中间件随后使用 `http.ErrAbortHandler` 交由 Go HTTP 服务收尾，避免标准库再把原始 panic 值写入默认日志。运行账户需要能够在数据目录创建和轮换日志。启动时无法打开日志文件，程序会告警并继续运行，只保留标准输出日志；运行中轮换后暂时无法重开时，后续写入会重试。部分写入失败会尝试回滚；若回滚失败，下次写入会先轮换仍有内容的可疑活动文件，空文件不会覆盖唯一的 `app.log.1`。博客备份只包含数据库快照和已登记媒体，不包含运行日志。日志可能包含错误详情和资源路径，应与数据库一样限制数据目录访问；分享日志片段前应移除个人信息。

登录、评论和密码找回限流默认按 TCP 对端地址计数。若反向代理需要区分访客，应只把实际代理地址加入 `APP_TRUSTED_PROXIES`；服务仅信任这些代理转发的 `X-Forwarded-For`，不会采信其他来源自行提交的该请求头。无效 CIDR 会记录告警并忽略，因此应检查启动日志，确认所有配置值都有效。代理应覆盖或正确追加此头，避免客户端伪造地址。

`/healthz` 表示进程存活；`/readyz` 检查初始化状态及存储可用性，未完成初始化时返回 503。服务收到退出信号后最多等待 10 秒处理进行中的 HTTP 请求；超时会强制关闭活动连接。随后最多等待 12 秒让已启动的密码找回邮件任务结束，同时取消每日维护清理。邮件任务未能在期限内结束时会记录警告并以非零状态退出，方便服务包装器识别未完成的关机。

密码找回要求配置完整 SMTP 投递：STARTTLS 默认使用 587 端口，隐式 TLS 使用 465 端口。邮件发送到用户账户登记的邮箱，包含 30 分钟有效的一次性口令；发件凭据只从进程环境读取，不写入数据库或日志。未配置 SMTP 时，所有找回请求都返回相同的服务不可用响应。找回请求同时按来源和有效账户限流；每账户限流只在邮件成功投递后保留，投递失败可立即重试。

待审核评论会进入所有者的站内通知历史；SMTP 已配置时也会尝试发送到站点邮箱。站内通知接口仅所有者可读，支持分页，保留最近 90 天且最多 1000 条。服务启动时和之后每日会清理过期记录，新增通知时也会执行清理；读取通知列表只返回保留期内记录，不修改数据库。过期会话也在服务启动时和每日维护中清理；鉴权读取只判断会话有效性，不删除记录。邮件发送失败不会撤回已经保存的评论，失败原因只写服务日志，站内通知仍保留。

## 备份

所有者登录后，可以在管理页顶部下载备份，也可以使用带登录 Cookie 的 `GET /api/v1/backup`。备份接口只接受 GET；HEAD 请求会返回 405，避免为探测请求生成完整归档。ZIP 包含 `app.db` 和数据库快照引用的 `media/` 文件。尚未上传媒体的站点可以生成只有数据库的备份。临时上传文件和未登记的文件不包含在内。

管理页在支持文件保存 API 的浏览器中会先选择保存位置，再将 ZIP 响应流直接写入文件，避免把整个备份载入浏览器内存；接口错误会显示在页面并可重试。其他浏览器会在当前页面使用浏览器默认下载方式。

数据库使用 SQLite `VACUUM INTO` 生成一致快照，媒体依据快照中的记录归档。ZIP 完整生成后才开始下载；媒体缺失或无法读取时返回 `backup_failed`，不会将残缺备份作为成功下载。备份时避免删除媒体；数据库与媒体文件不是跨资源事务快照。临时目录需要容纳数据库快照和完整压缩包。同一服务实例在生成或传输备份期间只处理一个备份请求，其他请求返回 `503 backup_busy`，并建议 5 秒后重试。`APP_WRITE_TIMEOUT` 覆盖整个 HTTP 请求，包含快照生成、压缩和发送；客户端中止时，快照 SQL、归档查询和文件压缩会读取请求取消信号并尽早停止。备份较大或下载链路较慢时，应同时提高应用超时与反向代理超时，留出生成和完整传输的时间。

备份包含账户哈希、会话、评论邮箱等私密数据，应限制访问并保存在服务器之外。

## 恢复

1. 停止博客服务，保留当前数据和媒体目录的完整副本，以便回退。
2. 将备份解压到独立临时目录。确认有 `app.db`，媒体位于 `media/`。
3. 将解压得到的 `app.db` 放入新的空 `APP_DATA_DIR`，将 `media/` 的内容放入新的空 `APP_MEDIA_DIR`。没有媒体时创建空目录。不要将旧数据库的 `app.db-wal` 或 `app.db-shm` 混入恢复目录。
4. 配置服务使用这些新目录，确保运行账户拥有访问权限，再启动服务。
5. 检查 `/readyz`、所有者登录、文章正文、评论和已上传图片。必要时在站点设置中调整站点 URL。

恢复会回到备份时刻的账户、密码和会话状态。不要将数据库恢复到备份时刻却继续使用一份不匹配的媒体目录。

### Linux Docker Compose

Compose 恢复使用新建的数据卷，保留当前卷以便回退。以下命令在 Linux 服务器的仓库根目录运行；需要安装 `unzip`，并将 `restore_archive` 改成管理端下载的备份路径。先停止应用和反向代理，避免恢复期间仍有请求进入：

```sh
set -eu
restore_archive=/path/to/typecho-backup.zip
restore_dir=$(mktemp -d)
trap 'rm -rf "$restore_dir"' EXIT
unzip -t "$restore_archive"
unzip -q "$restore_archive" -d "$restore_dir"
test -s "$restore_dir/app.db"
mkdir -p "$restore_dir/uploads"
if [ -d "$restore_dir/media" ]; then
  cp -a "$restore_dir/media/." "$restore_dir/uploads/"
fi

docker compose --env-file .env -f deploy/compose.yaml down

restore_volume="typecho-next-data-restore-$(date +%Y%m%d-%H%M%S)"
if docker volume inspect "$restore_volume" >/dev/null 2>&1; then
  printf 'Restore volume already exists: %s\n' "$restore_volume" >&2
  exit 1
fi
docker volume create "$restore_volume" >/dev/null
docker run --rm --user 0:0 \
  --mount "type=volume,source=$restore_volume,target=/restore" \
  --mount "type=bind,source=$restore_dir,target=/source,readonly" \
  alpine:3.22 sh -ec '
    cp /source/app.db /restore/app.db
    mkdir -p /restore/uploads
    cp -a /source/uploads/. /restore/uploads/
    chown -R 10001:10001 /restore
  '
TYPECHO_DATA_VOLUME="$restore_volume" docker compose --env-file .env -f deploy/compose.yaml up -d --build
```

检查 `/readyz`、所有者登录、文章和图片后，再把恢复卷名设为日常 Compose 环境中的 `TYPECHO_DATA_VOLUME`。如果恢复命令失败或验收不正确，先运行 `docker compose --env-file .env -f deploy/compose.yaml down`，再用 `TYPECHO_DATA_VOLUME=typecho-next-data docker compose --env-file .env -f deploy/compose.yaml up -d --build` 启动原卷；如果原部署使用了自定义卷名，应填回原名。保留旧卷和恢复卷，直到恢复结果与新备份都确认可用。

## 升级与回滚

1. 记录当前应用版本，并在停机窗口前下载一份完整备份，确认 ZIP 可读且包含预期的数据库和媒体。
2. 停止服务进程。将当前应用目录改名为带版本号的备份目录，再把新版本解压到原应用路径；保留配置的数据和媒体目录（默认数据目录为 `%LOCALAPPDATA%\TypechoNext\data`）。
3. 启动新版本并检查 `/readyz`、所有者登录、文章、评论和图片。数据库 schema 变更只支持向前升级；启动失败时不要反复尝试或删除数据库。
4. 若新版本无法使用，停止服务，将应用目录恢复为旧版本，并按“恢复”步骤把升级前备份的数据库和媒体恢复到新的空目录，再配置旧版本使用这些目录。应用回退必须和升级前的数据备份配套，不能把已迁移的数据库直接交给旧程序。

每个版本的草稿 Release 应在发布前检查 ZIP 文件名、`VERSION`、启动器和前端页面，再由维护者发布。已发布版本不可通过重跑标签工作流替换；修复需要新的版本标签。

## 发布检查

可执行的 Windows 浏览器验收使用 `cd web` 后运行 `npm run test:e2e`，覆盖浏览器操作、权限、备份恢复和重启；说明见 [e2e-testing.md](e2e-testing.md)。

发布前在 Windows 执行 `go test ./...`、`go vet ./...`、`go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...` 和 `.\tasks.ps1 build`，并使用临时数据目录验收初始化、登录、文章发布、评论审核、图片上传和备份恢复。首次初始化后验证重启仍能读取数据与持久会话。

RC 应对应固定提交和明确版本号，例如 `0.1.0-rc.1`。版本标签工作流验证标签指向的提交，运行 CI，并创建或更新草稿 Release；带预发布后缀的标签会设置 GitHub prerelease 标记，手动发布后仍保持预发布语义。正式发布前应检查草稿包和自动生成的说明，再手动发布。`.\tasks.ps1 clean` 只清理程序和前端构建目录，保留博客数据；需要重新构建后才能运行完整页面。
