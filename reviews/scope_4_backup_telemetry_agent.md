# Scope 4 Audit: Backup, Telemetry & Agent

**Auditor**: Backup, Telemetry & Agent Auditor
**Date**: 2026-08-30
**Supersedes**: stale 2026-08-30 18:05 review filed under a different scope split (predates the multi-DB streaming backup + cron scheduler commit `9d4eacc`).

**Scope**:
- `pkg/backup/*.go` + `pkg/backup/s3/*.go` (multi-DB streaming backups, S3 client, cron scheduler)
- `pkg/telemetry/*.go` (docker collector, /proc reader, ring buffer, downsampler, WS hub)
- `pkg/agent/*.go` + `cmd/pikpik-agent/*.go` (pikpik-agent static binary)

**Method**: Direct Read/Grep/Bash inspection against `specs/04-AGENT-AND-TELEMETRY-SPEC.md` and `specs/05-REGISTRY-AND-STREAMING-BACKUPS-SPEC.md`. No subagents were spawned per instructions.

---

## Health Score: **5.8 / 10 — Critical**

The core streaming/compression/S3 mechanics are well engineered and genuinely honor the "pure streaming, bounded memory" invariant. However, the two headline features implied by the commit ("multi-DB streaming backups" and "cron scheduler") are **not reachable from the running server at all** — they exist as fully tested, orphaned packages. On top of that there are two crash-class bugs (an index-out-of-range panic on malformed `/proc/stat`, and an unsynchronized map shared between goroutines) and a production wiring bug that ships an S3 client with **no credentials**. Per the rubric, "unauthenticated mutation" and "crash loops" and "silent data loss" are explicit Critical criteria, and this audit found instances of the crash and silent-data-loss classes, which caps the score below 7.0 regardless of the many things done well.

---

## Invariant Breaches

| # | Invariant (spec) | Status | Evidence |
|---|---|---|---|
| 1 | Invariant 3 (spec 05): "Single Unified Runtime... schedulers execute as in-process Go goroutines" | **BROKEN** | `backup.CronScheduler` is never constructed or `.Start()`-ed anywhere outside its own package and tests. `grep -rn "CronScheduler\b"` across the repo returns zero hits outside `pkg/backup/scheduler.go`/`scheduler_test.go`. Scheduled backups never run in production. |
| 2 | Invariant 5 (spec 05): "Universal S3 Interoperability... configurable endpoint resolvers" | **BROKEN** | `cmd/pikpik/main.go:179-182` constructs the *only* live `s3.Client` with `Bucket: "pikpik-backups"` and **no `AccessKeyID`/`SecretAccessKey`/`Region`/`Endpoint`**. Every backup's SigV4 signature is computed with an empty secret key — uploads to any real provider will get `403`. |
| 3 | Per-schedule S3 config (`BackupSchedule.S3Bucket/S3Endpoint/S3AccessKey/S3SecretKeyEncrypted`, `pkg/store/models.go`) | **DEAD DATA** | `BackupJobConfig.S3Bucket` (`pkg/backup/types.go:29`) is populated in `scheduler.go:374` but never read by `ExecuteMultiDBBackup` (`pkg/backup/multi_db.go`) — bucket/creds always come from the single globally-injected `s3.S3Client`, so multi-tenant / per-schedule bucket isolation is not implemented despite the schema supporting it. |
| 4 | Spec §6.4 (telemetry): "hourly aggregation worker" runs "every 60 minutes" | **BROKEN** | `cmd/pikpik/main.go:221-240` ticks every **10 minutes** and always downsamples `t.Truncate(time.Hour)` (the *current*, still-filling hour), not the just-completed hour. Result: duplicate rows per hour (no `UNIQUE` constraint in `00002_telemetry_schema.sql`) and the final ~10 minutes of every hour's samples are never captured in any `DownsampleHour` call (silent data loss). |
| 5 | Design Tenet #4 (spec 04): "Zero Write-Amplification on Ingestion" | **BROKEN** | Same bug as #4 causes ~6x redundant `INSERT`s into `system_metrics_hourly` per entity per hour instead of one. |

---

## Top 5 Findings (exact file:line)

