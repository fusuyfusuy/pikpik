# Scope 2 Audit — Orchestration & Runtime Engines Boundary

**Auditor**: Orchestration & Runtime Engines Auditor
**Commit**: `36f3239` (master)
**Scope**: `pkg/orchestration/**`, `pkg/deploy/**`, `pkg/build/**`, `pkg/registry/**`, `pkg/templates/**`, `pkg/git/**` (+ boundary call-sites in `pkg/api/controller.go`)
**Verification**: `go build ./pkg/...` ✅ · `go test -count=1 ./pkg/{orchestration,deploy,build,registry,templates,git}/...` — all 6 packages **ok** ✅

---

## 1. Executive Scorecard

| Dimension | Score | Rating |
|---|---|---|
| 1. Correctness (reconciliation, rolling state machine, rollback, DAG, constraints) | **6.4** | Critical |
| 2. Robustness (socket errors, partial-failure recovery, ctx, goroutines, retry) | **6.7** | Critical |
| 3. Performance (streaming, bounded memory, event loop, goroutines) | **8.5** | Minor |
| 4. Security (HMAC, SSRF, secrets, token auth, registry creds) | **6.2** | Critical |
| 5. Invariant 1 — Zero Shelling | **9.0** | Minor |
| **Overall Health Score** | **7.4 / 10** | **Moderate (Critical-weighted)** |

**Invariant status**: Invariant 1 upheld in letter (no `sh -c`/`bash` anywhere in scope) with one transitive gray zone (nixpacks→docker CLI). Invariant 4 partially breached (build workspaces + logs default to `os.TempDir()`; per-line unbuffered log writes).

---

## 2. Critical Findings (P1)

### F1 — Generic git webhook fails open: unauthenticated build trigger with attacker-controlled clone URL
`pkg/api/controller.go:2930-2936` — `HandleGenericGitWebhook` only enforces the deploy token **when the service already has `DeployTokenHash` set**: `if err == nil && svc != nil && svc.DeployTokenHash != "" { ... }`. If `svc` is nil, lookup errors, or no token was ever configured, **the check is skipped entirely** and a build is enqueued. The `PushEvent` is then populated from the request — including `clone_url`/`repo_url` **query parameters** (`pkg/git/webhook.go:311-316`) — which flows into `BuildJob.RepoURL` (`controller.go:2948`) → `git.CloneRepository`. An unauthenticated remote caller can trigger arbitrary git clones and docker builds on the host.

### F2 — SSRF via clone URL: no internal-host blocklist
`pkg/git/clone.go:40-57` — `validateRepoURL` allowlists schemes (`https`, `http`, `ssh`, scp-like) but performs **no host validation**: no DNS resolution, no private/loopback/link-local IP rejection. Combined with F1, `http://169.254.169.254/...` or `http://127.0.0.1:2019/` (Caddy admin!) are valid clone targets; git will `GET` them, and clone stderr can leak response content into build logs. Plaintext `http` is also in `GIT_ALLOW_PROTOCOL` (`clone.go:122`), exposing embedded tokens in transit.

### F3 — Unvalidated `CommitSHA` → git flag injection
`pkg/git/clone.go:155,162,165` — `opts.CommitSHA` comes from webhook payloads and is passed **without validation and without a `--` separator** to `git checkout <sha>` and `git fetch --depth=N origin <sha>`. `RepoURL` and `Branch` are hardened against flag injection (`clone.go:40-57,96-98`; tests `clone_test.go:161-192`), but `CommitSHA` is not — a value like `--upload-pack=<cmd>` reaches `git fetch` as an option. Fix: validate `^[0-9a-f]{4,64}$` (or reject leading `-`) and add `--` before the SHA.

### F4 — Compose stack rollback is structurally broken for multi-service updates
`pkg/orchestration/compose.go:822-824` retires (stops **and removes**) the old container immediately after each service's health gate passes. If a *later* service in the topological order fails, the rollback path (`compose.go:543-554`) attempts `_ = m.containers.Start(ctx, item.oldContainerID)` on containers that **no longer exist** — the restart silently fails, leaving previously-updated services **down** with no old version to return to. Rollback only works for the single service that failed. Fix: stop-but-don't-remove old containers until the *entire stack* is healthy (two-phase commit), or recreate-from-spec on rollback.

