# B站接口适配记录

最后只读实际验证日期：**2026-08-14（Asia/Shanghai）**。这些接口不是稳定的正式公共 API。路径集中于 `internal/bili/endpoints`；所有 JSON 同时检查 HTTP 状态和顶层 `code`。结构缺字段时返回带 `*_STRUCTURE_CHANGED` 的兼容性错误，不把零值当成功。

| 用途 | 方法与路径 | 认证 | 关键参数/响应 | 已见错误 | 夹具/验证 | 降级 |
|---|---|---|---|---|---|---|
| buvid | GET `/x/frontend/finger/spi` | 否 | `data.b_3`, `data.b_4` | 网络错误 | 2026-08-14 实际 `code=0` | 记录警告，继续请求 |
| WBI key/账号 | GET `/x/web-interface/nav` | 可选 Cookie | `data.wbi_img.{img_url,sub_url}`；登录时 `isLogin/mid/uname` | 未登录 `code=-101` 但仍带 WBI key | 实际未登录响应；WBI 固定向量测试 | key 缓存 6h，签名失败失效并重试一次 |
| 视频详情 | GET `/x/web-interface/wbi/view` | 可选 | `bvid/aid/title/owner/pic/duration/state/rights/pages` | `-404` 等 | 实际读取 BV17x411w7KC，10P | pages 缺失时请求 pagelist |
| 分P | GET `/x/player/pagelist` | 否 | `cid/page/part/duration/dimension` | 资源不存在 | 2026-08-14 实际 10P | 无合法 page 明确失败 |
| DASH | GET `/x/player/wbi/playurl` | 可选 | `cid,qn,fnval=4048,fourk,wts,w_rid`；`dash.video` 同时兼容 camel/snake 字段 | 签名/权限/风控 | 实际未登录返回 360P DASH | 刷新 WBI；按解码器和目标宽度选安全 CDN |
| 弹幕段 | GET `/x/v2/dm/web/seg.so` | 否 | `type=1,oid=cid,pid=aid,segment_index`；Protobuf | 段不存在/结构变化 | BV17x411w7KC P1 segment 1 实际 207408 bytes | 单条不支持模式过滤，段解析失败则任务失败 |
| @我的 | GET `/x/msgfeed/at` | Cookie；消息中心 Referer/Origin | `platform=web,build=0,mobi_app=web,id,at_time`；`data.cursor/items`，可选 `at_details[].mid/uname` | 未登录 `-101` | `notifications_at_unauth_2026-08-14.json` 为实际未登录响应；`notifications_at_redacted_variant.json` 仅为 schema 回归夹具，**不是账号录制响应** | 指数退避；错误持久化；管理员可执行串行只读探测；412/验证码熔断；倒序/游标循环/非末页无 cursor 明确失败 |
| 扫码生成/轮询 | GET passport `/x/passport-login/web/qrcode/generate`、`.../poll` | 否 | passport Referer/Origin；`url/qrcode_key`；状态 86101/86090/86038/0；成功时合并 callback URL 与 `Set-Cookie` | 过期/未知状态 | 2026-08-14 实际生成 `code=0` 且立即轮询得到 86101；passport 请求上下文和仅从 `Set-Cookie` 取得凭据有 httptest；最终扫码确认仍未执行，**账号登录尚未验证** | 未知状态明确失败，不覆盖旧账号 |
| 图片上传 | POST `/x/dynamic/feed/draw/upload_bfs` multipart | Cookie+CSRF | `file_up` GIF、`biz=draw`,`category=daily`,`csrf`,`csrf_token`; `image_url/width/height/size` | HTTP 412、账号、验证码、审核 | multipart payload httptest；**未真实写入验证** | 临时错误最多 3 次；风控立即熔断 |
| 一级评论 | POST `/x/v2/reply/add` form | Cookie+CSRF | `oid=aid,type=1,message,pictures,at_name_to_mid,plat,csrf,csrf_token`；根评论不含 root/parent | 验证码、敏感、关闭、重复 | payload httptest；**未真实写入验证** | 风控熔断；任务编号查重后才重试 |
| 评论回读 | GET `/x/v2/reply/main` | 可选 | `type=1,oid,mode,next`; `replies.member.mid`、`content.{message,pictures}`、`cursor.next` | 未认证实测曾返回 `-352`；审核/不可见 | httptest；**认证读取结构尚待专用账号复验** | 找不到/缺图记 `pending_review`；账号侧找到也仅记 `published_unverified`，不宣称公开可见 |

## 通知字段解释

适配器接受数字或数字字符串，提取通知 ID/时间、user MID/昵称/头像、`subject_id/root_id/source_id/target_id`、business/type、URI、评论文本和可选 `at_details` MID 列表。任务来源本身是该账号的“@我的”，因此不依赖当前机器人昵称做字符串命中；自发任务用持久化账号 MID 排除。当前对 `source_id` 为触发评论 rpid、`root_id` 为评论根的映射来自参考实现与 schema 回归夹具，尚未用本项目专用账号的真实脱敏通知复验；若 `source_id` 为零或无法由 `subject_id/URI` 确定视频，事件标为结构不兼容而不会入队。取得真实脱敏夹具后应扩展测试，不应在业务层继续猜测字段。

2026-08-15 在用户环境确认网页媒体管线正常但 `@` 事件未进入 `mention_events`。对照 B站消息中心和仍维护的开源客户端后，通知请求改为发送 `Referer: https://message.bilibili.com/`、匹配的 Origin 与 JSON Accept。管理后台的“立即轮询 @”用于取得账号侧的实际成功游标或脱敏错误；在用户提供真实响应前，不将该兼容调整描述为已完成真实通知读取验证。

## HTTP 与风控

请求使用固定 UA、Referer、Origin、超时和有限响应体。412、`-352`、验证码类 code 标为 Risk + Permanent，持久化 `bot_state.paused` 并暂停写操作；资源级永久错误（如 `12002`）不会误触发全账号熔断，429/频率错误只做有限带抖动重试。日志统一经过 URL/Cookie/CSRF/签名参数脱敏。
