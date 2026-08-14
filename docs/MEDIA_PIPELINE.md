# 媒体管线

1. `wbi/view` 解析 aid/bvid/cid/page/duration/dimension；严格检查时间位于同一分P。
2. `wbi/playurl` 请求 DASH，仅保留标准端口且域名边界正确的 `*.bilivideo.com`/`*.hdslb.com`，根据启动时 FFmpeg 解码器探测过滤 AVC/HEVC/AV1，选择不低于目标宽度的最小合理轨道；主 CDN 失败时按已校验 backup URL 顺序尝试。
3. 弹幕打开时，只计算 `[start,end)` 相交的 6 分钟 segment：`floor(start/360s)+1` 到 `floor((end-ε)/360s)+1`。进度减去 clip start 后进入 ASS。
4. ASS 布局分别维护滚动、顶部和底部轨道；按文本估算宽度、速度和占用期判断碰撞，逆向滚动与正向滚动不会在同轨迎面相交。颜色、文字/描边透明度、控制字符清理与 ASS 转义均在服务端生成。超过密度稳定丢弃，输入顺序相同时输出确定。
5. FFmpeg 第一遍执行 `fps,scale=lanczos,subtitles,palettegen`；第二遍使用相同画面链和 `paletteuse`，设置 `-loop 0`。参数作为 argv 传给 `exec.CommandContext`，不经过 shell。
6. ffprobe 检查 GIF codec/format、偶数尺寸、时长容差和帧数；字节级检查 Netscape/AnimExt 无限循环扩展。
7. 超过 `MAX_GIF_BYTES` 时依次降低 FPS、宽度、颜色数、最后改用 bayer 抖动。达到 `min_fps/min_width/min_colors` 后明确 `GIF_TOO_LARGE`，永不缩短时间。

每个产物保存请求参数、实际参数、首次大小、压缩次数、最终大小、是否降级和 SHA-256。临时 ASS/调色板总是清理；最终 GIF 同时受保留期限和总磁盘配额管理，崩溃遗留的无数据库 GIF/调色板在安全老化后也会清理。

## 取消与资源边界

Linux 子进程设置独立进程组，取消或超时向整个组发送 SIGKILL；stderr 只保存尾部并脱敏。用户在上传/发布/验证阶段请求取消时不会中断可能已到达 B站的 POST，而是让幂等核对完成，避免形成未知写入。worker、FFmpeg 和发布分别受限并发。容器示例限制 CPU、内存、PID 和临时盘；磁盘低于阈值时 readiness 失败。

## 字体

镜像通过 Debian 包安装 Noto CJK 和 fontconfig，不提交字体二进制。启动使用 `fc-match` 验证配置字体名称及代表性中文字符 U+4E2D；libass 缺失或 `subtitles` filter 不可用会阻止 ready。
