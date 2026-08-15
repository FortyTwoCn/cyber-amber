# 赛博琥珀自助机

赛博琥珀自助机是一个面向 Linux 的 Go 服务：公开网页可以从普通 B站 UGC 视频生成指定时间段的优化 GIF；配置机器人账号后，服务可以持久化轮询“@我的”，解析评论命令，生成 GIF，并在同一视频下发布一条真正 `@` 发起者的一级带图评论。

项目当前版本为 `0.1.3`，module 为 `github.com/FortyTwoCn/cyber-amber`，使用 Go 1.26（开发与 CI 固定为 Go 1.26.6）。项目采用 GPL-3.0-only，原因与参考范围见 [docs/REFERENCES.md](docs/REFERENCES.md)。

## 能力

- BV、AV、完整链接、`b23.tv` 和分享文本；严格的重定向与 SSRF 防护。
- `MM:SS`、`HH:MM:SS`、全角字符和中文参数别名；所有 FFmpeg 参数都经过类型化校验。
- 多P解析、WBI key 缓存与签名、DASH 视频轨道选择和 CDN 域名校验。
- 仅下载与区间相交的约 6 分钟弹幕 segment；Protobuf 解码和 ASS 碰撞规避布局。
- FFmpeg 调色板生成/应用、无限循环、ffprobe 验证和固定顺序的自适应体积降级。
- SQLite WAL 持久化队列、持续租约恢复、带抖动有限重试、原子 Web 去重、跨任务渲染复用、缓存与磁盘配额清理。
- 原生 HTML/JavaScript、SSE 进度、移动端 UI、预览下载；生产不需要 Node.js。
- AES-256-GCM 账号凭据、Argon2id 管理密码、HMAC Session、CSRF、CSP 和限流。
- 图片上传、无 `root`/`parent` 的一级带图评论、真正的 `at_name_to_mid`、发布查重与可选原评论文字提示。
- JSON 日志、健康/就绪检查和 Prometheus 指标。

## 快速启动（Docker）

Linux 上可使用完整初始化脚本：

```bash
chmod +x deploy/docker/*.sh
./deploy/docker/prepare.sh
docker compose up -d
./deploy/docker/verify.sh
```

脚本会安全生成三个 secret、交互式生成管理员密码哈希、构建镜像并校验 Compose。详细说明、备份和升级流程见 `deploy/docker/README.md`。手动部署步骤如下：

1. 复制配置并保持 dry-run：

   ```bash
   cp config.example.yaml config.yaml
   # 将 paths/database 改成容器内 /data、/cache、/tmp/cyber-amber，或直接使用环境变量。
   ```

2. 为管理后台生成密码哈希和随机密钥。明文密码只经 stdin 进入一次性容器，不出现在进程参数中：

   ```bash
   mkdir -p secrets
   openssl rand -base64 32 > secrets/cookie_encryption_key
   openssl rand -base64 48 > secrets/session_secret
   docker build -t cyber-amber:local .
   printf '%s' '至少十二位的强密码' | docker run --rm -i cyber-amber:local -hash-password=- > secrets/admin_password_hash
   chmod 600 secrets/*
   ```

   compose 已把这三个文件映射为 Docker secrets，并把 `config.yaml` 只读挂载到 `/config/config.yaml`；`secrets/` 已加入 `.gitignore`，仍不得提交或粘贴其中内容。

3. 启动：

   ```bash
   docker compose up -d --build
   curl http://127.0.0.1:8080/healthz
   curl http://127.0.0.1:8080/readyz
   ```

4. 打开 `http://127.0.0.1:8080/`。机器人默认为 `BILI_DRY_RUN=true`；在管理页扫码登录或导入 Cookie 后，先用 dry-run 验证生成流程。只有为专用账号完成风险评估后才关闭 dry-run。

Docker 镜像安装 `ffmpeg`、libass、fontconfig、Noto CJK、CA 证书和时区数据，以 UID 10001 非 root 运行。compose 示例仅绑定本机回环地址。

## 本地构建

```bash
go mod tidy
go test ./...
go test -race ./...
go vet ./...
golangci-lint run ./...
CGO_ENABLED=0 go build -buildvcs=false -trimpath ./cmd/cyber-amber
```

