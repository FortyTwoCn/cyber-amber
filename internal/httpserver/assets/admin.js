"use strict";

const $ = (id) => document.getElementById(id);

let csrf = "";
let qrID = "";
let qrTimer = null;
let adminTimezone = "Asia/Shanghai";

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...(csrf ? { "X-CSRF-Token": csrf } : {}),
      ...(options.headers || {}),
    },
  });
  const body = await response.json().catch(() => ({
    error: { message: "服务器返回了无法解析的响应" },
  }));
  if (!response.ok) {
    const error = new Error(body.error?.message || `HTTP ${response.status}`);
    error.status = response.status;
    error.code = body.error?.code || "HTTP_ERROR";
    throw error;
  }
  return body;
}

function setMessage(element, message, kind = "") {
  element.textContent = message;
  element.classList.remove("is-error", "is-success", "is-loading");
  if (kind) element.classList.add(`is-${kind}`);
}

function setButtonBusy(button, busy, busyText, normalText) {
  button.disabled = busy;
  button.textContent = busy ? busyText : normalText;
}

function cell(value) {
  const item = document.createElement("td");
  item.textContent = value ?? "";
  return item;
}

function emptyRow(columns, message) {
  const row = document.createElement("tr");
  row.className = "empty-row";
  const item = document.createElement("td");
  item.colSpan = columns;
  item.textContent = message;
  row.append(item);
  return row;
}

function formatTime(value) {
  if (!value) return "永久";
  try {
    return new Intl.DateTimeFormat("zh-CN", {
      timeZone: adminTimezone,
      dateStyle: "medium",
      timeStyle: "medium",
    }).format(new Date(value));
  } catch (_) {
    return value;
  }
}

function showLogin() {
  $("login-panel").classList.remove("hidden");
  $("admin-panel").classList.add("hidden");
}

function showDashboard() {
  $("login-panel").classList.add("hidden");
  $("admin-panel").classList.remove("hidden");
}

function updateOverview(body) {
  const account = body.account || {};
  const bot = body.bot || {};
  const paused = Boolean(bot.Paused ?? bot.paused);
  const circuitReason = bot.CircuitReason ?? bot.circuit_reason ?? "";

  $("metric-queue").textContent = String(body.queue_depth ?? 0);
  $("metric-active").textContent = String(body.active_jobs ?? 0);
  $("metric-bot").textContent = paused ? "已暂停" : "运行中";
  $("metric-account").textContent = account.logged_in ? account.nickname || `MID ${account.mid}` : "未登录";

  const dryRun = $("dry-run-badge");
  dryRun.textContent = body.dry_run ? "DRY RUN" : "LIVE MODE";
  dryRun.className = body.dry_run ? "badge warning" : "badge success";

  const badge = $("account-badge");
  if (account.logged_in) {
    badge.textContent = "会话有效";
    badge.className = "badge success";
    $("account-name").textContent = account.nickname || "已登录 B站";
    $("account-meta").textContent = `MID ${account.mid} · 最近检查 ${formatTime(account.last_checked_at)}`;
  } else {
    badge.textContent = account.error ? "会话异常" : "未连接";
    badge.className = account.error ? "badge danger" : "badge neutral";
    $("account-name").textContent = "尚未登录 B站";
    $("account-meta").textContent = account.error || "扫码登录或导入浏览器 Cookie";
  }

  if (paused && circuitReason) {
    $("metric-bot").title = circuitReason;
  } else {
    $("metric-bot").removeAttribute("title");
  }
}

$("login-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = event.submitter || $("login-form").querySelector("button[type=submit]");
  setButtonBusy(button, true, "正在验证…", "进入控制台 →");
  setMessage($("login-status"), "正在建立安全会话…", "loading");
  try {
    const body = await api("/api/v1/admin/login", {
      method: "POST",
      body: JSON.stringify({ password: $("password").value }),
    });
    csrf = body.csrf_token;
    $("password").value = "";
    setMessage($("login-status"), "登录成功", "success");
    showDashboard();
    await refresh();
  } catch (error) {
    setMessage($("login-status"), error.message, "error");
  } finally {
    setButtonBusy(button, false, "正在验证…", "进入控制台 →");
  }
});

async function refresh() {
  const body = await api("/api/v1/admin/account");
  csrf = body.csrf_token || csrf;
  adminTimezone = body.timezone || adminTimezone;
  $("account-status").textContent = JSON.stringify(body, null, 2);
  updateOverview(body);
  await Promise.all([jobs(), mentions(), blocked(), audit(), settings()]);
  return body;
}