### 1. [CRITICAL] Cron scheduler and multi-DB backup schedules are entirely unwired — feature is dead on arrival
- **Where**: no reference to `backup.NewCronScheduler`/`CronScheduler.Start` anywhere in `cmd/` or `pkg/api/` (verified via repo-wide grep). Also zero references to `store.Schedules()` or `store.BackupSchedule{` outside `pkg/backup/scheduler.go` itself — there is no API route to even create a schedule.
- **Impact**: An operator who believes they configured a recurring backup (the entire point of the feature named in the commit message) gets **silent data loss**: no backup ever executes, and there is no way to create the underlying `BackupSchedule` row through any HTTP endpoint. This is a textbook "silent data loss" Critical per the rubric.
- **Fix**: Wire `backup.NewCronScheduler(st, backupEng, s3Cli)` + `.Start(ctx)` into `cmd/pikpik/main.go` next to the downsampler ticker, and add CRUD API routes (`pkg/api/controller.go`) backed by `store.Schedules()`.

### 2. [CRITICAL] Production S3 client has no credentials; per-schedule S3 config is silently ignored
- **Where**: `cmd/pikpik/main.go:179-182`:
  ```go
  s3Cli, _ := s3.NewClient(s3.ClientOptions{
      Bucket:   "pikpik-backups",
      Provider: s3.ProviderAWS,
  })
  ```
  and `pkg/backup/multi_db.go` (`ExecuteMultiDBBackup`) never reads `cfg.S3Bucket` (confirmed via `grep -n "cfg.S3Bucket" pkg/backup/*.go` — zero hits outside test files).
- **Impact**: Every backup attempt will fail SigV4 auth (empty `AccessKeyID`/`SecretAccessKey`), and even once credentials are added, all schedules/tenants will write to the same hardcoded bucket regardless of what they configured in `BackupSchedule.S3Bucket/S3Endpoint/S3AccessKey/S3SecretKeyEncrypted` — a cross-tenant data-isolation defect once the scheduler above is wired up.
- **Fix**: Build S3 client from persisted config (registry `S3StorageConfig` / per-schedule fields), and construct/cache a per-schedule `s3.Client` inside `ExecuteScheduleJob` using `sch.S3Endpoint/S3Bucket/S3Region/S3AccessKey` + `vault.DecryptString(sch.S3SecretKeyEncrypted)`.

### 3. [CRITICAL] Unsynchronized map shared between telemetry-ingest goroutine and downsample-ticker goroutine → data race / fatal crash
- **Where**: `cmd/pikpik/main.go:200` (`ringBuffers := make(map[string]telemetry.RingBuffer)`) is passed into `agent.NewAgentServer` and mutated under `s.ringMu` inside `pkg/agent/server.go:303-309` (`recordToRingBuffer`) on every inbound "metric" frame from any connected node — but `main.go:232` (`for key, buf := range ringBuffers`) iterates the **same map** every 10 minutes from a separate goroutine with **no lock at all** (the `ringMu` that protects writes is a private field of `defaultAgentServer`, inaccessible to `main.go`).
- **Impact**: Go's runtime detects concurrent map read/write and calls `fatal error: concurrent map iteration and map write`, which is **unrecoverable** (panic/recover does not catch a `fatal` runtime error) — it crashes the entire `pikpik` control-plane process. This will reproduce whenever a new node/container starts sending metrics for the first time (map write) while the 10-minute downsample tick is mid-iteration.
- **Fix**: Expose a thread-safe snapshot/iterator (e.g. `AgentServer.SnapshotRingBuffers() map[string]telemetry.RingBuffer` under `ringMu.RLock()`), and have `main.go`'s downsample loop use that instead of a shared raw map.

### 4. [CRITICAL] Malformed `/proc/stat` line panics the agent (index out of range)
- **Where**: `pkg/telemetry/proc_reader.go:94` (`if len(fields) < 8 { continue }`) followed by `pkg/telemetry/proc_reader.go:105` (`steal, _ := strconv.ParseUint(fields[8], 10, 64)`).
- **Impact**: The guard allows `len(fields) == 8` (indices 0-7 valid) through to an access of `fields[8]`, which panics with `index out of range [8] with length 8`. A `/proc/stat` `cpu` line with only 7 numeric fields (no `steal`/`guest`/`guest_nice` — plausible on older kernels, minimal containers, or any malformed/truncated read under memory pressure) crashes the whole `pikpik-agent` process. This directly contradicts the audit's own robustness requirement ("resilience to malformed /proc data") and the identical bug pattern also exists verbatim in the spec's illustrative code (spec never fixed it; the implementation copied it instead of hardening it — contrast with `readMem`/`readDisk`/`readNet` in the same file, which all correctly bounds-check before indexing).
- **Fix**: Change the guard to `if len(fields) < 9 { continue }` (need index 8), or bounds-check `steal` access individually with a slice-length check before use.

