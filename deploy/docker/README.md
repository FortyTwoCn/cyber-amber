# Docker 部署

本目录的脚本与项目根目录的 `Dockerfile`、`docker-compose.yml`、`config.example.yaml` 组成完整 Docker 部署方案。镜像内包含 FFmpeg、libass、Noto CJK、fontconfig、CA 证书和时区数据，服务以 UID/GID 10001 非 root 运行。

## 前置条件

- Linux amd64 或 arm64；
- Docker Engine 24+；
- Docker Compose v2；
- OpenSSL；
- 建议至少 2 CPU、2 GiB 内存和 20 GiB 可用磁盘；
- 公网部署需要支持 SSE 的 TLS 反向代理。

## 首次部署

在项目根目录执行：

```bash
chmod +x deploy/docker/*.sh
./deploy/docker/prepare.sh
docker compose up -d
./deploy/docker/verify.sh
```

`prepare.sh` 会：

1. 从 `config.example.yaml` 创建权限为 0600 的 `config.yaml`；
2. 创建 Cookie AES-256-GCM 密钥和 Session 密钥；
3. 构建 `cyber-amber:local`；
4. 通过 stdin 生成 Argon2id 管理员密码哈希；
5. 执行 `docker compose config --quiet`。

秘密文件保存在 `secrets/`，该目录已被 Git 和 Docker build context 排除。脚本不会覆盖已有密钥；备份与恢复时必须把 `secrets/` 和 `/data` 卷作为同一组保存，否则数据库中的 B站凭据将无法解密。

默认只监听宿主 `127.0.0.1:8080`，且 `BILI_DRY_RUN=true`。打开 `http://127.0.0.1:8080/admin` 登录管理后台。公开服务前，修改 `config.yaml` 中的 `app.base_url`，配置 `deploy/nginx/cyber-amber.conf` 的域名与证书，并按实际反代网络设置 `web.trusted_proxy_cidrs`。

## 常用操作

```bash
# 状态和日志
docker compose ps
docker compose logs -f --tail=200 cyber-amber

# 安全停止与启动
docker compose stop -t 30 cyber-amber
docker compose start cyber-amber

# 升级源码后重建
docker compose build --pull
docker compose up -d
./deploy/docker/verify.sh

# 查看数据卷
docker volume inspect cyber-amber_cyber_amber_data
docker volume inspect cyber-amber_cyber_amber_cache
```

## 备份与恢复

备份前先停止服务，避免复制不一致的 SQLite WAL：

```bash
docker compose stop -t 30 cyber-amber
mkdir -p backup
docker run --rm -v cyber-amber_cyber_amber_data:/source:ro -v "$PWD/backup:/backup" alpine \
  tar -C /source -czf /backup/data.tar.gz .
tar -czf backup/secrets.tar.gz secrets config.yaml
docker compose start cyber-amber
```

恢复到空数据卷前先停止服务，并确保恢复的是同一组 `data.tar.gz`、`secrets.tar.gz`。秘密文件权限应恢复为 0600。更完整的迁移、灾难恢复和故障处理见 `docs/OPERATIONS.md`。

## 安全边界

- 不要提交 `config.yaml`、`.env` 或 `secrets/`；
- 不要在命令参数、Compose YAML 或日志中粘贴真实 Cookie；
- 未完成专用测试账号验证前保持 dry-run；
- `/metrics` 只应对监控网络开放；
- 容器使用只读根文件系统、无 Linux capabilities、`no-new-privileges`、PID/CPU/内存/日志限制；
- `/data` 和 `/cache` 是持久卷，`/tmp/cyber-amber` 是有大小限制的 tmpfs。
