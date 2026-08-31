# Scope 3 Audit — Backup / Telemetry / Agent Boundary

**Repo**: `pikpik` @ `36f3239` (master) · **Auditor**: Backup/Telemetry/Agent Auditor · **Method**: mimori warmup → map (pkg/backup, pkg/telemetry, pkg/agent) → symbol slices → leaf reads. No subagents.

**Files in scope**: `pkg/backup/**` (engine.go, scheduler.go, multi_db.go, types.go, s3/{client,signer,retention,types}.go), `pkg/telemetry/**` (ring_buffer.go, downsampler.go, proc_reader.go, docker_collector.go, ws_hub.go), `pkg/agent/**` (server.go, client.go, dispatcher.go, types.go), `cmd/pikpik-agent/main.go`, wiring in `cmd/pikpik/main.go`.

---

## Executive Summary

**Overall Health Score: 6.6 / 10**

The boundary is architecturally sound (typed Docker SDK, no shelling, no `/tmp` staging, per-schedule S3 credential scoping, good test coverage for cron/retry/retention), but contains **one critical concurrency deadlock** in the multipart uploader, **one silent data-loss fail-open** in the exec runner, **two agent-channel security gaps** (node identity hijack, Docker API path traversal), and **a retention-pruning gate that ignores weekly/monthly-only policies**.

### Invariant Scorecard

| Invariant | Status | Evidence |
|---|---|---|
| 1 — Zero Shelling | ✅ COMPLIANT | `pkg/backup/engine.go:35-72` uses `ContainerExecCreate/Attach` + `stdcopy`; `multi_db.go` builds argv arrays; zero `exec.Command` in scope (rg-verified; only `pkg/git`, `pkg/build`, `pkg/api/pty.go` — out of scope). Agent dispatcher speaks raw HTTP over unix socket (`dispatcher.go:39-49`) — API-first. |
| 2 — Single Unified Runtime | ✅ COMPLIANT | SQLite WAL via `st.DB()` (`cmd/pikpik/main.go:272`), in-memory ring buffers, in-process cron ticker (`scheduler.go:298-314`). |
| 3 — Dynamic Ingress | N/A (out of scope) | — |
| 4 — Pure Streaming Pipelines | ⚠️ CONDITIONAL | Streaming path is genuine (`io.Pipe` → gzip → multipart, `multi_db.go:266-325`); zero `os.CreateTemp` in scope (verified). **But** the multipart result channel deadlocks for uploads > ~524 MB via a non-cancellable channel send — effectively a bounded-memory violation + goroutine leak for large backups. See F-1. |

---

## Findings

### CRITICAL

#### F-1 · Multipart upload deadlocks (and leaks goroutines) for uploads > ~100 parts — Invariant 4 breach
**`pkg/backup/s3/client.go:278-343`**

The result channel is buffered at 100 (`partsChan := make(chan partResult, 100)`, :278) but is only **drained after `wg.Wait()`** (:329-343). Worker goroutines send results at :311 **before** the deferred `<-semaphore; wg.Done()` (:305-308) runs. Once 100 results sit in the buffer, a completing worker blocks on the channel send **while still holding a semaphore slot**; the main read loop blocks forever at the plain channel send `semaphore <- struct{}{}` (:301) — not in a `select` with `ctx.Done()`, so even context cancellation cannot break it. `wg.Wait()` can never complete because the drain that would unblock workers happens after it.

- Trigger: any backup larger than `100 × PartSizeBytes` (default 5 MB → **~524 MB**; real database dumps exceed this routinely).
- Consequence: backup hangs permanently, goroutines leaked; `scheduler.go:311` runs jobs inline on the ticker loop → the whole scheduler wedges and `Stop()` (`scheduler.go:320-329`) never returns if ctx is background.
- Tests never catch it: `s3_test.go:165,172` uses 1 MB parts / 2.5 MB payload (3 parts).
- Fix (smallest diff): drain `partsChan` in a dedicated goroutine started before the read loop collecting into a mutex-guarded slice, or `select { case partsChan <- r: case <-ctx.Done(): }`. Add a >100-part regression test.

#### F-2 · Nil Docker exec runner silently "succeeds" — empty backups recorded as completed
**`pkg/backup/engine.go:36-38` and `:76-78`** wired via **`cmd/pikpik/main.go:226-230`**

`ExecStreamStdout`/`ExecStreamStdin` return `(0, nil)` when `r.cli == nil`. In `cmd/pikpik/main.go:226-229`, `execRunner` is left `nil` whenever the Docker socket is unreachable, and `NewBackupEngine` accepts it. Downstream, `ExecuteMultiDBBackup` (`multi_db.go:293-340`) sees exit code 0, uploads a gzip of **zero bytes** to S3, records a `completed` execution (`scheduler.go:510-528`), and advances the schedule — **silent data loss with a green dashboard**. A nil runner is an internal invariant violation and must fail fast: return an error, never exit code 0.