async function settings() {
  const body = await api("/api/v1/admin/settings");
  $("settings-json").value = JSON.stringify(body.pending, null, 2);
  setMessage(
    $("settings-status"),
    body.restart_required ? "有待生效设置，请安全重启服务。" : "当前显示的设置已生效。",
    body.restart_required ? "loading" : "success",
  );
}

async function jobs() {
  const body = await api("/api/v1/admin/jobs");
  const rows = body.jobs.map((job) => {
    const row = document.createElement("tr");
    [job.id, job.source, job.status, `${job.progress}%`, job.error_message || ""].forEach((value) => row.append(cell(value)));
    const action = cell("");
    const detail = document.createElement("button");
    detail.textContent = "详情";
    detail.className = "ghost small";
    detail.addEventListener("click", async () => {
      try {
        const detailBody = await api(`/api/v1/admin/jobs/${job.id}`);
        $("job-detail").textContent = JSON.stringify(detailBody, null, 2);
        $("job-detail").classList.remove("hidden");
      } catch (error) {
        $("job-detail").textContent = error.message;
        $("job-detail").classList.remove("hidden");
      }
    });
    action.append(detail);
    if (["failed", "interrupted"].includes(job.status)) {
      const retry = document.createElement("button");
      retry.textContent = "重试";
      retry.className = "secondary small";
      retry.addEventListener("click", async () => {
        retry.disabled = true;
        try {
          await api(`/api/v1/admin/jobs/${job.id}/retry`, { method: "POST", body: "{}" });
          await jobs();
        } catch (error) {
          $("job-detail").textContent = error.message;
          $("job-detail").classList.remove("hidden");
        } finally {
          retry.disabled = false;
        }
      });
      action.append(retry);
    }
    row.append(action);
    return row;
  });
  $("jobs-body").replaceChildren(...(rows.length ? rows : [emptyRow(6, "暂无任务")]));
}

const mentionStatusLabels = {
  received: "待处理",
  enqueued: "已入队",
  invalid: "命令无效",
  ignored_self: "忽略自身",
  blocked: "黑名单",
  rate_limited: "已限流",
  deduplicated: "已去重",
};

const mentionErrorLabels = {
  CLIP_TOO_LONG: "截取时长超过 max_clip_duration",
  INVALID_TIME_RANGE: "时间范围格式无效",
  INVALID_PARAMETER: "参数不在允许范围",
  INVALID_COMMAND: "无法解析命令",
  USER_BLOCKED: "用户在黑名单中",
  USER_CONCURRENCY_LIMIT: "用户并发任务已达上限",
  MID_DAILY_LIMIT: "用户每日任务已达上限",
  USER_COOLDOWN: "用户仍在冷却时间内",
  VIDEO_RATE_LIMIT: "该视频小时任务已达上限",
};

async function mentions() {
  const body = await api("/api/v1/admin/mentions");
  const rows = body.mentions.map((mention) => {
    const row = document.createElement("tr");
    const sender = mention.sender_name ? `${mention.sender_name} · ${mention.sender_mid}` : String(mention.sender_mid);
    const video = mention.bvid || (mention.aid ? `av${mention.aid}` : "-");
    const status = mentionStatusLabels[mention.status] || mention.status;
    const reason = mentionErrorLabels[mention.error_code] || mention.error_code || "-";
    [formatTime(mention.occurred_at), sender, mention.message, video, status, reason].forEach((value) => row.append(cell(value)));
    row.title = `通知 ${mention.notification_id} · 评论 ${mention.rpid}`;
    return row;
  });
  $("mentions-body").replaceChildren(...(rows.length ? rows : [emptyRow(6, "尚未收到新的 @ 通知")]));
}

async function blocked() {
  const body = await api("/api/v1/admin/blocked");
  const rows = body.blocked.map((item) => {
    const row = document.createElement("tr");
    [item.subject_type, item.subject_id, item.reason, formatTime(item.expires_at)].forEach((value) => row.append(cell(value)));
    const action = cell("");
    const remove = document.createElement("button");
    remove.className = "ghost small danger-text";
    remove.textContent = "移除";
    remove.addEventListener("click", async () => {
      remove.disabled = true;
      try {
        await api(`/api/v1/admin/blocked/${encodeURIComponent(item.subject_type)}/${encodeURIComponent(item.subject_id)}`, {
          method: "DELETE",
          body: "{}",
        });
        await blocked();
      } catch (error) {
        setMessage($("block-status"), error.message, "error");
      } finally {
        remove.disabled = false;
      }
    });
    action.append(remove);
    row.append(action);
    return row;
  });
  $("blocked-body").replaceChildren(...(rows.length ? rows : [emptyRow(5, "黑名单为空")]));
}