### F5 — `BuildManager.Enqueue` can panic: send on closed channel
`pkg/build/manager.go:163-168` checks `bm.closed` under `RLock`, releases the lock, then sends at `manager.go:233-238`. `Close()` (`manager.go:270-286`) closes `bm.queue` under a *different* critical section. A concurrent `Close()` between check and send → **`panic: send on closed channel`**, crashing the control plane. Fix: hold the read lock across the select-send, or never close the queue channel (workers already exit on `closeChan`).

### F6 — Swarm-path `DeployApp` swallows orchestration errors and reports success
`pkg/api/controller.go:1078-1080` — both `UpdateService` and `CreateService` errors are discarded (`_ =`), then the function falls through to `app.Status = "running"` + `deployment_finished` broadcast (`controller.go:1120-1136`). In Swarm mode a failed rollout is reported healthy to the store, WS hub, and SSE. The standalone-container path (`controller.go:1098-1116`) handles this correctly — the swarm path is asymmetric. Related: `pkg/build/manager.go:496-510` logs a deploy failure as a mere `WARNING`, still marks the build `StatusSuccess`, and overwrites the service record to `running`.


---

## 3. High/Moderate Findings (P2)

### F7 — Registry "reload" is a SIGHUP kill; robot credentials are memory-only
`pkg/registry/manager.go:320-323` — `syncHtpasswdLocked` sends `SIGHUP` via `ContainerKill` to reload htpasswd. Docker distribution **does not handle SIGHUP**; the default disposition terminates PID 1, so every robot-account create/revoke *kills* the registry and relies on `unless-stopped` to resurrect it (push/pull outage window, in-flight layer uploads aborted). Worse, `m.robotAccounts` is an in-memory map with no store persistence — after a control-plane restart the map is empty, and the next `syncHtpasswdLocked` (`manager.go:310-318`) **rewrites the htpasswd file with zero entries**, silently orphaning all previously issued robot credentials.

### F8 — No image pull anywhere before create/deploy
No `ImagePull` call exists in non-test code. `DockerContainerManager.Create` (`pkg/orchestration/containers.go:28`), swarm `CreateService` (`swarm.go:111-128`), template deployer (`pkg/templates/deployer.go:283`), and stack deployer (`compose.go:773`) all assume the image is already local. On a fresh node, `ContainerCreate` fails with "No such image" and template deploys burn through the rollback path. The registry push (`build/manager.go:459-483`) mitigates this for *built* images but not for marketplace templates or compose stacks referencing external images.

### F9 — Rolling update with published host ports always fails (start-first port collision)
Both `DeployWithRollingUpdate` (`containers.go:355-486`) and `DeployStack` (`compose.go:666-828`) start the new container **before** stopping the old one. If the spec binds `HostPort` (`containers.go:59-66`), the new container can never start while the old one holds the port → every rolling update of a port-published service fails probation and rolls back. No stop-first fallback or port rebinding exists.

### F10 — SSH clone disables host-key verification
`pkg/git/clone.go:139` — `GIT_SSH_COMMAND="ssh -i ... -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"`. A network-position attacker can MITM private repo clones and serve malicious source that then gets **built and deployed**. Fix: pin a known_hosts entry per service instead of `/dev/null`.

### F11 — Build-context path traversal via `ContextDir` / `DockerfilePath`
`pkg/build/dockerfile.go:49,57` — `filepath.Join(srcDir, opts.ContextDir)` and `filepath.Join(contextDir, dockerfileName)` accept `../..` segments escaping the cloned workspace. `CreateTarStream` (`dockerfile.go:128-212`) then faithfully tars whatever directory results — a build job can exfiltrate arbitrary host filesystem trees (e.g. `/var/lib/pikpik`) into a Docker build context. Fix: `filepath.Clean` + reject results escaping `srcDir`.

