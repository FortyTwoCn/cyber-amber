# Changelog

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
