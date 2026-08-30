# Cross-Boundary Seam & Interface Audit — pikpik

Scope: seams only (API<->Frontend, Persistence, Orchestration<->Ingress,
Build<->Registry<->Deploy, Agent<->Server, Config/Env, Specs vs Runtime).
No in-boundary review performed — six parallel auditors cover that.

Supersedes the 2026-08-30 18:05 version, which predates Git integration, PTY,
the React SPA rewrite, streaming backups, cron scheduler, canary splitting,
and the template marketplace.

## Health Score: 5.8 / 10 (Critical issues present)

Four independently-shipped subsystems (git/build cron scheduler, app git-linking,
plain-app orchestration wiring, build->registry->deploy push) are each fully
built in isolation but never connected across their owning module boundary.
Each gap is invisible to a single-subsystem review because both sides compile,
have tests, and look complete — the break only appears when tracing the seam
end-to-end.

---

## Findings by Seam

### 1. API <-> Frontend Contract

**F1 — CRITICAL — Backup Schedule feature has zero HTTP surface; frontend calls 404 on every action**

- Frontend side: `web/src/lib/api.ts:362-379` defines `api.backupSchedules.{list,create,update,delete}`
  hitting `GET/POST /api/v1/backups/schedules` and `GET/PUT/DELETE /api/v1/backups/schedules/{id}`.
  Consumed live in `web/src/views/BackupsView.tsx` and `web/src/views/DatabasesView.tsx`.
- Backend side: `pkg/api/routes.go` — grep for `schedules` returns **zero matches**. No `mux.Handle`
  for any `/api/v1/backups/schedules*` path exists anywhere in the router.
- The backend engine is otherwise fully implemented and wired: `pkg/store/schedule_store.go` (full
  CRUD, 303 lines), migration `pkg/store/migrations/00004_db_cron_schedules.sql:5-30` (table), and
  `pkg/backup/scheduler.go:305-330` (`RunDueJobs` consuming `s.store.Schedules().ListDue`), started
  from `cmd/pikpik/main.go`. The cron engine runs and executes schedules that already exist in the
  DB — there is simply no way for a user to create one through the product.
- Compounding: even if the route existed, the DTO shapes disagree. `web/src/lib/types.ts:230-244`
  (`BackupSchedule`) declares `cron_expression` (required) + `cron_expr` (optional alias),
  `s3_destination_id`, and a flat `retention_days`. The real wire type,
  `pkg/store/models.go:183-208` (`store.BackupSchedule`), has `cron_expr` only, no
  `s3_destination_id` (it stores `s3_bucket`/`s3_endpoint`/`s3_region`/`s3_access_key` directly),
  and four-tier retention (`retention_hourly/daily/weekly/monthly` + `max_backups`), not
  `retention_days`. Wiring the route today would still ship a broken form.
- Remediation: add the five missing routes in `routes.go` bound to `ctrl` methods that call
  `st.Schedules()`, and rewrite `BackupSchedule`/`CreateBackupScheduleRequest` in `types.ts` to match
  `store.BackupSchedule` field-for-field.

**F2 — CRITICAL — App git-linking fields are accepted by the UI and silently dropped by the API (no persistence path exists)**

- `web/src/lib/types.ts` declares `git_repo_url`, `git_branch`, `dockerfile_path`, `build_strategy`,
  `webhook_secret` on `App` (lines 39-43), `CreateAppRequest` (lines 54-58), and `UpdateAppRequest`
  (lines 67-71) — the file header even claims "Matching Backend Go DTOs".
- `pkg/api/types.go:47-75` (`App`, `CreateAppRequest`, `UpdateAppRequest`) has **none** of these
  fields. `grep -rn "GitRepoURL|GitBranch|DockerfilePath|BuildStrategy|WebhookSecret" pkg/api/*.go`
  returns zero matches — Go's `json.Unmarshal` silently ignores unknown keys, so a
  `POST /api/v1/apps` from the "connect to Git" UI flow returns 200 but every git-linking field is
  discarded.
- Persistence confirms there's nowhere for it to go even if the API were fixed: the `services` table
  (`pkg/store/migrations/00001_initial_schema.sql:91-109`) has no `repo_url`/`branch`/
  `dockerfile_path`/`build_strategy`/`webhook_secret` columns — those columns exist only on the
  per-build-run `builds` table (`00003_git_builds.sql:5-23`), not as durable per-app configuration.
  `pkg/store/models.go` `Service` struct (lines 84-99) confirms the same gap in Go.
