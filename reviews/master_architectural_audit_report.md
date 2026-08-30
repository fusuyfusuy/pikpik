# pikpik — Master Architectural Audit Report
**Date:** 2026-08-30 · **Baseline:** `master` @ `9d4eacc` · **Supersedes:** commit `2964ae3` (18:14) review, stale after 5 subsequent feature commits

## 1. Executive Scorecard

| # | Scope | Health Score | Band | Report |
|---|---|---|---|---|
| 1 | Core Store, Auth & Config | 5.9 / 10 | Critical | [scope_1_core_store_auth.md](file:///home/devhax/projects/fusuycorp/pikpik/reviews/scope_1_core_store_auth.md) |
| 2 | Orchestration & Ingress | 6.3 / 10 | Critical | [scope_2_orchestration_ingress.md](file:///home/devhax/projects/fusuycorp/pikpik/reviews/scope_2_orchestration_ingress.md) |
| 3 | Git / Build / Registry / Templates | 6.1 / 10 | Critical | [scope_3_git_build_registry_templates.md](file:///home/devhax/projects/fusuycorp/pikpik/reviews/scope_3_git_build_registry_templates.md) |
| 4 | Backup / Telemetry / Agent | 5.8 / 10 | Critical | [scope_4_backup_telemetry_agent.md](file:///home/devhax/projects/fusuycorp/pikpik/reviews/scope_4_backup_telemetry_agent.md) |
| 5 | API Gateway & CLI/Daemon | 4.3 / 10 | Critical | [scope_5_api_cli_daemon.md](file:///home/devhax/projects/fusuycorp/pikpik/reviews/scope_5_api_cli_daemon.md) |
| 6 | Frontend Web SPA | 7.6 / 10 | Moderate | [scope_6_frontend_web.md](file:///home/devhax/projects/fusuycorp/pikpik/reviews/scope_6_frontend_web.md) |
| 7 | Cross-Boundary Seams | 5.8 / 10 | Critical | [scope_seams.md](file:///home/devhax/projects/fusuycorp/pikpik/reviews/scope_seams.md) |
| | **Overall (unweighted mean)** | **6.0 / 10** | **Critical** | |

**Reading the scores:** six of seven scopes land Critical (<7.0) per the standardized rubric. This is not seven unrelated bugs — it's one recurring pattern: **individual subsystems are well-built in isolation** (the auditors independently praised the crypto primitives, the Docker SDK usage, the streaming backup engine, the React hooks) **but the connective tissue between them — auth gates, wiring, and cross-module contracts — is where every serious defect lives.**

## 2. Cross-Cutting Pattern: "Fail-Open at the Boundary"

Three independent auditors (scopes 3, 5, and the seam auditor) converged on the same root defect from different angles: webhook and role-based auth checks in this codebase are written as *"skip verification if the credential is absent"* rather than *"deny if verification cannot be performed."* This single pattern produces:
- An unauthenticated build-trigger (webhook signature)
- An unauthenticated generic-webhook build-trigger (deploy token)
- An unauthenticated root host shell (PTY role check)

All three are the same bug shape, in three different files. This should be fixed as one change (a shared "require-if-configured" helper), not three.

## 3. P1 — Security & Crash (fix before any production exposure)

| # | Finding | Location | Scope |
|---|---|---|---|
| 1 | GitHub webhook HMAC fails open when signature header is absent | [controller.go#L1485](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/controller.go#L1485) | 3, 5 |
| 2 | Generic git webhook token check fails open when `DeployTokenHash` unset | [controller.go#L1562](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/controller.go#L1562) | 3, 5 |
| 3 | `git.CloneRepository` has no URL-scheme allowlist — chainable with #1 via `ext::` transport for command execution on the build host | [clone.go#L26-74](file:///home/devhax/projects/fusuycorp/pikpik/pkg/git/clone.go#L26) | 3 |
| 4 | Hardcoded AES master key — every unmodified install shares one encryption key | [main.go#L154](file:///home/devhax/projects/fusuycorp/pikpik/cmd/pikpik/main.go#L154) | 5 |
| 5 | PTY host-shell role gate fails open on empty role — grants root host shell with no auth (proven by the package's own test) | [pty.go#L117-122](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/pty.go#L117) | 5 |
| 6 | Default admin credentials (`admin@pikpik.local`/`pikpikAdmin123!`) + login rate limiter exists but is never wired to `/auth/login` | [main.go#L111](file:///home/devhax/projects/fusuycorp/pikpik/cmd/pikpik/main.go#L111), [ratelimit.go#L86](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/ratelimit.go#L86) | 5 |
| 7 | Concurrent unsynchronized map read/write on `ringBuffers` → fatal unrecoverable crash | [main.go#L200,232](file:///home/devhax/projects/fusuycorp/pikpik/cmd/pikpik/main.go#L200) vs [server.go#L303-309](file:///home/devhax/projects/fusuycorp/pikpik/pkg/agent/server.go#L303) | 4 |
| 8 | `/proc/stat` parser out-of-bounds panic on truncated/malformed line | [proc_reader.go#L94,105](file:///home/devhax/projects/fusuycorp/pikpik/pkg/telemetry/proc_reader.go#L94) | 4 |
| 9 | Unauthenticated deploy-webhook DoS: rate limiter keys on spoofable `X-Forwarded-For` | [handler.go#L307-322](file:///home/devhax/projects/fusuycorp/pikpik/pkg/deploy/handler.go#L307) | 2 |
| 10 | Registry allowlist typosquat bypass (`HasPrefix` matches `ghcr.io/fusuycorpevil/...`) | [handler.go#L147](file:///home/devhax/projects/fusuycorp/pikpik/pkg/deploy/handler.go#L147) | 2 |
| 11 | `SessionVersion`/`SessionStore.Create` never invoked — password rotation does not revoke sessions | [users.go#L111](file:///home/devhax/projects/fusuycorp/pikpik/pkg/store/users.go#L111), [auth_service.go#L58-145](file:///home/devhax/projects/fusuycorp/pikpik/pkg/auth/auth_service.go#L58) | 1 |
| 12 | SQLite per-connection pragmas (`foreign_keys`, `busy_timeout`) dropped on pooled connections — reproduced live | [sqlite.go#L44-87](file:///home/devhax/projects/fusuycorp/pikpik/pkg/store/sqlite.go#L44) | 1 |
| 13 | `writeMu` write-serialization only fires inside `WithTx`, which has zero production callers — no write serialization actually happens | [sqlite.go#L89-159](file:///home/devhax/projects/fusuycorp/pikpik/pkg/store/sqlite.go#L89) | 1 |

## 4. P2 — Invariants, Wiring & Correctness (breaks features; not directly exploitable)

| # | Finding | Location | Scope |
|---|---|---|---|
| 1 | New-app deploy calls only `UpdateService`, never `CreateService`; error discarded via `_ =` — reports "running" while nothing exists in Swarm | [controller.go#L372-506](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/controller.go#L372) | Seams |
| 2 | Cron scheduler + multi-DB backup schedules fully built and tested, never started or exposed via API — users believe backups run; none do | [main.go](file:///home/devhax/projects/fusuycorp/pikpik/cmd/pikpik/main.go), no route in [routes.go](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/routes.go) | 4, Seams |
| 3 | Production S3 client built with zero credentials; per-schedule S3 bucket/creds stored but never read | [main.go#L179](file:///home/devhax/projects/fusuycorp/pikpik/cmd/pikpik/main.go#L179), [multi_db.go](file:///home/devhax/projects/fusuycorp/pikpik/pkg/backup/multi_db.go) | 4 |
| 4 | Built images get a bare local tag, never pushed to registry — multi-node Swarm deploys can't pull them | [manager.go#L176](file:///home/devhax/projects/fusuycorp/pikpik/pkg/build/manager.go#L176) | Seams |
| 5 | App git-linking fields (`git_repo_url`, `build_strategy`, ...) accepted by UI, silently discarded server-side — no DB columns exist | [types.ts#L39-43](file:///home/devhax/projects/fusuycorp/pikpik/web/src/lib/types.ts#L39) vs [migrations](file:///home/devhax/projects/fusuycorp/pikpik/pkg/store/migrations/00001_initial_schema.sql) | Seams |
| 6 | Canary/blue-green rollout silently reverted to 100% stable traffic by unrelated ingress config reconciliation | [manager.go#L117-207](file:///home/devhax/projects/fusuycorp/pikpik/pkg/ingress/manager.go#L117) | 2 |
| 7 | Placement constraint validation fully built & tested, never invoked before `ServiceCreate`/`ServiceUpdate` | [swarm.go#L490-496](file:///home/devhax/projects/fusuycorp/pikpik/pkg/orchestration/swarm.go#L490) | 2 |
| 8 | Live log streaming (`Follow` mode) silently dies at exactly 60s — shared client-wide HTTP timeout | [engine.go#L31](file:///home/devhax/projects/fusuycorp/pikpik/pkg/orchestration/engine.go#L31), [logs.go#L65-110](file:///home/devhax/projects/fusuycorp/pikpik/pkg/orchestration/logs.go#L65) | 2 |
| 9 | Template deployer has no rollback/compensation on mid-stack failure — orphaned containers/volumes/DB rows | [deployer.go#L279-291](file:///home/devhax/projects/fusuycorp/pikpik/pkg/templates/deployer.go#L279) | 3 |
| 10 | `sanitizeResolvedVariables` is a dead no-op — auto-generated secrets return in plaintext via deploy API response | [deployer.go#L525-531](file:///home/devhax/projects/fusuycorp/pikpik/pkg/templates/deployer.go#L525) | 3 |
| 11 | Traffic-split default upstream uses wrong identifier + hardcoded port, inconsistent with the correct pattern already used elsewhere | [controller.go#L630,657](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/controller.go#L630) vs [#L783](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/controller.go#L783) | 2, Seams |
| 12 | Compose rollback leaks volumes, swallows `NetworkCreate`/`VolumeCreate` errors | [compose.go](file:///home/devhax/projects/fusuycorp/pikpik/pkg/orchestration/compose.go) | 2 |
| 13 | `agent.Server`↔caller "log" `StreamMessage` type documented but implemented on neither side | [pkg/agent](file:///home/devhax/projects/fusuycorp/pikpik/pkg/agent) | Seams |

## 5. P3 — Polish, Docs, Minor

- No React error boundary anywhere — a render exception in any of 12 views white-screens the app ([web/src/main.tsx#L19-27](file:///home/devhax/projects/fusuycorp/pikpik/web/src/main.tsx#L19))
- `AppLogsViewer` renders 200 hardcoded synthetic log lines behind a pulsing "Live" badge — no real SSE wiring ([AppsView.tsx#L1521-1580](file:///home/devhax/projects/fusuycorp/pikpik/web/src/views/AppsView.tsx#L1521))
- 1.18MB monolithic frontend bundle, no code-splitting on 12 static view imports ([App.tsx#L9-20](file:///home/devhax/projects/fusuycorp/pikpik/web/src/App.tsx#L9))
- Bearer tokens passed via URL query string for SSE/WS connections, landing in access/proxy logs ([useSSE.ts#L51-58](file:///home/devhax/projects/fusuycorp/pikpik/web/src/hooks/useSSE.ts#L51), [usePTY.ts#L79-82](file:///home/devhax/projects/fusuycorp/pikpik/web/src/hooks/usePTY.ts#L79))
- User-enumeration timing side channel (non-existent email skips Argon2id) ([auth_service.go#L58-73](file:///home/devhax/projects/fusuycorp/pikpik/pkg/auth/auth_service.go#L58))
- Config manager misfires on any plaintext value prefixed `"v1:"` instead of gating on `IsSecret` ([manager.go#L58-71](file:///home/devhax/projects/fusuycorp/pikpik/pkg/config/manager.go#L58))
- CORS unconditionally `*` despite unused `EnableCors`/`AllowedOrigins` config; no uniform `MaxBytesReader` on JSON bodies
- ~10/21 marketplace templates pinned to `:latest`
- World-readable (`0644`) registry `htpasswd`/`config.yml` with plaintext S3 credentials embedded

## 6. What's Actually Solid (don't relitigate these)

- Argon2id, AES-256-GCM, and the GitHub webhook HMAC *comparison itself* (constant-time) are correctly implemented
- Zero shell-out / zero `os/exec` string interpolation across orchestration and ingress — the "typed Docker SDK" invariant genuinely holds
- Streaming backup path is genuinely streaming — no full-buffer accumulation, correct SigV4, correct abort-on-failure, ~20MB peak RAM verified
- 4-tier config resolver precedence and nullable-column DB mapping — no drift found
- SSE/PTY React hooks correctly tear down connections and timers on unmount
- Static SPA embed.FS server (`cmd/pikpik/static.go`) — no path traversal, correct API/WS exclusion from HTML fallback

## 7. Recommended Remediation Order

1. **P1 items 1–2** (the fail-open webhook pattern) — one shared fix closes two independently-discovered unauthenticated build-trigger paths.
2. **P1 items 3–6** (clone SSRF, hardcoded key, PTY fail-open, default creds) — each is independently a full compromise path.
3. **P1 items 7–8** (crash bugs) — trivial fixes, currently live crash-on-use bugs in shipped code.
4. **P2 item 1** (CreateService gap) — arguably higher priority than any P2 item since it breaks the core product flow (new app deploys silently no-op); consider promoting to P1-adjacent.
5. Remaining P1 items 9–13, then P2 in listed order, then P3.
