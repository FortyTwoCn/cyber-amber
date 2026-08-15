# Changelog

## 0.1.5 - 2026-08-15

- Use the current comment-image upload context (`biz=new_dyn`) and preserve the server-provided `img_size` instead of substituting a file-size estimate.
- Verify the assigned root-comment rpid without account cookies and distinguish publicly visible comments from review delays and confirmed deletion.
- Mark the delivery job failed, pause bot writes for 30 minutes, and suppress the misleading original-comment hint after a confirmed instant deletion.
- Record the first real-account write result: image upload and `reply/add` returned success, but B站 subsequently deleted the assigned root comment.

## 0.1.4 - 2026-08-15

- Accept both string and numeric `item.type` values from the authenticated B站 mention feed.
- Preserve enum names such as `reply` in a backward-compatible SQLite text column.
- Added response-shape and database migration regression coverage for the real-account incompatibility reported by the administrator probe.

## 0.1.3 - 2026-08-15

- Send the B站 message-center Referer, Origin and Accept headers when reading `/x/msgfeed/at`.
- Added an administrator-only “立即轮询 @” action that reports the current B站 error or persisted cursor immediately.
- Serialized scheduled and manual notification polls to prevent cursor races.
- Log newly persisted mention batches without exposing raw notification payloads or account credentials.

## 0.1.2 - 2026-08-15

- Added a recent mention inbox to the administration console, including ingestion status and rejection codes such as `CLIP_TOO_LONG`.
- Added an authenticated read-only admin API for mention diagnostics without exposing raw notification payloads.
- Fixed the first-login notification race by waking the mention poller immediately after QR login, Cookie import, or bot resume instead of waiting for an existing authentication backoff.
- Added regression tests for persisted rejected mentions and poller wake-up behavior.

## 0.1.1 - 2026-08-14

- Redesigned the public generator and administration console with a modern, responsive cyber-amber interface.
- Fixed QR sign-in feedback so loading, scanning, confirmation, expiration and API failures are always visible.
- Clarified that browser credentials must be exported as a Cookie `Header String`, not JSON.
- Added client-side Cookie format checks and a regression test for visible QR request failures.
- Hardened QR login with passport request headers and `Set-Cookie` credential capture.
- Added the `csrf_token` compatibility field to image uploads and comment submissions.
- Recorded the user-suggested BiliGo reference without copying its unlicensed source.

## 0.1.0 - 2026-08-14

- Initial production-oriented Go implementation.
- Added strict B站 input/WBI/video/DASH/danmaku adapters and recorded read-only API notes.
- Added SQLite migrations, durable leased queue, notification cursor, deduplication and recovery.
- Added ASS layout, two-pass FFmpeg palette pipeline, adaptive size control and ffprobe verification.
- Added public Web/SSE/API, admin auth/account controls, metrics and cleanup.
- Added dry-run-safe upload/root-image-comment/verification adapters and risk circuit breaker.
- Added Docker, Compose, systemd, nginx, CI and operations/security/license documentation.
- Hardened continuous lease recovery, lease-loss cancellation, paused-job parking, active Web dedupe and cross-job render reuse.
- Added persisted, fully validated admin operational settings with restart-safe application and reset.
- Added artifact quota/orphan cleanup, strict artifact path isolation, backup CDN fallback and exact GIF frame verification.
- Hardened authenticated session replacement fallback, notification cursor/order checks, exact author-bound publish markers and honest account-side verification status.
- Added complete Docker prepare/verify scripts, read-only container hardening, bounded Docker logs and deployment backup/upgrade instructions.

Real B站 write validation is intentionally not claimed in this release.