async function audit() {
  const body = await api("/api/v1/admin/audit");
  const rows = body.audit.map((item) => {
    const row = document.createElement("tr");
    [formatTime(item.created_at), item.action, item.target, item.request_id].forEach((value) => row.append(cell(value)));
    return row;
  });
  $("audit-body").replaceChildren(...(rows.length ? rows : [emptyRow(4, "暂无审计记录")]));
}

$("block-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const expiry = $("block-expiry").value;
  setMessage($("block-status"), "正在保存…", "loading");
  try {
    await api("/api/v1/admin/blocked", {
      method: "POST",
      body: JSON.stringify({
        subject_type: $("block-type").value,
        subject_id: $("block-id").value,
        reason: $("block-reason").value,
        expires_at: expiry ? new Date(expiry).toISOString() : "",
      }),
    });
    event.target.reset();
    setMessage($("block-status"), "已加入黑名单", "success");
    await blocked();
  } catch (error) {
    setMessage($("block-status"), error.message, "error");
  }
});

$("refresh-jobs").addEventListener("click", async () => {
  try { await jobs(); } catch (error) { $("job-detail").textContent = error.message; $("job-detail").classList.remove("hidden"); }
});

$("refresh-mentions").addEventListener("click", async () => {
  try { await mentions(); } catch (error) { window.alert(error.message); }
});

$("refresh-audit").addEventListener("click", async () => {
  try { await audit(); } catch (error) { window.alert(error.message); }
});

$("clear-cache").addEventListener("click", async () => {
  const button = $("clear-cache");
  setButtonBusy(button, true, "清理中…", "清理缓存");
  try {
    await api("/api/v1/admin/cache/cleanup", { method: "POST", body: "{}" });
    await audit();
  } catch (error) {
    window.alert(error.message);
  } finally {
    setButtonBusy(button, false, "清理中…", "清理缓存");
  }
});

async function runAccountAction(buttonID, busyText, request, successText = "") {
  const button = $(buttonID);
  const normalText = button.textContent;
  setButtonBusy(button, true, busyText, normalText);
  try {
    await request();
    await refresh();
    if (successText) setMessage($("cookie-status"), successText, "success");
  } catch (error) {
    setMessage($("cookie-status"), error.message, "error");
  } finally {
    setButtonBusy(button, false, busyText, normalText);
  }
}

$("check-account").addEventListener("click", () => runAccountAction(
  "check-account",
  "检查中…",
  () => api("/api/v1/admin/account/check", { method: "POST", body: "{}" }),
  "账号会话检查通过",
));

$("poll-mentions").addEventListener("click", async () => {
  const button = $("poll-mentions");
  setButtonBusy(button, true, "轮询中…", "立即轮询 @");
  setMessage($("mention-poll-status"), "正在读取 B站 @ 我的通知…", "loading");
  try {
    const body = await api("/api/v1/admin/bot/poll", { method: "POST", body: "{}" });
    const succeededAt = body.cursor?.LastSuccess || body.cursor?.last_success_at;
    setMessage($("mention-poll-status"), `轮询成功${succeededAt ? ` · ${formatTime(succeededAt)}` : ""}`, "success");
    await refresh();
  } catch (error) {
    setMessage($("mention-poll-status"), `轮询失败：${error.message}`, "error");
  } finally {
    setButtonBusy(button, false, "轮询中…", "立即轮询 @");
  }
});

$("pause-button").addEventListener("click", () => runAccountAction(
  "pause-button",
  "暂停中…",
  () => api("/api/v1/admin/bot/pause", { method: "POST", body: JSON.stringify({ reason: "ADMIN_PAUSED" }) }),
));

$("resume-button").addEventListener("click", () => runAccountAction(
  "resume-button",
  "恢复中…",
  () => api("/api/v1/admin/bot/resume", { method: "POST", body: "{}" }),
));

$("logout-button").addEventListener("click", async () => {
  try {
    await api("/api/v1/admin/logout", { method: "POST", body: "{}" });
  } finally {
    window.location.reload();
  }
});

$("reload-settings").addEventListener("click", async () => {
  try { await settings(); } catch (error) { setMessage($("settings-status"), error.message, "error"); }
});

$("save-settings").addEventListener("click", async () => {
  const button = $("save-settings");
  setButtonBusy(button, true, "保存中…", "保存设置");
  try {
    const value = JSON.parse($("settings-json").value);
    const body = await api("/api/v1/admin/settings", { method: "PUT", body: JSON.stringify(value) });
    $("settings-json").value = JSON.stringify(body.pending, null, 2);
    setMessage(
      $("settings-status"),
      body.restart_required ? "已保存；请安全重启服务以应用。" : "设置与当前运行值相同。",
      body.restart_required ? "loading" : "success",
    );
  } catch (error) {
    setMessage($("settings-status"), error instanceof SyntaxError ? "JSON 格式不正确" : error.message, "error");
  } finally {
    setButtonBusy(button, false, "保存中…", "保存设置");
  }
});