#### F-3 · Agent node identity is client-asserted → session hijack / takeover
**`pkg/agent/server.go:169-175, 246`**

`nodeID` is taken verbatim from the `X-Node-ID` header (or query). Any agent holding the single shared enrollment token can register as *another* node's ID; `RegisterNode` (:92-99) blindly overwrites `s.sessions[nodeID]` without closing the previous connection — the displaced session's heartbeat loop keeps running (:253) and the attacker now receives `DispatchCommand` traffic intended for the victim (docker logs/inspect — potentially sensitive). The enrollment token is a **shared bootstrap secret** (auto-generated once, `main.go:149-156`, shown once) used as a long-lived credential for every node. No per-node credential, no challenge, no mTLS requirement server-side.

- Remediation: on overwrite, close the prior session's conn; bind nodeID to the mTLS client cert CN/SAN when certs are configured; move to per-node tokens issued at enrollment time.

#### F-4 · Docker API path traversal in agent dispatcher (`docker.inspect`, `docker.logs`)
**`pkg/agent/dispatcher.go:157, 202`**

`targetID` from the control-plane command payload is interpolated unescaped into the Docker URL: `http://localhost/containers/%s/json`. A crafted ID such as `x/archive?path=/etc/shadow#` rewrites the request path/query (`GET /containers/x/archive?path=/etc/shadow#/json`) → **arbitrary file read out of any container** via the agent's socket credentials. The `tail` parameter (:202) is likewise unvalidated query injection. This is a "parse at the boundary" violation: `CommandPayload.Args` arrive raw and are trusted in the core. Validate against `^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$` and `url.PathEscape` (or use the typed Docker SDK — see below). Positive note: **no `docker.exec` handler is registered** (:53-59) — the agent cannot run arbitrary commands; keep it that way.

### HIGH

#### F-5 · Retention pruning gate ignores weekly/monthly-only policies (duplicated logic)
**`pkg/backup/multi_db.go:343`** and **`pkg/backup/scheduler.go:535`**

Both gates are `(KeepHourly > 0 || KeepDaily > 0 || MaxBackups > 0)` — `KeepWeekly`/`KeepMonthly` are absent. A schedule with only weekly/monthly retention **never prunes**; objects accumulate forever (S3 cost + policy breach). The block is also copy-pasted twice (Rule of Three threshold approaching). Prune errors are discarded with `_, _ =` in both places, so failures are invisible. Fix the predicate to include all five rules and log/delete-list failures.

#### F-6 · `ListObjects` has no pagination — retention silently stops at 1,000 objects
**`pkg/backup/s3/client.go:556-602`**

No `IsTruncated` / `continuation-token` handling. With >1000 objects under a prefix, older objects are invisible to `EvaluateRetention` (`retention.go:45`) → they are never deleted and `MaxBackups` is enforced against a truncated view. Long-lived schedules with hourly backups hit 1000 objects in ~42 days.

#### F-7 · Downsampler re-inserts duplicate hourly rollups; metric pruning is dead code
**`cmd/pikpik/main.go:271-290`** + **`pkg/telemetry/downsampler.go:56-95`**

The background ticker fires every 10 minutes and downsample-inserts the **current, in-progress hour** for every ring buffer. `SaveAggregate` is a plain `INSERT` with no `UNIQUE(entity_type, entity_id, bucket_start)` / upsert → ~6 duplicate rows per entity-hour, and `QueryHourlyMetrics` returns them all (corrupted charts). Additionally, `PruneMetricsOlderThan` (`downsampler.go:150-161`) has **zero callers** — `system_metrics_hourly` grows unboundedly. Fix: upsert on conflict + only finalize *closed* hours, and wire a daily prune.

### MODERATE

#### M-1 · Multipart initiate/complete have no retry; empty ETag accepted
**`client.go:250-259, 391-402, 458-461`** — `uploadPart` correctly retries transport errors and 5xx with jitter, and correctly does **not** retry 4xx (:434-480; tests `s3_test.go:203-357`). But `InitiateMultipartUpload`, `CompleteMultipartUpload`, `ListObjects`, and `DeleteObjects` are single-shot; a transient 500 on Complete aborts an otherwise-successful multi-GB upload. Also :459-461 returns the `ETag` header without checking it's non-empty, and :405 ignores the Complete response body decode — S3's "HTTP 200 + Error XML" failure mode would be reported as success.

#### M-2 · Credential exposure in database process argv
**`multi_db.go:109, 118, 127` and `:168, 177, 186`**