### 5. [MODERATE] Database/S3 secrets passed as CLI arguments; scheduled schedule passwords never decrypted
- **Where**:
  - `pkg/backup/multi_db.go:108` — `cmd = []string{"mysqldump", ..., "-p" + cfg.Password, dbName}` and `:117` — `cmd = []string{"redis-cli", "--rdb", "-", "-a", cfg.Password}`. Both engines *also* set the equivalent env var (`MYSQL_PWD`, `REDISCLI_AUTH`) right next to it, making the `-p`/`-a` CLI argument redundant and strictly worse: process arguments are visible to any other process on the host/container via `/proc/<pid>/cmdline`, `ps aux`, and `docker top`, whereas the env var alone would suffice and is not visible via `ps`.
  - `pkg/backup/scheduler.go:373` — `Password: sch.PasswordEncrypted` is passed straight through to `BuildDumpCommand` with no call to `vault.Decrypt`/`DecryptString` anywhere in `pkg/backup`. Given the field is explicitly named `PasswordEncrypted` and the codebase has a working `crypto.AESVault` (`pkg/crypto/aes_vault.go`) used elsewhere (`config.NewConfigManager`), this is either (a) a name that lies — the value is actually stored in plaintext, a Secret-Isolation-Boundary violation — or (b) a bug that will make every scheduled backup fail auth the moment schedule-creation is wired up to actually encrypt the password. Same issue for `S3SecretKeyEncrypted` (`scheduler.go:407`, stored but never decrypted/used).
- **Fix**: Drop the `-p`/`-a` command-line flags entirely (env var is sufficient for both mysqldump and redis-cli); call `vault.DecryptString` on `PasswordEncrypted`/`S3SecretKeyEncrypted` before use in `ExecuteScheduleJob`.

---

## Other Notable Findings

