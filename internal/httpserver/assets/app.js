"use strict";

const $ = (id) => document.getElementById(id);

let resolved = null;
let currentJob = null;
let eventSource = null;

for (const option of $("resolution").options) {
  if (option.dataset.width && Number(option.dataset.width) > Number($("width").max)) option.disabled = true;
}

function setMessage(element, message, kind = "") {
  element.textContent = message;
  element.classList.remove("is-error", "is-success", "is-loading");
  if (kind) element.classList.add(`is-${kind}`);
}

function setButtonBusy(button, busy, busyText, normalHTML) {
  button.disabled = busy;
  button.innerHTML = busy ? busyText : normalHTML;
}

function duration(seconds) {
  const total = Math.max(0, Number(seconds) || 0);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = Math.floor(total % 60);
  return hours
    ? `${hours}:${String(minutes).padStart(2, "0")}:${String(secs).padStart(2, "0")}`
    : `${minutes}:${String(secs).padStart(2, "0")}`;
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
  });
  const body = await response.json().catch(() => ({
    error: { message: "服务器返回了无法解析的响应" },
  }));
  if (!response.ok) throw new Error(body.error?.message || `HTTP ${response.status}`);
  return body;
}

$("resolve-button").addEventListener("click", async () => {
  const button = $("resolve-button");
  const input = $("video-input").value.trim();
  if (!input) {
    setMessage($("resolve-status"), "请先输入 BV 号、视频链接或分享文本", "error");
    $("video-input").focus();
    return;
  }

  setButtonBusy(button, true, "正在解析…", "解析视频 <span aria-hidden=\"true\">→</span>");
  setMessage($("resolve-status"), "正在读取视频信息与分P…", "loading");
  try {
    resolved = await api("/api/v1/videos/resolve", {
      method: "POST",
      body: JSON.stringify({ input, page: 0 }),
    });
    $("video-title").textContent = resolved.title;
    $("video-owner").textContent = `UP：${resolved.owner_name}`;
    $("video-duration").textContent = `总时长：${duration(resolved.duration)}`;
    $("video-cover").src = resolved.cover_url;

    const select = $("page");
    const options = resolved.pages.map((page) => {
      const option = document.createElement("option");
      option.value = page.page;
      option.textContent = `P${page.page} · ${page.part} (${duration(page.duration)})`;
      return option;
    });
    select.replaceChildren(...options);
    select.value = resolved.selected_page;
    select.disabled = false;
    $("video-card").classList.remove("hidden");
    $("create-button").disabled = false;
    setMessage($("resolve-status"), `解析完成 · ${resolved.pages.length} 个分P`, "success");
    estimate();
  } catch (error) {
    resolved = null;
    $("video-card").classList.add("hidden");
    $("create-button").disabled = true;
    setMessage($("resolve-status"), error.message, "error");
  } finally {
    setButtonBusy(button, false, "正在解析…", "解析视频 <span aria-hidden=\"true\">→</span>");
  }
});

function parseClock(value) {
  const parts = value.trim().replaceAll("：", ":").split(":").map(Number);
  if ((parts.length !== 2 && parts.length !== 3) || parts.some((part) => !Number.isFinite(part) || part < 0)) return NaN;
  return parts.length === 3
    ? parts[0] * 3600 + parts[1] * 60 + parts[2]
    : parts[0] * 60 + parts[1];
}

