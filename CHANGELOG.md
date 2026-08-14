# Changelog

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
