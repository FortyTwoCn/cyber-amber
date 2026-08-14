# 运维手册

## 安装与首次启动

推荐 Docker Compose；首次部署可执行 `deploy/docker/prepare.sh`，启动后执行 `deploy/docker/verify.sh`，完整容器备份和升级步骤见 `deploy/docker/README.md`。原生 Debian/Ubuntu 需安装 `ffmpeg fontconfig fonts-noto-cjk libass9 ca-certificates tzdata`，创建低权限 `cyber-amber` 用户和 `/var/lib/cyber-amber`、`/var/cache/cyber-amber`。复制 systemd 与 nginx 示例，生成管理员哈希、Cookie AES key、Session secret，通过权限 0600 的 EnvironmentFile/secret 引用。

首次保持 `BILI_DRY_RUN=true`。访问管理页扫码登录；状态应显示登录、MID、昵称和最近检查时间，但不显示 Cookie。`/readyz` 全部通过后，用公开页面完成短片生成。只有专用测试视频与账号准备好后才评估关闭 dry-run。

管理页可保存默认参数、配额、保留期限、磁盘阈值、并发和 dry-run 等非敏感设置。保存时执行完整校验并写入 SQLite，页面显示“待重启生效”；使用正常 SIGTERM/Compose stop/systemctl restart 安全重启。选择“恢复部署配置”会删除数据库覆盖，下一次启动重新使用环境变量/YAML。路径、FFmpeg 可执行文件与秘密只能由部署配置修改。

## 升级与迁移

1. 备份 SQLite（WAL 模式下使用 `sqlite3 cyber-amber.db '.backup backup.db'` 或停止服务后同时保存 db/wal/shm）。
2. 拉取/构建新镜像，阅读 CHANGELOG。
3. 停止旧实例；启动时嵌入 migration 自动事务执行。
4. 检查 `/readyz`、日志、queue depth 和一个 dry-run 任务。

Migration 只前进。回滚二进制前必须确认旧版本理解新 schema。

## 备份与恢复

需要备份数据库、`/data/artifacts`（若希望保留下载）和外部 secret。密钥与数据库必须成对恢复；丢失 Cookie encryption key 无法恢复凭据，只能删除账号记录后重新登录。恢复后先强制 dry-run、检查账号与游标，再恢复轮询。

## 日志与监控

默认输出 JSON 到 stdout/journald。使用 request_id/job_id/notification_id 关联。抓取 `/metrics`，至少告警：readiness 失败、notification poll error 增长、queue depth 持续增长、失败率、B站风险熔断、磁盘余量。

systemd：`journalctl -u cyber-amber -f`。Docker：`docker compose logs -f cyber-amber`。

## 常见故障

- **Cookie 失效**：管理页显示未登录或 `-101`；任务会保留产物并停放，重新扫码/导入后再恢复机器人。无有效账号时恢复 API 会拒绝。失败的 Cookie 替换不会覆盖仍有效的旧会话。不要在日志/工单粘贴 Cookie。
- **HTTP 412/验证码**：`bot_state` 显示 `BILI_RISK_CONTROL`；停止自动写入，等待冷却并人工确认账号，禁止循环恢复。
- **FFmpeg 缺滤镜**：运行 `ffmpeg -filters | grep -E 'palette(gen|use)|subtitles'`，安装带 libass 的发行版包。
- **中文方框**：`fc-match 'Noto Sans CJK SC'`，刷新 `fc-cache -f`，确认容器 fonts-noto-cjk。
- **任务卡住**：检查 lease_until、FFmpeg 子进程和磁盘。每次认领都会恢复已过期租约，续租失败会取消本地工作；不要直接改状态，优先管理端重试。
- **磁盘不足**：缩短 `ARTIFACT_RETENTION` 或降低 `ARTIFACT_QUOTA_BYTES`，检查 `/data/artifacts` 与临时卷；清理器按过期时间/最旧产物删除数据库记录，并清理由崩溃遗留且已老化的孤立 GIF/调色板。路径保护失败时只报警，不扩大删除范围。
- **SSE 断线**：客户端会重连并用普通 GET 恢复；nginx 必须 `proxy_buffering off`。

## 安全反向代理

公网必须 TLS。只暴露 443；应用绑定 127.0.0.1 或内部网络。不要把 `/metrics` 和管理端无条件暴露给互联网；用网络 ACL/额外认证保护。保持 `client_max_body_size 1m`，请求体日志关闭。只有在 `trusted_proxy_cidrs` 写入实际反代网段后才会信任该直连对端提供的 X-Forwarded-For；公网配额仍应在 nginx/WAF 重复执行。

## 灾难恢复

停止服务，保存故障 db/wal/shm 和日志副本，恢复最近一致备份及对应密钥，先在隔离环境以 dry-run 启动。核对通知 cursor 与 published_comments；任务编号查重会降低重复发布风险。若不确定某个发布请求是否已到达 B站，先人工或读接口搜索任务编号，绝不能盲目删除发布记录后重试。
