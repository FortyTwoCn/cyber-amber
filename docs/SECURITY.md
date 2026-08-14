# 安全模型

## 信任边界

公开用户只提交 B站视频标识和类型化媒体参数。URL 解析只接受 `bilibili.com/www/m` 与 `b23.tv` 的标准端口且拒绝 userinfo；短链每次重定向重新校验。DNS 解析后固定连接已检查的地址，并拒绝回环、私网、链路本地、CGNAT、文档/基准保留网段、组播、未指定和常见云元数据地址。播放 CDN 只接受正确域名边界的 `bilivideo.com`/`hdslb.com`。

FFmpeg 不使用 shell，不接受任意滤镜、路径或下载 URL。临时目录 0700，文件名由 ULID 生成。Linux 取消杀死整个 FFmpeg 进程组。

## 凭据

- Cookie 只保留明确白名单字段，数据库使用 AES-256-GCM 和上下文 AAD。
- 主密钥只能来自环境或 secret 文件；数据库有账号但密钥缺失/错误时明确失败。
- 公开 API 和账号状态从不返回完整 Cookie、refresh token、CSRF 或密文。
- 管理密码使用 Argon2id；哈希工具只从 stdin 读取明文，避免进程列表泄露。Session 由至少 32 字节密钥 HMAC 签名，Session/CSRF Cookie 均 HttpOnly/SameSite=Strict，HTTPS 时 Secure。
- 所有管理写操作要求双提交 Cookie + HMAC CSRF token；登录与公开创建分别限流。

## Web

模板自动转义，动态内容用 `textContent`，全站 CSP 禁止内联/第三方脚本，默认不启用 CORS。响应设置 nosniff、DENY frame、same-origin referrer 和受限 Permissions-Policy；HTTPS 配置下发送 HSTS。服务默认只相信 TCP `RemoteAddr`；仅当直连对端命中显式 `trusted_proxy_cidrs` 时，才从 `X-Forwarded-For` 右向左跳过可信代理并选择最右侧不可信客户端，防止伪造左端地址。反代仍应重复限流。

产物下载要求数据库路径是配置产物目录中的直接、普通 `.gif` 文件，拒绝符号链接和目录逃逸。清理器也只删除允许目录中的直接目标，显式拒绝 `..`、嵌套路径和符号链接。

## B站写入

默认 dry-run。一级图片评论 payload 没有 `root`/`parent`；真实 @ 使用 MID 映射。写入先按机器人 MID 搜索精确任务编号行，风控/412/验证码会持久化暂停并停放未完成交付，账号失效也会阻止管理员直接恢复。接口返回成功和账号侧回读均只记 `published_unverified`；缺图、不匹配或暂不可见记 `pending_review`，没有游客视角证据不会记 `published`。

## 威胁与剩余风险

- B站私有接口会变化；结构错误会停在兼容性错误而不是误报。
- 已登录 Cookie 具有账号权限，必须使用专用低权限账号、秘密挂载和最小发布配额。
- ULID 含 80 位随机熵但下载 URL仍是 bearer capability；高度敏感部署应在反代层增加登录或签名 URL。
- 管理 Cookie 导入正文不会写日志，但反向代理也必须禁用请求体日志。
- 内容审核与游客可见性无法仅靠账号视角完全证明，应人工抽查专用测试视频。

漏洞报告请避免附带真实 Cookie、密钥或可写测试账号。
