# 产品范围与发布约定

Typecho Next 面向希望自托管个人博客的站点所有者。一个实例服务一个站点，使用 SQLite 保存内容、账户和会话，使用本地目录保存图片。项目提供 Windows x64 原生发布包，以及从源码构建的 Linux Docker Compose 部署配置；暂不发布预构建容器镜像。

## 支持边界

- 支持 Windows x64 原生部署和 Linux Docker Compose 单站点自托管。
- 支持 SQLite 与本地媒体目录；站点数据独立于可替换的应用目录，容器部署使用独立命名卷。
- 支持通过反向代理提供 HTTPS。Windows 原生服务默认只监听本机回环地址；容器服务只发布给私有 Compose 网络。
- 不承诺 macOS 原生部署或多站点托管支持。
- 不支持导入旧 PHP Typecho 数据库或 `app.json`，也不承诺兼容未知 SQLite schema。
- 不包含主题或插件生态、多数据库后端、托管服务或自动公开站点功能。

这些边界用于让安装、备份和升级行为保持可预测。CI 会在 Linux runner 上解析 Compose 配置、校验 Caddyfile、构建部署镜像，并运行 `deploy/smoke-test.sh`。该检查禁用容器网络且不发布端口，验证首次初始化、媒体上传、命名卷跨容器重建后会话/文章/媒体仍可读取，以及包含图片的备份恢复到全新数据卷。检查使用单个小型 GIF，不覆盖大容量生产备份、磁盘故障或中断恢复。公网 DNS、Caddy ACME 证书签发与外部 HTTPS 验收仍是上线前门槛；当前站点保持离线，因此不会在 CI 或本地烟测中启动公网代理。

## 发布契约

每个版本使用不可变的 `vMAJOR.MINOR.PATCH` 标签；预发布版本可使用 `vMAJOR.MINOR.PATCH-rc.N`。标签必须指向已通过 CI 的精确提交。版本包使用 `typecho-next-vVERSION-windows-amd64.zip` 命名，`BUILD.txt` 记录版本和源码提交，并附许可证、安全政策和运维文档；GitHub 标签源码归档提供对应源码。

标签工作流运行可复用 CI，然后生成 Windows x64 ZIP 和 SHA-256 校验文件。预发布标签对应的草稿会设置 GitHub prerelease 标记，手动发布后仍显示为预发布版本；更新同一提交的草稿时也会校正该标记。工作流只创建或更新草稿 Release；已有正式发布的标签会被拒绝，不会覆盖资产。维护者检查草稿包、说明和校验值后手动发布。需要修正已发布版本时，使用新版本号和新标签。

## 发布验收

- CI 完成 Linux 容器镜像构建、Compose 配置解析与 Caddyfile 校验，以及 Go 格式、测试、race、vet、staticcheck、漏洞扫描、前端审计、代理测试和 Windows 浏览器验收。
- 发布包内含 `typecho-next.exe`、`start.cmd`、`VERSION`、`web/dist/index.html`、许可证、安全政策和运维说明。
- 打包任务在隔离临时目录启动 ZIP 内的 `start.cmd`，检查健康和初始化状态，完成建站、所有者登录、文章发布、媒体上传、备份恢复及恢复后重启，并确认数据库、媒体、会话和文章仍可读取。正式标签仍必须通过完整 CI（Windows 与 Linux 构建）和浏览器 E2E，并检查草稿 Release。
- 升级说明要求先备份数据库和媒体。需要回滚时，同时恢复旧程序和升级前的数据副本。
