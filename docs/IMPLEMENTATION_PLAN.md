# 实现计划与验收状态

状态更新时间：2026-08-14。

| 阶段 | 模块 | 依赖/风险 | 状态 |
|---|---|---|---|
| 0 | 空仓审查、Go 1.26.6、参考 commit/许可证、真实只读探测 | 私有 API 漂移、GPL | 完成 |
| 1 | 配置、JSON 日志、SQLite migration、领域状态机、租约队列、health/ready | modernc SQLite 并发 | 完成；持续租约恢复、原子 Web 去重、运行设置持久化均有自动化测试 |
| 2 | BV/AV/短链/分享文本、SSRF、WBI、view/pagelist/playurl、Protobuf 弹幕、通知 | 无认证通知真实样本有限 | 完成；2026-08-14 真实只读 UGC/WBI/DASH/弹幕复验通过，认证通知仍待 Cookie 验证 |
| 3 | ASS、碰撞布局、FFmpeg argv/进程组、palette、降级、ffprobe、清理 | FFmpeg/libass/字体版本差异 | 完成；精确帧数、备用 CDN、渲染复用、配额/孤立文件清理已覆盖；本地媒体 E2E 已通过有/无弹幕、降级和取消 |
| 4 | 公开 Web、两步解析、SSE、预览下载、限流、管理登录/扫码/队列 | SSE 反代缓冲 | 完成；httptest 覆盖公开 API、取消所有权、CSRF、设置校验持久化、任务详情、黑名单与审计 |
| 5 | 加密账号、上传、一级图片评论、真正 @、提示、验证、幂等、熔断 | 必须用专用写入账号/视频 | 实现与本地假服务器通过；真实写入尚未验证 |
| 6 | 指标、安全、Docker/systemd/nginx、文档、race/vet/lint | 当前宿主没有 Docker daemon | 代码与部署文件完成；`go mod tidy`、普通/race 测试、vet、golangci-lint、govulncheck、Linux amd64/arm64 交叉构建和媒体 E2E 均已通过；Dockerfile/compose 无 daemon，不能声称实际构建运行 |

## 验收风险

- B站认证通知、图片上传与评论均为非稳定私有接口。无真实 Cookie 时，认证通知只使用明确标注为 schema 回归用途的合成脱敏夹具；写接口只接受本地假服务器为自动化证据，不把它们称为真实账号验证。
- 当前执行环境将 `b23.tv` 解析为 `198.18.0.48`，严格 SSRF 策略按 RFC 2544 保留网段拒绝该地址；真实只读 API/WBI/DASH/弹幕链路已通过，短链实时重定向仅由 `httptest` 覆盖。
- “接口 code=0”不等于游客可见，必须保留 `published_unverified`。
- Docker 构建结果只能在可用 Docker daemon 的环境中声明；否则由 CI/部署机执行。
- 媒体 integration 需要带 libass 与 Noto CJK 的 FFmpeg，缺失会让 readiness 失败而不是降级假成功。