- This is silent data loss at a trust boundary per the rubric — scores Critical regardless of the
  9.5+ ceiling other seams in this same file hit.
- Remediation: either add `services.repo_url/branch/dockerfile_path/build_strategy/webhook_secret`
  columns + wire them through `api.CreateAppRequest`/`UpdateAppRequest`/`App` and
  `controller.CreateApp`/`UpdateApp`, or remove the fields from the frontend until the feature is
  real.

**F3 — MODERATE — `builds.cancel` client method targets a route that does not exist**

- `web/src/lib/api.ts:333-335` defines `builds.cancel(buildId)` -> `POST /api/v1/builds/${buildId}/cancel`.
- `pkg/api/routes.go` registers only `GET /api/v1/builds/{build_id}`, `GET .../stream`, and
  `POST .../rebuild` (lines 901-935) — no `/cancel` handler.
- Currently unreachable from any view (`grep -rn "builds.cancel"` across `web/src/views` and
  `web/src/components` returns nothing), so it is dead SDK surface today rather than a live user
  bug — kept at Moderate rather than Critical. Wire the handler (there is a `build.Manager` job
  registry that could support cancellation) or delete the client method.

### 2. Orchestration <-> API/Ingress Contract

**F4 — CRITICAL — Standalone app deploy never creates a Swarm service; only `UpdateService` is called, and its error is swallowed**

- `pkg/api/controller.go:372-410` (`CreateApp`) only populates the in-memory `c.apps` map and writes
  a `store.Service` row — it never calls `c.orch.Swarm().CreateService(...)`.
- `pkg/api/controller.go:468-507` (`DeployApp`), the only other place an app's container state is
  pushed to Docker, calls `_ = c.orch.Swarm().UpdateService(ctx, id, 1, orchestration.ServiceSpec{...})`
  (line 500) — never `CreateService`. `CreateService` (declared in
  `pkg/orchestration/interfaces.go:45`, implemented `pkg/orchestration/swarm.go:111`) is never
  referenced anywhere in `pkg/api/controller.go` (`grep -n "CreateService" pkg/api/controller.go`
  returns nothing).
- Net effect: creating a plain app (the primary, non-template, non-stack, non-git flow) and then
  deploying it calls `UpdateService` against a service ID that was never created — this fails inside
  the Docker Engine client, and the error is discarded via `_ =`, so the API reports
  `Status: "running"` (line 509) and broadcasts `deployment_finished` (lines 510-521) regardless.
  The hardcoded `version=1` passed to `UpdateService` is also wrong for any service that has already
  been updated once (Swarm versions are monotonic), compounding future update failures the same way.
- The same `ServiceSpec` literal at line 500-505 never sets `.Ports`, so even a successful update
  would carry no port mapping — see F5.
- Remediation: `DeployApp` must attempt `CreateService` when no existing Swarm service is found for
  `id` (or `CreateApp` must create it eagerly), propagate the real error instead of `_ =`, and use the
  version returned by a prior `Swarm().GetService` lookup instead of a hardcoded `1`.

**F5 — MODERATE — Canary/traffic-split default upstream is a fictional address (`app.Name + ":80"`) that doesn't match the identifier or port orchestration actually uses**

- `pkg/api/controller.go:630` (`GetAppTraffic` fallback) and `:657` (`SetAppTraffic` fallback) both
  synthesize `app.Name + ":80"` as the default stable upstream when no explicit traffic-split
  override exists yet.
- But the Swarm service is addressed by `id` (the app ID, e.g. `app_xxx`), not `app.Name` — see
  `UpdateService(ctx, id, ...)` at controller.go:500 and `RemoveService(ctx, id)` at line 459. Swarm's
  internal DNS resolves services by the name given in `ServiceSpec.Name`, and the only place that
  spec's `Name` is set is `app.Name` (line 501) — so this pair happens to line up *only* if
  `app.Name` is also unique/DNS-safe, which is not guaranteed (arbitrary user-entered names with
  spaces/mixed case will not resolve). More importantly, the port is hardcoded to `80` regardless of
  the service's actual `container_port` — no code path in `CreateApp`/`DeployApp` ever reads/writes a
  container port for a plain app service at all (there is no `container_port` field on
  `CreateAppRequest`/`api.App` in `pkg/api/types.go`).