### F12 — GitHub webhook fails open when secret is empty
`pkg/api/controller.go:2856-2860` — `if secret != "" { ...verify... }`. A never-configured webhook secret disables signature verification entirely instead of rejecting. Fail closed when the integration is enabled. `VerifyGitHubSignature` itself is exemplary (prefix-tolerant, hex decode, length check, `subtle.ConstantTimeCompare` — `pkg/git/webhook.go:30-59`).

### F13 — Registry container healthcheck is a permanent no-op
`pkg/registry/manager.go:184` — `Test: []string{"CMD-SHELL", "wget -q -O - http://127.0.0.1:5000/v2/ || exit 0"}`. The `|| exit 0` swallows wget's exit code: the healthcheck **always succeeds**, so a wedged registry is never detected. Also hardcodes `:5000` while `cfg.InternalPort` is configurable (`manager.go:104-107`).

### F14 — Silent wrong-commit builds on checkout failure
`pkg/git/clone.go:160-167` — if the shallow clone lacks `CommitSHA`, the code fetches the SHA and retries checkout, but the **final checkout error is discarded** (`_ = exec...Run()`). The build then proceeds on branch HEAD while the build record, commit status, and image tag all claim the requested SHA — a provenance lie. Fail the clone instead.

### F15 — Widespread error swallowing at state boundaries
- `pkg/registry/manager.go:81,91,317` — config/htpasswd `os.WriteFile` errors discarded; registry runs on stale/absent auth config.
- `pkg/templates/deployer.go:157,176,190,194` — all store writes (`Services().Create`, `EnvVars().Set`, `Volumes().Create`, `Deployments().Create`) discarded; SQLite can disagree with Docker reality with zero signal.
- `pkg/templates/deployer.go:103` — network create failure ignored; surfaces later as an opaque container-create error.
- `pkg/orchestration/compose.go:740` — `existingList, _ :=` swallows `List` failure; a transient socket error turns a rolling update into a name-conflict failure.
- `pkg/build/manager.go:334` — `logFile, _ := os.OpenFile(...)`; log-open failure silently drops all on-disk build logs.

### F16 — Template rollback leaks store metadata
`pkg/templates/deployer.go:522-548` — rollback deletes Service and Volume rows but **not** the EnvVar rows (`ResourceID=appID`, written at `deployer.go:176-186`) nor the Deployment row (`deployer.go:194-201`). Orphaned env vars (including `IsSecret` ones) accumulate in SQLite after every failed template install. Also `ValueEncrypted: v` receives **plaintext** (`deployer.go:181`) — encryption, if any, is delegated to the store layer; verify `store.EnvVars().Set` actually encrypts before persisting.

---

## 4. Minor Findings (P3)