$("reset-settings").addEventListener("click", async () => {
  const button = $("reset-settings");
  setButtonBusy(button, true, "重置中…", "恢复部署值");
  try {
    await api("/api/v1/admin/settings", { method: "DELETE", body: "{}" });
    setMessage($("settings-status"), "已移除数据库覆盖；重启后恢复配置文件与环境变量。", "success");
  } catch (error) {
    setMessage($("settings-status"), error.message, "error");
  } finally {
    setButtonBusy(button, false, "重置中…", "恢复部署值");
  }
});

$("cookie-button").addEventListener("click", async () => {
  const cookie = $("cookie-input").value.trim();
  if (!cookie) {
    setMessage($("cookie-status"), "请先粘贴 Header String 格式的 Cookie", "error");
    return;
  }
  if (cookie.startsWith("{") || cookie.startsWith("[")) {
    setMessage($("cookie-status"), "这里不接收 JSON；导出时请选择 Header String", "error");
    return;
  }
  const button = $("cookie-button");
  setButtonBusy(button, true, "正在验证…", "验证并加密保存");
  setMessage($("cookie-status"), "正在向 B站验证账号会话…", "loading");
  try {
    await api("/api/v1/admin/account/cookie", {
      method: "POST",
      body: JSON.stringify({ cookie }),
    });
    $("cookie-input").value = "";
    setMessage($("cookie-status"), "Cookie 有效，已加密保存", "success");
    await refresh();
  } catch (error) {
    setMessage($("cookie-status"), error.message, "error");
  } finally {
    setButtonBusy(button, false, "正在验证…", "验证并加密保存");
  }
});

function clearQRTimer() {
  if (qrTimer) window.clearTimeout(qrTimer);
  qrTimer = null;
}

function setQRPlaceholder(message, failed = false) {
  const placeholder = $("qr-placeholder");
  placeholder.classList.remove("hidden", "is-error");
  if (failed) placeholder.classList.add("is-error");
  placeholder.querySelector("small").textContent = message;
  $("qr-image").classList.add("hidden");
  $("qr-image").removeAttribute("src");
}

$("qr-button").addEventListener("click", async () => {
  const button = $("qr-button");
  clearQRTimer();
  qrID = "";

  // 先展示容器，确保网络或 B站接口错误不会被隐藏。
  $("qr-box").classList.remove("hidden");
  setQRPlaceholder("正在请求二维码");
  setMessage($("qr-status"), "正在连接 B站登录服务…", "loading");
  setButtonBusy(button, true, "正在生成…", "扫码登录");

  try {
    const body = await api("/api/v1/admin/account/qrcode", { method: "POST", body: "{}" });
    qrID = body.id;
    $("qr-image").src = body.image;
    $("qr-image").classList.remove("hidden");
    $("qr-placeholder").classList.add("hidden");
    setMessage($("qr-status"), "二维码已生成，请使用哔哩哔哩客户端扫码", "success");
    qrTimer = window.setTimeout(pollQR, 1800);
  } catch (error) {
    setQRPlaceholder("二维码生成失败", true);
    setMessage($("qr-status"), `无法生成二维码：${error.message}`, "error");
  } finally {
    setButtonBusy(button, false, "正在生成…", "扫码登录");
  }
});

async function pollQR() {
  if (!qrID) return;
  clearQRTimer();
  try {
    const body = await api(`/api/v1/admin/account/qrcode/${encodeURIComponent(qrID)}`);
    const messages = {
      waiting: "等待扫码…",
      scanned: "已扫码，请在手机端确认登录",
      confirmed: "登录成功，正在刷新账号状态…",
      expired: "二维码已过期，请重新生成",
    };
    const message = body.message || messages[body.state] || body.state;
    setMessage($("qr-status"), message, body.state === "confirmed" ? "success" : body.state === "expired" ? "error" : "loading");

    if (body.state === "confirmed") {
      qrID = "";
      await refresh();
      return;
    }
    if (body.state === "expired") {
      qrID = "";
      setQRPlaceholder("二维码已过期", true);
      return;
    }
    qrTimer = window.setTimeout(pollQR, 1800);
  } catch (error) {
    qrID = "";
    setQRPlaceholder("状态检查失败", true);
    setMessage($("qr-status"), `二维码状态检查失败：${error.message}`, "error");
  }
}

window.addEventListener("beforeunload", clearQRTimer);

refresh()
  .then(() => showDashboard())
  .catch((error) => {
    if (error.status === 401) {
      showLogin();
      return;
    }
    showLogin();
    setMessage($("login-status"), error.message, "error");
  });