- Contrast: `DeployBlueGreen` (controller.go:783) gets this right —
  `StableUpstream: greenID + fmt.Sprintf(":%d", containerPort)` uses the actual configured
  `ContainerPort` from the request. The inconsistency between the two traffic-shaping code paths is
  itself evidence this was never contract-tested end-to-end.
- Remediation: derive the default upstream from the orchestration layer (resolved container IP/port
  for the service ID), not from a string concatenation of user-supplied `Name` and a hardcoded port.

### 3. Build <-> Registry <-> Deploy Contract

**F6 — CRITICAL — Built images are never pushed to the embedded registry; multi-node Swarm deploys cannot pull them**

- `pkg/build/manager.go:176`: `job.ImageTag = fmt.Sprintf("pikpik/%s:%s", appName, shortSHA)` — a bare
  local tag with no registry host/port prefix.
- `grep -rln "ImagePush|\.Push(" pkg/ cmd/` (excluding tests) returns only `pkg/agent/server.go`
  (an unrelated agent-side operation) — there is no push call anywhere in `pkg/build` or
  `pkg/deploy` after a build completes.
- `pkg/registry/manager.go` implements robot-credential management, htpasswd sync, and GC
  (`CreateRobotAccount`, `RevokeRobotAccount`, `GetStatus`, etc.) but nothing in the build pipeline
  ever authenticates to it or issues a push.
- Consequence: a git-triggered build's resulting image exists only in the local Docker image cache of
  whichever node ran the build. `orchestration.ServiceSpec.Image` (consumed at deploy time,
  `pkg/orchestration/types.go:110`) is set to that same bare tag. On a single-node dev setup this
  works by accident; on any real multi-node Swarm cluster — the product's core "Swarm" selling
  point — worker nodes scheduling the service will fail to pull an image that was never pushed
  anywhere they can reach.
- Remediation: after a successful build, push to `127.0.0.1:5000/<tag>` (or the configured registry
  endpoint) using the registry's robot credentials, and set `ServiceSpec.Image` to the
  registry-qualified reference, not the bare local tag.

### 4. Agent <-> Server Contract

**F7 — MODERATE — `"log"` is a documented `StreamMessage.Type` value that neither side implements**

- `pkg/telemetry/types.go:135`: `Type string \`json:"type"\` // "metric" | "log" | "command" | "ack" | "command_response"`.
- Agent send side (`pkg/agent/client.go`) only ever constructs `StreamMessage{Type: "ack"|"command_response"|"metric"}`
  (lines 202-203, 234-235, 260-261, 291-292) — never `"log"`.
- Server receive side (`pkg/agent/server.go:258-274`) switches on `msg.Type` with cases for `"ack"`,
  `"metric"`, `"command_response"` only — no `case "log"`, so an agent that did send one would be
  silently dropped by the `switch`'s implicit no-op default.
- Not currently harmful (nothing sends it), but it's dead protocol surface that will bit-rot further
  if someone implements agent-side log tailing assuming the server already handles the frame type the
  docstring promises.
- Remediation: either implement `case "log":` forwarding (mirroring the `"metric"` branch) or remove
  `"log"` from the doc comment until it's real.

### 5. Config/Env Seam

No drift found. `pkg/config/manager.go:29-68` (`ResolveHierarchy`) implements the documented 4-tier
cascade (`org -> project -> stage -> service`, lines 42-68, later tier overrides earlier) correctly
against `store.ScopeTier` constants. Only caller is `pkg/api/controller.go` `DeployApp` (line 489),
which is architecturally fine since that's the only place resolved env is materialized before a
container update — but note this is also downstream of F4 (the deploy call whose result is
discarded), so correctly-resolved env vars are being handed to a Swarm update call that silently
fails for newly-created apps.

### 6. Persistence Contract

Nullable-column handling is solid where checked: `services.container_port` (nullable) is scanned via
`sql.NullInt64` in `pkg/store/services.go:100,138`; `deployments.commit_sha`/`finished_at`
(nullable) are scanned via `sql.NullString`/`sql.NullTime` in `pkg/store/deployments.go:65-68,102-105`;
`backup_schedules.last_run_at`/`next_run_at` likewise in `pkg/store/schedule_store.go:29,64-69`. No
column/type drift found between `migrations/*.sql` and `models.go`/`types.go` for the tables
inspected. The only persistence-relevant gap is F2 above (missing columns for a feature the frontend
assumes exists).

### 7. Documented Invariants vs Runtime (specs/*.md)

Spot-checked against `specs/05-REGISTRY-AND-STREAMING-BACKUPS-SPEC.md` "Core Invariants" table:

