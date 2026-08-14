# 参考与溯源

## downkyicore

- URL: https://github.com/FortyTwoCn/downkyicore
- 实际检查 commit: `b8edb9b12bf214411fca933ebb07457c949f577f`
- commit 日期/标题：2026-06-27 14:30:34 +08:00，`fix: 修复cookie膨胀 #507`
- 许可证：GPL-3.0

完整阅读的重点文件：

- `DownKyi.Core/BiliApi/BiliUtils/ParseEntrance.cs`：输入类型、视频 ID 入口。
- `.../BvId.cs`：新版 BV/AV Base58 变换常量与测试向量。
- `.../Video/VideoInfo.cs` 及 `VideoView/VideoPage` 模型：WBI view、pagelist、核心字段。
- `.../VideoStream/VideoStream.cs` 及 PlayUrl/Dash 模型：playurl 参数、DASH 轨道字段。
- `.../Danmaku/DanmakuProtobuf.cs`、`BiliDanmaku.cs` 与生成的 `Dm.cs`：6 分钟弹幕段、字段号与模型。
- `.../Sign/WbiSign.cs`：mixin table、排序、字符过滤与 MD5。
- `.../WebClient.cs`：Referer/Origin/UA/Cookie/buvid 请求上下文。
- `.../Login/LoginQR.cs`、`LoginHelper.cs` 与登录模型：扫码端点、状态与 Cookie 范围。

本项目用 Go 重新设计错误处理、SSRF、context、并发、持久化和测试；没有生产依赖 .NET/DownKyi，也没有复制其 UI。BV/WBI/协议实现属于实质性改写，因此整个项目明确采用 GPL-3.0-only，并保留本记录与第三方声明。

## 其他协议依据

- 2026-08-14 对 B站只读端点的实际响应探测；结果与字段记于 `BILIBILI_API_NOTES.md`。
- WBI 固定向量：img/sub key、mixin key 与 `w_rid=8f6f...`，用于防止编码回归。
- 弹幕 `.proto` 为实际所需字段子集，生成代码记录 protoc/protoc-gen-go 版本。

没有从来源不明的字体二进制复制内容。镜像只通过 Debian 包管理器安装字体与 FFmpeg。