MySQL `"-p"+Password` and Redis `"-a", Password` are passed on the command line (visible in `/proc/<pid>/cmdline` inside the container) even though `MYSQL_PWD` / `REDISCLI_AUTH` env are also set — the argv variants are redundant; drop them. Mongo passes the password inside a `--uri` argv argument **unescaped** (`BuildMongoURI`, :49-74): a password containing `@ : / ?` breaks the URI or injects authority components. Use `url.UserPassword`-style escaping and prefer env (`MONGODB_URI`) over argv.

#### M-3 · Unbounded stderr/stdout buffering in exec runner
**`engine.go:60, 109-110`** — `strings.Builder` accumulates the entire stderr of a dump (and full stdout+stderr of a restore) with no cap. A chatty `pg_dump` or restore error spew breaks the 32 MB bound (Invariant 4). Cap with a truncating writer.

#### M-4 · Unbounded `io.ReadAll` + wrong frame stripping in `docker.logs`
**`dispatcher.go:214-228`** — reads the whole log response into memory (tail is client-controlled, e.g. `all`), then strips 8 bytes **per line**, but Docker's multiplexed stream header is 8 bytes **per frame**, not per line — output is corrupted whenever frames don't align with newlines. Use `stdcopy.StdCopy` (already used in `pkg/backup/engine.go:61`) and enforce a max tail.

#### M-5 · Data race on `NodeSession.LastHeartbeat`
**`server.go:306`** (write in `readLoop`) vs **`server.go:269`** (read in `heartbeatLoop`) with no synchronization — `go test -race` would flag it. Make it `atomic.Int64` (unix nanos).

#### M-6 · Enrollment token accepted via URL query parameter
**`server.go:161`** — `?token=` fallback leaks the shared enrollment secret into Caddy/access logs, proxies, and browser history. Header-only; deprecate the query fallback (the official agent always sends the header, `client.go:127-129`).

### MINOR

- **Mi-1** · SigV4 canonical-query encoding uses `url.QueryEscape` (`signer.go:154`) which encodes space as `+`; SigV4 requires `%20`. Latent — current calls never carry such values. Similarly `uriEncodePath` (`signer.go:161-167`) uses `url.PathEscape`, which leaves sub-delims (`& + = ,`) unencoded, diverging from AWS canonicalization for exotic key names. Use RFC 3986 unreserved-only escaping.
- **Mi-2** · `DownsampleHour` hard-codes the 10 s sampling interval to convert rates→volumes (`ring_buffer.go:129-132`) and `[from, to]` inclusive bounds double-count a point landing exactly on `hourEnd` (:94-95). `uint64(pt.NetRxRate)` truncates fractional rates.
- **Mi-3** · `client.go:49-51` — `!opts.UseSSL && opts.Endpoint != ""` is dead inside `if endpoint == ""`; UseSSL=false still yields https for provider-derived endpoints (safe direction, but dead logic).
- **Mi-4** · `client.go:443` manually sets `Content-Length` — transport recomputes it from `bytes.Reader`; harmless but redundant (and correctly not a signed header).
- **Mi-5** · `retention.go:174-188` — dash-format branch computes `normalized` then discards it (`_ = normalized`); dead code.
- **Mi-6** · `proc_reader.go:205-225` — diskstats filter excludes `loop/ram/zram` but not partitions (`sda1` + `sda` both counted → ~2× disk throughput on partitioned disks).
- **Mi-7** · `server.go:208-210` — `websocket.AcceptOptions{InsecureSkipVerify: true}` (nhooyr: Origin check skipped). Harmless for non-browser agents today, but unnecessary; drop it.
- **Mi-8** · `scheduler.go:349-379` — `RunDueJobs` holds `s.mu` for the full serial execution of all due backups; `Trigger` blocks behind long uploads. Ticker errors discarded (`scheduler.go:311`).
- **Mi-9** · S3 key built from `projectSlug/serviceSlug` without slug re-validation (`multi_db.go:263`) — safe today if API-boundary slug validation holds (see scope_1 audit); a `/` or `..` in a slug would escape the per-service retention prefix.

### Known debt (verified, not re-reported)
- `engine.go:168` — `VerifyBackupEphemeral` is a stub returning `(true, nil)` (:167-173): confirmation that **no restore verification actually happens** while the UI implies it does. Ledger entry accurate; note the stub is fail-open rather than fail-closed.
- `downsampler.go:11`, `ring_buffer.go:72` — confirmed present and correctly scoped (linear GetRange is fine at 8,640 pts).

---

## Dimension Scores