- **Invariant 1 (Zero Shelling)** — held. `grep -rn "exec.Command|os/exec" pkg/backup/*.go` returns
  nothing; backups drive `pg_dump`/etc. via the Docker Exec API, not a shell.
- **Invariant 2 (Zero Host Port Mapping)** — held for the registry container:
  `pkg/registry/manager.go:178` sets `ExposedPorts` only, no `PortBindings`/host port anywhere in
  the file.
- **Invariant 4 (Pure Memory-Bounded Streaming, `io.Pipe`, zero `/tmp` buffering)** — held:
  `pkg/backup/multi_db.go:229` uses `io.Pipe()`; no `os.CreateTemp`/`os.TempDir` in the backup
  package.

These three are Exemplary (9.5+) and worth calling out positively — the streaming-backup subsystem is
the best-executed seam-adjacent code in this audit. The spec's Route Catalog
(`specs/06-API-AND-CLI-SPEC.md:101-165`) predates the cron scheduler and does not itself claim a
`/backups/schedules` route, so F1 is a product gap introduced after that spec was written, not a
spec/code disagreement.

---

## Top 5 Findings (file:line on both sides)

1. **F1 (Critical)** — Backup schedule routes missing.
   Frontend: `web/src/lib/api.ts:362-379`, `web/src/lib/types.ts:230-264`.
   Backend: no match in `pkg/api/routes.go`; real engine at `pkg/store/schedule_store.go:1-303`,
   `pkg/backup/scheduler.go:305-330`, table at `pkg/store/migrations/00004_db_cron_schedules.sql:5-30`.

2. **F2 (Critical)** — App git-linking fields silently dropped.
   Frontend: `web/src/lib/types.ts:39-43,54-58,67-71`.
   Backend: `pkg/api/types.go:47-75` (fields absent); no columns in
   `pkg/store/migrations/00001_initial_schema.sql:91-109`.

3. **F4 (Critical)** — `CreateService` never called for standalone apps; `UpdateService` error swallowed.
   Backend only, both sides of the internal seam: `pkg/api/controller.go:372-410` (CreateApp, no
   orchestration call) vs `pkg/api/controller.go:498-506` (DeployApp, `_ = ...UpdateService(...)`)
   vs `pkg/orchestration/interfaces.go:45` / `pkg/orchestration/swarm.go:111` (`CreateService`,
   unused).

4. **F6 (Critical)** — No image push between build and registry.
   Build: `pkg/build/manager.go:176` (bare local tag).
   Registry: `pkg/registry/manager.go` (credential/GC management only, no push consumer).
   Deploy consumer: `pkg/orchestration/types.go:110` (`ServiceSpec.Image`, receives the unpushed tag).

5. **F5 (Moderate)** — Traffic-split default upstream uses wrong identifier + hardcoded port.
   `pkg/api/controller.go:630,657` (`app.Name + ":80"`) vs the correct pattern already used at
   `pkg/api/controller.go:783` (`greenID + fmt.Sprintf(":%d", containerPort)`).

---

## Actionable Remediations (priority order)

1. Fix F4 first — it silently breaks the primary "create and deploy an app" flow in Swarm mode. Add
   a `CreateService` call gated on service existence in `DeployApp`/`CreateApp`, stop discarding the
   `UpdateService` error, and track the real Swarm service version instead of hardcoding `1`.
2. Fix F6 — add a push step in `pkg/build/manager.go` after a successful build (or in a new
   post-build hook) targeting the embedded registry, and registry-qualify `ImageTag` before it's
   handed to `ServiceSpec.Image`.
3. Fix F2 — either ship the `services` table columns + Go DTO fields for git-linking, or strip those
   fields from `web/src/lib/types.ts` so the UI doesn't imply a feature that silently no-ops.
4. Fix F1 — add the five `/api/v1/backups/schedules*` routes and correct the `BackupSchedule`/
   `CreateBackupScheduleRequest`/`UpdateBackupScheduleRequest` TS shapes to match
   `store.BackupSchedule` (cron_expr, 4-tier retention, direct S3 fields, no `s3_destination_id`).
5. Fix F5 — compute the default traffic-split upstream from the orchestration layer's actual
   service/container port, matching the pattern already correct in `DeployBlueGreen`.
6. Fix F3 and F7 as cleanup — delete or implement `builds.cancel` and the `"log"` StreamMessage type
   so client SDK and wire-protocol surface area doesn't outrun what's actually wired.