运行主机必须提供 `ffmpeg`、`ffprobe`、`fc-match` 和 Noto Sans CJK 字体。缺少 `palettegen`、`paletteuse`、`subtitles`、可用解码器或中文字体时，`/readyz` 会失败，worker 不会认领任务。

## 配置

基础配置优先级为环境变量 > YAML > 内置安全默认值；管理端保存的非敏感运行参数会在打开数据库后覆盖对应基础值，并在下一次安全重启时生效。可在管理端删除这层覆盖，恢复环境变量/YAML。完整示例见 [`config.example.yaml`](config.example.yaml) 和 [`.env.example`](.env.example)。关键原则：

- `BILI_COOKIE` 非空时必须同时提供可解码为 32 字节的 `COOKIE_ENCRYPTION_KEY`。
- 数据库已有账号而主密钥丢失时启动会明确报告错误，不会生成新密钥覆盖旧凭据。
- 未配置 `ADMIN_PASSWORD_HASH` 和至少 32 字节的 `SESSION_SECRET` 时，管理 API 返回 `ADMIN_DISABLED`。
- 默认不回填首次启动前的通知，默认 dry-run，默认公开网页任务不发布 B站评论。

所有持续时间使用 Go duration，例如 `15s`、`10m`、`168h`。

## 评论命令

```text
@赛博琥珀自助机 10:10-10:20
@赛博琥珀自助机 10:10-10:20 fps=12 width=640 danmaku=on
@赛博琥珀自助机 01:10:10-01:10:20 p=2 resolution=480p danmaku=off
@赛博琥珀自助机 10：10～10：20 帧率=12 宽度=640 弹幕=开
@赛博琥珀自助机 10:10-10:20 分P=2 分辨率=480p 无弹幕
```

机器人身份判断使用通知来源、发送者 MID 和持久化账号 MID，不依赖昵称字符串。带图结果始终创建新一级主楼；可选提示才回复原评论，并且提示是纯文字。

## HTTP API

公开 API：

```text
POST /api/v1/videos/resolve
POST /api/v1/jobs
GET  /api/v1/jobs/{id}
GET  /api/v1/jobs/{id}/events
POST /api/v1/jobs/{id}/cancel
GET  /api/v1/jobs/{id}/artifact
GET  /healthz
GET  /readyz
GET  /metrics
```

管理 API 还提供登录/退出、账号扫码与 Cookie 导入及健康检查、通知游标、经完整校验并持久化的运行参数、暂停/恢复机器人、任务详情与重试、黑名单和审计日志。运行参数保存后明确返回 `restart_required`。错误统一为：

```json
{"error":{"code":"INVALID_TIME_RANGE","message":"结束时间必须晚于开始时间","request_id":"..."}}
```

公开响应不会包含 Cookie、SQL、磁盘路径、完整 FFmpeg 命令或内部诊断。

## 当前验证边界

- 已通过自动化单元与本地 `httptest` 集成测试的模块覆盖输入、WBI、通知结构、弹幕、ASS、FFmpeg 参数、队列、加密、评论 payload、HTTP 安全与管理登录。
- 测试永远不会调用真实 B站写接口。
- 没有真实 Cookie 和专用测试视频时，不能声称真实图片上传、评论可见性或审核状态已验证。API `code=0` 以及账号侧回读匹配都只记为 `published_unverified`；缺图或不匹配记为 `pending_review`。当前没有游客视角证据，因此不会误标 `published`。
- 2026-08-14 已用无账号真实只读接口复验直接 BV 输入、分P、WBI、DASH 与分段弹幕；本地 FFmpeg 媒体 E2E 已覆盖有/无弹幕、体积降级和取消。本执行环境把 `b23.tv` 解析到 RFC 2544 基准地址，严格 SSRF 策略因此拒绝实时短链访问；短链重定向由本地假服务器覆盖，尚未在可获得公网 DNS 的环境复验。认证通知、扫码确认和所有真实写入仍未验证。

生产运维见 [docs/OPERATIONS.md](docs/OPERATIONS.md)，安全模型见 [docs/SECURITY.md](docs/SECURITY.md)。