| Dimension | Score | Rationale |
|---|---|---|
| 1. Correctness | **6.2** | Cron parser is genuinely excellent (Vixie DOM/DOW OR semantics `scheduler.go:170-171,198-199`; `7→0` normalization :145-147; macro expansion :40-51; `Next` skip-optimization :182-231). Ring buffer wrap-around math correct (:36-48, :63-68). GFS buckets + `MaxBackups` newest-first truncation correct (`retention.go:135-150`; timestamp parsing tolerant with safe fallback :163-195). But F-1 (multipart deadlock), F-5 (retention gate), F-7 (duplicate rollups), Mi-2 pull it down. |
| 2. Robustness | **6.4** | `uploadPart` retry policy exemplary (5xx+jitter retry, 4xx fail-fast, ctx-aware — `client.go:434-480`); agent reconnect loop exemplary (jittered exp backoff `client.go:90-123`, deadline symmetry 10 s ping / 25 s read / 25 s heartbeat-kill `server.go:269`); scheduler catch-up via `ListDue` prevents missed runs and double-fires. But F-2 fail-open, F-6 pagination, M-1 no-retry on lifecycle ops, and swallowed errors everywhere (`_, _ =` at `multi_db.go:345`, `scheduler.go:289,482,528,537`, `main.go:285`). |
| 3. Performance / Invariant 4 | **7.2** | True streaming `io.Pipe`→gzip→S3 (`multi_db.go:266-325`), zero `/tmp` staging (rg-verified), `countingWriter` stats without extra pass, bounded 5 MB part buffers × 4 workers ≈ 25 MB — under 32 MB **when it works**. Ring buffers bounded (8,640 pts × ~100 B ≈ 1 MB/container). `/proc` sampling is cheap (5 small file reads per tick). Breached in spirit by F-1 (large uploads), M-3, M-4. |
| 4. Security | **6.6** | Strong: per-schedule credential scoping with vault-encrypted secrets (`scheduler.go:460,465,498`; `json:"-"` on Password/SecretKey `backup/types.go:30,35,71,76`), constant-time token compare (`server.go:50`), TLS 1.3 MinVersion (`agent/types.go:48`), no secrets found in any log/error path (rg-verified), no docker.exec handler (dispatcher allow-list :53-59). Weak: F-3, F-4, M-6, shared-token model, mTLS optional on both sides (server never requires client certs). |
| 5. Invariants 1 & 2 | **9.5** | Fully compliant; agent's HTTP-over-unix-socket dispatcher is a legitimate API-first pattern. Minor tension: dispatcher hand-rolls Docker API URLs instead of the typed SDK already in `go.mod` (AHA/LoB) — hand-rolled URL building is exactly what caused F-4. |

**Overall Health Score: 6.6 / 10** — weighted toward Correctness/Security criticals; architecture and test culture are strong, failures are concentrated in concurrency and boundary validation.

---

## Remediation Plan (ranked)

**P1 — this week**
1. **F-1**: drain `partsChan` concurrently (or ctx-aware send); add >100-part regression test. ~30 LOC, `client.go:283-343`.
2. **F-2**: nil runner → return error (`engine.go:36-38,76-78`); make `VerifyBackupEphemeral` fail-closed until implemented.
3. **F-4**: slug-validate + escape `targetID`/`tail` in dispatcher (`dispatcher.go:147-202`); or swap to typed Docker SDK.
4. **F-5**: fix retention gate predicate in **both** sites (`multi_db.go:343`, `scheduler.go:535`); log prune errors; extract to one shared function.
5. **F-3**: close displaced session on nodeID re-register (`server.go:92-99`); prefer cert-bound node identity.

**P2 — next sprint**
6. **F-6**: pagination loop on `ListObjectsV2` (continuation token), `client.go:556-602`.
7. **F-7**: `INSERT ... ON CONFLICT(entity_type, entity_id, bucket_start) DO UPDATE`; downsample only closed hours; schedule `PruneMetricsOlderThan` (`main.go:271-290`).
8. **M-1**: retry initiate/complete (reuse `sleepWithJitter`); validate non-empty part ETags; decode Complete error XML.
9. **M-2**: drop argv credentials (env-only), URL-escape Mongo URI.
10. **M-5/M-6**: atomic LastHeartbeat; remove query-string token fallback.

**P3 — backlog**
11. Mi-1 strict RFC 3986 canonical escaping; Mi-2..Mi-9 cleanups; M-3 bounded stderr writer; M-4 stdcopy for logs.
12. Per-node enrollment (bootstrap token → issued node credential) to retire the shared-secret model.

**Verification**: `export PATH=$PATH:/usr/local/go/bin:/home/devhax/go/bin && go test -race -count=1 ./pkg/backup/... ./pkg/telemetry/... ./pkg/agent/...` — note `-race` currently flags M-5; the new multipart test must exceed 100 parts to lock F-1.