function estimate() {
  if (!resolved) {
    $("estimate").textContent = "解析视频后估算";
    return;
  }
  const fps = Number($("fps").value) || 10;
  const width = Number($("width").value) || 640;
  const start = parseClock($("start").value);
  const end = parseClock($("end").value);
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) {
    $("estimate").textContent = "请检查时间范围";
    return;
  }
  const bytes = (end - start) * fps * width * 0.55;
  $("estimate").textContent = `约 ${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

["start", "end", "fps", "width"].forEach((id) => $(id).addEventListener("input", estimate));

$("width").addEventListener("input", () => {
  const widths = { "360p": 640, "480p": 854, "720p": 1280 };
  const selected = $("resolution").value;
  if (widths[selected] && Number($("width").value) !== widths[selected]) $("resolution").value = "custom";
});

$("resolution").addEventListener("change", () => {
  const widths = { "360p": 640, "480p": 854, "720p": 1280 };
  const width = widths[$("resolution").value];
  if (width) $("width").value = String(width);
  estimate();
});

function bindRange(id, output, formatter = (value) => value) {
  const input = $(id);
  const target = $(output);
  const update = () => { target.textContent = formatter(input.value); };
  input.addEventListener("input", update);
  update();
}

bindRange("opacity", "opacity-value", (value) => `${Math.round(Number(value) * 100)}%`);
bindRange("font-scale", "font-value", (value) => `${Number(value).toFixed(1)}×`);
bindRange("density", "density-value", (value) => `${Math.round(Number(value) * 100)}%`);

$("job-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!resolved) return;

  eventSource?.close();
  $("result").classList.add("hidden");
  $("job-error").classList.add("hidden");
  $("job-error").textContent = "";
  $("gif-preview").removeAttribute("src");
  setButtonBusy($("create-button"), true, "正在创建任务…", "生成赛博琥珀 <span aria-hidden=\"true\">✦</span>");
  setMessage($("create-status"), "正在写入持久化队列…", "loading");

  const request = {
    input: resolved.bvid,
    page: Number($("page").value),
    start: $("start").value,
    end: $("end").value,
    fps: Number($("fps").value),
    width: Number($("width").value),
    resolution: $("resolution").value === "custom" ? "" : $("resolution").value,
    danmaku: $("danmaku").checked,
    danmaku_opacity: Number($("opacity").value),
    danmaku_font_scale: Number($("font-scale").value),
    danmaku_density: Number($("density").value),
  };

  try {
    const body = await api("/api/v1/jobs", { method: "POST", body: JSON.stringify(request) });
    currentJob = body.job;
    $("job-panel").classList.remove("hidden");
    $("job-id").textContent = `JOB ${currentJob.id}${body.deduplicated ? " · 已复用相同任务" : ""}`;
    $("cancel-button").hidden = body.cancel_allowed === false;
    setMessage($("create-status"), "任务已进入队列", "success");
    watch(currentJob.id);
    $("job-panel").scrollIntoView({ behavior: "smooth", block: "start" });
  } catch (error) {
    setMessage($("create-status"), error.message, "error");
    setButtonBusy($("create-button"), false, "正在创建任务…", "生成赛博琥珀 <span aria-hidden=\"true\">✦</span>");
  }
});

function watch(id) {
  eventSource?.close();
  eventSource = new EventSource(`/api/v1/jobs/${id}/events`);
  eventSource.addEventListener("job", (event) => renderJob(JSON.parse(event.data)));
  eventSource.onerror = async () => {
    try {
      renderJob(await api(`/api/v1/jobs/${id}`));
    } catch (_) {
      // EventSource will reconnect. The persisted status endpoint remains the fallback.
    }
  };
}

const labels = {
  queued: "排队中",
  resolving_video: "解析视频",
  fetching_stream: "获取视频流",
  fetching_danmaku: "获取弹幕",
  rendering: "渲染画面",
  optimizing: "优化体积",
  uploading: "上传图片",
  publishing: "发布评论",
  verifying: "验证发布",
  succeeded: "已完成",
  failed: "失败",
  cancelled: "已取消",
  interrupted: "已中断",
};

function renderJob(job) {
  currentJob = job;
  $("progress-bar").style.width = `${Math.max(0, Math.min(100, job.progress))}%`;
  $("job-status").textContent = `${labels[job.status] || job.status} · ${job.progress}%`;
  $("cancel-button").disabled = ["succeeded", "failed", "cancelled"].includes(job.status);

  if (job.status === "succeeded") {
    eventSource?.close();
    const url = `/api/v1/jobs/${job.id}/artifact`;
    $("gif-preview").src = url;
    $("download").href = `${url}?download=1`;
    $("final-params").replaceChildren();
    Object.entries(job.final || {}).forEach(([key, value]) => {
      const term = document.createElement("dt");
      const description = document.createElement("dd");
      term.textContent = key;
      description.textContent = String(value);
      $("final-params").append(term, description);
    });
    $("result").classList.remove("hidden");
    setMessage($("create-status"), "", "");
    setButtonBusy($("create-button"), false, "正在创建任务…", "生成赛博琥珀 <span aria-hidden=\"true\">✦</span>");
  } else if (["failed", "cancelled"].includes(job.status)) {
    eventSource?.close();
    $("job-error").textContent = job.error_message || labels[job.status];
    $("job-error").classList.remove("hidden");
    setButtonBusy($("create-button"), false, "正在创建任务…", "生成赛博琥珀 <span aria-hidden=\"true\">✦</span>");
  }
}

$("cancel-button").addEventListener("click", async () => {
  if (!currentJob) return;
  const button = $("cancel-button");
  button.disabled = true;
  try {
    await api(`/api/v1/jobs/${currentJob.id}/cancel`, { method: "POST", body: "{}" });
    $("job-status").textContent = "正在取消…";
  } catch (error) {
    $("job-error").textContent = error.message;
    $("job-error").classList.remove("hidden");
    button.disabled = false;
  }
});

window.addEventListener("beforeunload", () => eventSource?.close());