- **F17** `pkg/orchestration/swarm.go:197-209,244-256` — `InspectService`/`ListServices` report `RunningReplicas = DesiredReplicas = spec replicas`; actual task state (`TaskList` aggregation) is never consulted. Dashboard shows "3/3 running" for a crash-looping service.
- **F18** `pkg/orchestration/swarm.go:272-274` — `ScaleService` unconditionally overwrites `svc.Spec.Mode.Replicated`; scaling a **global-mode** service silently converts it to replicated.
- **F19** `pkg/build/nixpacks.go:116-121` — build env (incl. `BuildArgs` secrets) passed as `--env K=V` **argv** — visible to every host process via `/proc/<pid>/cmdline`. Prefer env passthrough or a 0600 `--env-file`.
- **F20** `pkg/build/nixpacks.go:135-141` — `bufio.Scanner` with default 64KiB line cap; `scanner.Err()` never checked → long BuildKit lines silently truncate the log stream.
- **F21** No build timeout: `executeJob` (`manager.go:308-310`) derives a cancel-only context; a hung git/docker op pins one of only 2 workers forever → queue-starvation DoS.
- **F22** `pkg/build/manager.go:339-356` — `logCb` does one unbuffered `WriteString` syscall per build-log line; verbose builds generate 100k+ syscalls. Wrap in `bufio.Writer` with periodic flush.
- **F23** `pkg/orchestration/logs.go:42-45` — `DecodeStream` goroutine leaks if the caller cancels ctx without closing `src` (only `StreamDemux`'s `defer stream.Close()` saves it). Also `bufferPool` (`logs.go:22-30`) is allocated but never used — dead code.
- **F24** `pkg/orchestration/containers.go:420,426,431,441,457` — probation-failure cleanup uses the (possibly cancelled) request ctx; use `context.Background()` like the compose rollback does (`compose.go:548-566`).
- **F25** `pkg/orchestration/compose.go:679-684` — `for ... range netNameMap { break }` picks a nondeterministic network when a stack has multiple; sort keys first.
- **F26** `pkg/git/clone.go:178-183` — `maskSensitive` masks the raw token but not its URL-encoded form (the form actually embedded in `cloneURL` at `clone.go:89-90`); encoded tokens can leak in git stderr.
- **F27** `pkg/git/github.go:250` — owner/repo/sha interpolated into the status URL without `url.PathEscape`; `github.go:196,282` unbounded `io.ReadAll` (low risk against api.github.com, but the `GITHUB_API_URL` env override widens it). New `http.Client` per call (`github.go:274`) forgoes connection reuse.
- **F28** `pkg/deploy/handler.go:186-194` — nudge token lives in the URL path; it will appear in reverse-proxy access logs. The comparison loop (`handler.go:218-226`) is O(n) over all tokens per request — fine at expected scale.
- **F29** `pkg/deploy/rate_limiter.go:38-44` — bucket GC only triggers at >10k buckets and only evicts >10min idle entries; a spoofed-source flood between GCs grows the map. Bounded enough, but a hard cap with LRU eviction would be tighter.



---

## 5. What Is Done Well (keep these patterns)

- **Deploy nudge webhook** (`pkg/deploy/handler.go:175-319`): SHA-256-hashed token storage, constant-time comparison, dual IP+token token-bucket rate limiting, 64KB `MaxBytesReader`, registry allowlist with path-segment-boundary anti-typosquat matching (`handler.go:159-172`), and a documented refusal to trust `X-Forwarded-For` (`handler.go:321-334`). Best-in-codebase boundary parsing.
- **HMAC verification** (`pkg/git/webhook.go:30-59`): length pre-check + `subtle.ConstantTimeCompare`; 5MB body cap (`webhook.go:215`).
- **Streaming builds**: `io.Pipe` tar context (`dockerfile.go:128-212`) with `CloseWithError` propagation, 1MB-capped BuildKit JSON scanner (`dockerfile.go:216-286`), push output streamed through the same parser (`manager.go:472-482`), `stdcopy.StdCopy` demux (`logs.go:42-45`) — Invariant 4 memory-boundedness holds on the hot path.
- **DAG resolution**: `ResolveDeploymentOrder` (`compose.go:24-77`) and template `resolveServiceOrder` (`deployer.go:445-500`) are deterministic (sorted Kahn's), rejecting unknown deps and cycles cleanly.
- **Placement constraints**: strict field allowlist with operator validation (`constraints.go:6-60`), enforced at both create and update (`swarm.go:112-118,135-141`).
- **Rolling update fail-safe**: `DeployWithRollingUpdate` (`containers.go:355-486`) keeps the old container running until the new one passes probation — correct single-service semantics.
- **Repo-URL transport hardening**: scheme allowlist + `GIT_ALLOW_PROTOCOL` + `--` separator + `--upload-pack` injection test (`clone_test.go:161-192`).
- **Registry robot tokens**: CSPRNG + bcrypt at rest, secret omitted from listings (`robot_auth.go`, `manager.go:262-277`).

---

## 6. Invariant Compliance Matrix

| Invariant | Verdict | Evidence |
|---|---|---|
| **1 — Zero Shelling** | ✅ Upheld (1 gray zone) | No `exec.Command("sh"/"bash")` in scope. `git` (`clone.go:121`) and `nixpacks` (`nixpacks.go:123`) are typed `exec.CommandContext` with argv arrays, no shell interpolation — acceptable since no typed SDK exists for git. **Gray zone**: nixpacks internally invokes the `docker` CLI, so the nixpacks build path transitively depends on the docker binary rather than the pure socket; the Dockerfile path uses `ImageBuild` correctly. Registry healthcheck uses `CMD-SHELL` (`manager.go:184`) but that executes *inside* the container via the daemon — compliant. |
| **2 — Unified Runtime** | ✅ Upheld | In-memory worker pool, channel queue, SQLite store; no external deps introduced. |
| **3 — Dynamic Ingress** | N/A (not in scope) | No Caddyfile writes observed in scope. |
| **4 — Pure Streaming** | ⚠️ Partial | Streaming dataflow is correct (§5), but build workspaces default to `os.TempDir()` (`manager.go:117`, `clone.go:76`) and build logs are spooled to disk per-line (`manager.go:332-345`) — the letter of "zero /tmp staging" is breached for the build path. |

---

## 7. Remediation Plan (ranked)

| # | Pri | Action | Location |
|---|---|---|---|
| R1 | **P1** | Fail closed in `HandleGenericGitWebhook`: require service + token match unconditionally; reject when `DeployTokenHash == ""` | `pkg/api/controller.go:2930-2936` |
| R2 | **P1** | Fail closed in `HandleGitHubWebhook` when webhook secret unconfigured | `pkg/api/controller.go:2856-2860` |
| R3 | **P1** | Validate `CommitSHA` (`^[0-9a-f]{4,64}$`, reject leading `-`) and add `--` before SHA args | `pkg/git/clone.go:155,162,165` |
| R4 | **P1** | SSRF guard: resolve host, reject loopback/link-local/RFC1918 (configurable allowlist), drop `http` from allowed protocols | `pkg/git/clone.go:40-57,122` |
| R5 | **P1** | Fix Enqueue/Close race: hold RLock across select-send, or stop closing `queue` | `pkg/build/manager.go:163-168,233-238,270-286` |
| R6 | **P1** | Compose rollback: defer old-container removal until whole stack healthy; restart-only on rollback | `pkg/orchestration/compose.go:543-554,822-824` |
| R7 | **P1** | Propagate swarm deploy errors in `DeployApp`; mark build failed when `DeployApp` errors | `pkg/api/controller.go:1078-1080`; `pkg/build/manager.go:496-510` |
| R8 | **P2** | Replace registry SIGHUP "reload" with recreate-on-htpasswd-change; persist robot accounts in SQLite and rehydrate on boot | `pkg/registry/manager.go:310-323` |
| R9 | **P2** | Add `ImagePull` (streamed, auth-aware) before container/service create in deployer + stack paths | `pkg/templates/deployer.go:283`; `pkg/orchestration/compose.go:773` |
| R10 | **P2** | Contain `ContextDir`/`DockerfilePath` within workspace (`filepath.Clean` + prefix check) | `pkg/build/dockerfile.go:47-57` |
| R11 | **P2** | Fix registry healthcheck (drop `|| exit 0`, honor `InternalPort`) | `pkg/registry/manager.go:184` |
| R12 | **P2** | Return error when final commit checkout fails; never build an unrequested SHA | `pkg/git/clone.go:160-167` |
| R13 | **P2** | Host-key pinning for SSH clones (per-app known_hosts) | `pkg/git/clone.go:139` |
| R14 | **P2** | Stop-first fallback (or clear rejection) for rolling updates with `HostPort` bindings | `pkg/orchestration/containers.go:355+`; `compose.go:666+` |
| R15 | **P2** | Template rollback: delete EnvVar + Deployment rows; stop swallowing store-write errors | `pkg/templates/deployer.go:157-201,522-548` |
| R16 | **P3** | Real replica health via `TaskList` aggregation in `InspectService`; guard global-mode in `ScaleService` | `pkg/orchestration/swarm.go:173-210,263-288` |
| R17 | **P3** | Move build workspaces off `/tmp` (default `/var/lib/pikpik/builds`), `bufio.Writer` for log spool, build timeout, nixpacks scanner.Err check + env-file instead of argv secrets | `pkg/build/manager.go:117,332-345`; `pkg/build/nixpacks.go:116-141` |
| R18 | **P3** | Cleanup: use or delete `LogFrameProcessor.bufferPool`; close-src contract on `DecodeStream`; URL-escape GitHub status path segments; mask URL-encoded token form | `pkg/orchestration/logs.go`; `pkg/git/github.go:250`; `pkg/git/clone.go:178-183` |