### Correctness
- **`cmd/pikpik-agent/main.go:121-123`** silently discards `PIKPIK_INSECURE_SKIP_VERIFY`, `PIKPIK_HOST_METRIC_INTERVAL_SEC`, `PIKPIK_CONTAINER_METRIC_INTERVAL_SEC` when set *only* inside the `--config agent.env` file (exactly the deployment mode documented in spec §2.2/2.3's systemd unit `ExecStart=... run --config /etc/pikpik/agent.env`). `parseFlagsAndEnv` calls `loadEnvFile()` at line 104, then unconditionally overwrites those three `cfg` fields from local flag-default variables at lines 121-123, clobbering whatever the file just set. `cmd/pikpik-agent/main_test.go`'s `TestLoadEnvFile` does not catch this because it calls `loadEnvFile()` directly instead of exercising `parseFlagsAndEnv(["run","--config",path])`, so the regression is untested. Net effect: TLS verification cannot be disabled, and metric cadence cannot be tuned, via the documented config-file mechanism — only via real process env vars or CLI flags.
- **`pkg/agent/dispatcher.go:180-234`** `handleDockerLogs` strips the first 8 bytes of *every line* of the response body, assuming Docker's stdcopy multiplex header (8 bytes) precedes every newline-delimited line. In reality the 8-byte header precedes each multiplexed *frame*, not each line, and containers started with a TTY (`Tty: true`) have **no** multiplex header at all. For TTY containers or any multi-line-per-frame chunk, this corrupts the returned logs by chopping real log content.
- **`pkg/agent/dispatcher.go:157,202`** (`handleDockerInspect`/`handleDockerLogs`) interpolate the caller-supplied `targetID` directly into a Docker Engine API URL via `fmt.Sprintf` with no character-set validation or `url.PathEscape`. A crafted ID containing `/` (e.g. `x/../../images/json`) could redirect the request to a different Docker API endpoint than intended.
- **Downsampler cadence/idempotency** (see Invariant Breach #4/#5 above) — `pkg/telemetry/downsampler.go:56` `SaveAggregate` is a bare `INSERT`, and `pkg/store/migrations/00002_telemetry_schema.sql` has no `UNIQUE(entity_type, entity_id, bucket_start)` constraint to make it idempotent even if the caller were fixed to run once/hour.
- **Path traversal in S3 key naming**: `pkg/backup/multi_db.go:226` builds the key as `fmt.Sprintf("backups/%s/%s/...", projectSlug, serviceSlug, ...)` with no sanitization of `projectSlug`/`serviceSlug`. Upstream, `pkg/api/controller.go:404` and `:1043` set `Slug: strings.ToLower(req.Name)` — i.e., **project/service slugs are user-supplied names, only lowercased, never validated against a safe charset**. A project or service named e.g. `../../other-tenant` would produce backup keys that escape the intended `backups/<project>/<service>/` prefix. On S3 proper this just becomes a literal (if odd) key, but on filesystem-backed S3-compatible stores (MinIO in local mode, which pikpik explicitly supports per spec §6.1) a `../` segment can escape the intended on-disk directory.

### Robustness
- **`pkg/backup/engine.go:36-38,76-78`** `SocketDockerExecRunner.ExecStreamStdout`/`ExecStreamStdin` silently `return 0, nil` (success, zero bytes) if `r.cli == nil`. If this path is ever reached (e.g. via any future refactor that permits constructing the runner without validating `cli`), a backup would "succeed" with 0 bytes copied — a corrupt/empty backup silently reported as healthy. Not currently reachable in tests (`mockExecRunner` is used instead), but it's dead defensive code that should fail loudly (`panic` or explicit error) rather than fabricate success.
- **`pkg/telemetry/docker_collector.go:262-266,87`** `lastAttempt` map (rate-limit bookkeeping keyed by container ID) is never pruned/evicted — only `streams` is cleaned up on stop. Long-running nodes with high container churn (CI runners, restart loops) will leak one map entry per unique container ID for the life of the agent process.
- **`pkg/backup/scheduler.go:306-332`** `RunDueJobs` processes all due schedules sequentially under a single `sync.Mutex`. A single slow/hanging backup blocks all other due schedules from running until it completes (or the calling context is cancelled) — no per-schedule concurrency or timeout is enforced at the scheduler layer (though `ExecuteMultiDBBackup` itself is streaming and shouldn't hang indefinitely absent a stuck container).
- **`pkg/telemetry/ws_hub.go:229-313`** (`wsHub.HandleWebSocket`) has zero authentication (`clientID := r.RemoteAddr`, no token check) and sets `InsecureSkipVerify: true` (disabling Origin-header CSWSH protection) on `websocket.Accept`. This is currently **dead code** — grep confirms it is never mounted as an HTTP route in `cmd/pikpik/main.go` (the live dashboard WS hub is the separate `pkg/api` implementation) — but if it is ever wired up as-is, it is an open, unauthenticated telemetry feed.
- **`pkg/agent/server.go:136,149`** the agent enrollment token may be passed via URL query string (`r.URL.Query().Get("token")`) as a fallback to the `Authorization` header. Query-string secrets get written to reverse-proxy/ingress access logs (Caddy, per spec's own architecture) and browser history if ever hit from a browser context — prefer requiring the header only, or explicitly document/short-lived-scope the query-param fallback.

### Performance
- The S3 multipart upload path (`pkg/backup/s3/client.go:220-269`) correctly bounds concurrent in-flight part buffers via a semaphore sized to `MaxConcurrency` (default 4 × 5 MB = 20 MB), matching the spec's ≤32 MB peak-RAM claim — verified no `bytes.Buffer`/`io.ReadAll` accumulation of the full dump anywhere in the hot path (`ExecuteMultiDBBackup` → `io.Pipe` → `gzip.Writer` → `UploadStreamMultipart`). The "pure streaming" claim holds.
- `pkg/telemetry/ring_buffer.go:73-90` `GetRange` is an O(n) linear scan (8,640-entry cap) with an already-documented `# ponytail:` debt marker; acceptable at current scale.
- Confirmed the P95 index-off-by-one present in the spec's illustrative code (`p95Idx := int(float64(len(cpuValues)) * 0.95)`, which can equal `len` for exact multiples) was **fixed** in the real implementation (`downsampler.go:137`, `ring_buffer.go:137`: uses `len(cpuValues)-1`) — good catch by the implementer, called out as a positive.

### Security
- SigV4 signer (`pkg/backup/s3/signer.go`) canonicalization (headers, URI-encoding, query string, credential scope) looks correct for the supported providers (path-style and virtual-hosted).
- `AbortMultipartUpload` is correctly invoked internally by `s3.DefaultS3Client.UploadStreamMultipart` on any part-upload or completion error (`client.go:289-291,297-299,312-313,320-321,326-328,335-336,342-344`), satisfying spec §5.3's orphan-part cleanup requirement — despite `ExecuteMultiDBBackup` itself never calling Abort directly, the invariant is honored one layer down.
- No `os/exec` usage found anywhere in `pkg/backup` — all container interaction goes through the Docker Engine API client with typed `[]string` command arrays (Invariant 1, "Zero Shelling," is honored).
- Command dispatch on the agent side (`pkg/agent/dispatcher.go`) uses a fixed handler-name allowlist (`ping`, `host.info`, `docker.ps`, `docker.inspect`, `docker.logs`) with no shell/raw-exec handler registered — `docker.exec` is mentioned in `CommandPayload.Command`'s doc comment but has no registered handler, so it currently just 404s with "unknown command" (spec-drift, not a vulnerability, but should be reconciled — either implement it with a proper allowlisted exec surface, or remove it from the doc comment).

---

## Actionable Remediations (priority order)

1. **Wire the cron scheduler into `cmd/pikpik/main.go`** (`backup.NewCronScheduler(...).Start(ctx)`) and add the missing `BackupSchedule` CRUD API routes — otherwise the entire feature this scope was named for does not run.
2. **Fix the production S3 client construction** in `cmd/pikpik/main.go:179-182` to source real credentials (from config/vault), and make `ExecuteMultiDBBackup`/`ExecuteScheduleJob` build/use a per-schedule S3 client from `BackupJobConfig.S3Bucket`/schedule S3 fields instead of ignoring them.
3. **Fix the `ringBuffers` map race**: add a `SnapshotRingBuffers()` method to `AgentServer` guarded by `ringMu`, and use it from the `main.go` downsample loop instead of ranging the shared map directly.
4. **Fix the `/proc/stat` index-out-of-range** at `pkg/telemetry/proc_reader.go:94` (`len(fields) < 9`).
5. **Fix the downsample ticker** to run once per hour against the just-completed hour (`hourStart := t.Add(-time.Hour).Truncate(time.Hour)` on an hourly ticker aligned to the wall clock, or track "last downsampled hour" per entity) and add a `UNIQUE(entity_type, entity_id, bucket_start)` index + `INSERT OR REPLACE`/upsert in `SaveAggregate`.
6. **Fix `cmd/pikpik-agent/main.go:121-123`** to only overwrite `cfg.InsecureSkipVerify`/`HostMetricInterval`/`ContainerMetricInterval` when the corresponding CLI flag or process env var was actually supplied, not unconditionally after `loadEnvFile`.
7. Drop the redundant `-p`/`-a` CLI password arguments in `pkg/backup/multi_db.go` (rely on `MYSQL_PWD`/`REDISCLI_AUTH` env vars only); wire `vault.DecryptString` into `ExecuteScheduleJob` for `PasswordEncrypted`/`S3SecretKeyEncrypted`.
8. Validate/sanitize project & service slugs at creation time (`pkg/api/controller.go:404,1043`) to a safe charset (`^[a-z0-9-]+$`) before they flow into Docker resource names and S3 key paths.
9. Fix `handleDockerLogs`'s log-demuxing to properly parse the stdcopy frame format (or detect TTY mode and skip demuxing), and validate/escape `targetID` before URL interpolation in `dispatcher.go`.

## Deferred / Not Actioned
- Dead-code `wsHub.HandleWebSocket` auth gap (finding under Robustness) — flagged but not fixed since the handler is currently unreachable; fix should happen if/when it is ever mounted.
- `docker.exec` command doc/implementation drift — cosmetic, left for product decision on whether to implement or remove from the comment.

---

*No subagents were used for this audit per explicit instruction; all findings were derived from direct `Read`/`Grep`/`Bash` inspection of the listed files plus the two referenced specs.*
