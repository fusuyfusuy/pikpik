# Scope 5 Architectural & Code Quality Audit Report
## API Gateway, PTY/WebSocket & SSE Streaming, Unified Daemon, Standalone CLI

- **Target Subsystem**: Scope 5 — API Gateway & CLI/Daemon
- **Repository**: `github.com/fusuycorp/pikpik`
- **Auditor**: Scope 5 Auditor (API Gateway & CLI/Daemon)
- **Review Date**: August 30, 2026
- **Status**: Audit Completed — Diagnostic Report
- **Supersedes**: The API-related section of `reviews/scope_4_cicd_backup_api.md` (2026-08-30 18:05), which predates the unified PTY architecture, the dual-protocol SSE streaming engine, and the pflag/POSIX CLI rewrite audited here.
- **Overall Health Score**: **4.3 / 10.0 (Critical Band)**

> **Scoring calibration used**: <7.0 Critical (RCE, silent data loss, unauthenticated mutation, crash loops) · 7.0–8.4 Moderate · 8.5–9.4 Minor · 9.5–10 Exemplary. The composite score is dominated by a small cluster of Critical findings (hardcoded master secrets, an authless webhook bypass, unthrottled login) that each independently constitute full-system compromise vectors; per the rubric these are weighted, not averaged away by the otherwise solid route-registration and CLI hygiene.

---

## 1. Scope & Method

Files reviewed directly (`Read`/`Grep`/`Bash`, no whole-file reads over ~450 lines where a targeted grep sufficed, no subagents per instructions):

- `pkg/api/{auth,controller,gateway,pty,ratelimit,routes,sse,types,ws_hub}.go` and all `*_test.go` siblings
- `cmd/pikpik/main.go` (daemon entrypoint)
- `cmd/pikpik-cli/{main,client,config}.go` and `main_test.go`
- `cmd/pikpik-agent/main.go` (cross-reference only, per instructions)
- `specs/06-API-AND-CLI-SPEC.md` for intended design
- Cross-package spot checks: `pkg/git/webhook.go` (`VerifyGitHubSignature`), `pkg/auth/token.go`, `pkg/crypto/aes_vault.go`

Every `Controller` interface method (Auth, Apps, Stacks, Nodes, Databases, Backups, Ingress, Registry, System, Builds/Webhooks, Templates — 60+ methods) was cross-referenced against `RegisterRoutes` in `routes.go`. **Route registration is complete** — no orphaned controller methods and no dangling route registrations were found. That correctness dimension passes cleanly; the failures below are concentrated in authn/authz defaults, secret management, and streaming robustness.

---

## 2. Critical Findings (severity < 7.0)

### 2.1 Hardcoded, publicly-known master encryption key — `cmd/pikpik/main.go:154`
```go
vault, _ := crypto.NewAESVault("pikpik_system_master_secret_32b!")
```
`crypto.NewAESVault` (`pkg/crypto/aes_vault.go:25-36`) derives the AES key via `scrypt.Key(masterSecret, defaultVaultSalt, ...)` — a **fixed salt** combined with this **literal, source-controlled string**. Every unmodified pikpik installation on earth derives the *identical* AES key. `configMgr` uses this vault to encrypt secrets at rest (registry credentials, S3 credentials, other config secrets per `pkg/config`). Anyone with read access to this public repository can decrypt the encrypted-secrets column of **any** pikpik deployment's SQLite DB or backup snapshot. There is no environment-variable override, unlike the admin password/enrollment token below — this is not even configurable. Root cause: a deployment-specific secret was compiled in as a literal instead of sourced from `PIKPIK_MASTER_KEY`/KMS/file-at-rest.
**Remediation**: require a `PIKPIK_MASTER_KEY` env var (fail startup if unset in production mode, ok to default only in an explicit `--dev` mode), and add a migration path for already-encrypted data.

### 2.2 Hardcoded default owner credentials — `cmd/pikpik/main.go:111-112`
```go
fs.StringVarP(&cfg.AdminEmail, "admin-email", "e", getEnvOrDefault("PIKPIK_ADMIN_EMAIL", "admin@pikpik.local"), ...)
fs.StringVarP(&cfg.AdminPassword, "admin-password", "p", getEnvOrDefault("PIKPIK_ADMIN_PASSWORD", "pikpikAdmin123!"), ...)
```
`setupUnifiedServer` unconditionally calls `authSvc.BootstrapAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword)` (line 159) whenever both are non-empty — which they always are by default. Any operator who starts the binary without explicitly overriding `PIKPIK_ADMIN_PASSWORD`/`-p` gets a fully-provisioned **Owner**-role account at `admin@pikpik.local` / `pikpikAdmin123!`, a password visible to anyone who reads this file. Combined with 2.4 (no login rate limiting), this is a walk-up full-cluster takeover.
**Remediation**: generate a random password on first boot and print/write it once (à la Rancher/K3s), or refuse to boot without an explicit admin password/token in non-dev mode.

### 2.3 Hardcoded worker-node enrollment token — `cmd/pikpik/main.go:110`
```go
fs.StringVarP(&cfg.EnrollmentToken, "token", "t", getEnvOrDefault("PIKPIK_ENROLLMENT_TOKEN", "pik_node_enrollment_secret_token"), ...)
```
Same pattern as 2.2: the token that lets a new node join the Swarm cluster as a worker defaults to a static, source-visible string. An attacker with network reach to `/agent/connect` can enroll a hostile node into the cluster using this default, potentially gaining scheduling access and a foothold with Docker socket exposure on the fleet.
**Remediation**: same as 2.2 — random-generate and surface once, or hard-require operator-supplied value.

### 2.4 Login endpoint has zero rate limiting — `pkg/api/routes.go:122-145`
```go
mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) { ... })
```
This is the only mutating/security-sensitive endpoint registered via bare `mux.HandleFunc` instead of the `authWrap(...)` closure that applies `rateLimiter.Allow(...)` (see `routes.go:102-119`). Because login is (correctly) unauthenticated, it never passes through `authWrap`, and no rate limiting is applied anywhere else to it either. `ratelimit.go` even defines a `TieredRateLimiter` with a dedicated `loginLimiter` (5/min) — **it is never constructed or wired anywhere** (`grep -rn NewTieredRateLimiter` outside its own definition returns nothing). Result: unlimited-speed password brute force / credential stuffing against every account, compounded by 2.2's guessable default.
**Remediation**: wrap `/api/v1/auth/login` with the `loginLimiter` tier, keyed by IP + attempted email.

### 2.5 GitHub webhook signature verification is fail-open — `pkg/api/controller.go:1484-1489`
```go
func (c *DefaultController) HandleGitHubWebhook(ctx context.Context, secret string, signature string, payload []byte) (*store.Build, error) {
	if secret != "" && signature != "" {
		if !git.VerifyGitHubSignature(secret, payload, signature) {
			return nil, errors.New("invalid webhook signature")
		}
	}
	...
```
`git.VerifyGitHubSignature` itself is implemented correctly (HMAC-SHA256, hex-decoded, length-checked). The bug is the caller's guard: verification is **skipped entirely**, and the request is treated as authentic, whenever `secret == ""` (env var `GITHUB_WEBHOOK_SECRET` unset — the out-of-the-box state, see routes.go:853) **or** `signature == ""` (attacker simply omits `X-Hub-Signature-256`/`X-Hub-Signature`). The second branch means even a fully-configured deployment with a correct secret is bypassable — an attacker just doesn't send a signature header, and the code takes the "no verification requested" path rather than "verification required and failed". This is a fail-open logic inversion, not a missing feature: it should be "if `secret != ""`: `signature` must be present and valid," full stop. Impact: unauthenticated POST to `/api/v1/webhooks/github` (which is not behind `authWrap` at all, `routes.go:840`) queues an arbitrary `BuildJob` with attacker-controlled `RepoURL`/`CommitSHA`/branch, directly feeding the build/BuildKit pipeline — an unauthenticated remote build-trigger with attacker-supplied source.
**Remediation**: `if secret == "" { return nil, errors.New("webhook not configured") }`; `if !git.VerifyGitHubSignature(secret, payload, signature) { return nil, errors.New("invalid webhook signature") }` unconditionally once a secret exists, and refuse to boot with the GitHub webhook route mounted if no secret is configured.

### 2.6 Generic git webhook token check is fail-open — `pkg/api/controller.go:1554-1566`
```go
if err == nil && svc != nil && svc.DeployTokenHash != "" {
    if token == "" || auth.HashToken(token) != svc.DeployTokenHash {
        return nil, errors.New("unauthorized git webhook token")
    }
}
```
Same shape as 2.5: if the service has no `DeployTokenHash` configured, the token check is skipped silently and **any caller can trigger a build for that `app_id`** via `POST /api/v1/webhooks/git/{app_id}` (also unauthenticated at the route layer, `routes.go:867`). There is no warning logged when this happens. This is a deny-by-default violation identical in shape to 2.5.
**Remediation**: require `DeployTokenHash` to be set before the generic webhook path is enabled for a service; otherwise reject.

---

## 3. Moderate Findings (7.0–8.4)

### 3.1 PTY host-machine role gate fails open — `pkg/api/pty.go:117-122`
```go
case "host_machine", "host", "node":
    role := GetRoleFromContext(r.Context())
    if role != "" && role != RoleAdmin && role != RoleOwner {
        safeConn.WriteExit(1, "forbidden: host machine terminal requires admin or owner role")
        return
    }
```
The guard only *denies* when `role` is a known non-empty value below Admin. If `role == ""` — i.e. this handler is ever reached without having passed through `AuthMiddleware` (which is the only thing that populates `contextKeyRole`) — the condition is `false` and access is **granted**, handing out a live host shell via `/dev/ptmx` with whatever privileges the daemon process holds (typically root, since it manages the Docker socket). Today this is masked because `routes.go:819-821` always wraps `ptyHandler.ServeHTTP` in `authWrap(RoleDeveloper, ...)`, which never lets an empty role through. But the flaw is proven live by the test suite itself: `pkg/api/pty_test.go:17-19` constructs `httptest.NewServer(api.NewPTYHandler(nil))` directly — bypassing `AuthMiddleware` entirely — and `TestPTY_HostMachine_Interactive` successfully obtains an interactive host shell with `target_type=host_machine` and no auth whatsoever, because `role` is empty. This is a deny-by-default inversion in a defense-in-depth check for the single most dangerous capability in the API surface; any future reuse of `PTYHandler` outside the exact current route wiring (an internal tool, a different mux, a refactor) silently grants root host access.
**Remediation**: invert to allow-list form: `if role != RoleAdmin && role != RoleOwner { deny }`. Empty role must deny.

### 3.2 PTY WebSocket connections have no read deadline or frame size limit — `pkg/api/pty.go` (whole file)
`ws_hub.go` sets `conn.SetReadLimit(64*1024)`, a 60s `SetReadDeadline`, and a pong handler that refreshes it (`ws_hub.go:183-188`). `pty.go`'s `ServeHTTP`/`handleContainerPTY`/`handleHostPTY` set **none of these**. Consequences:
- An idle or malicious client that opens `/ws/pty` and never sends/closes holds the blocking `sConn.conn.ReadMessage()` loop open indefinitely — there is no timeout to reclaim it. Meanwhile the Docker `exec` session (`attachResp`, only `Close()`d when the function returns) or the spawned host `exec.Cmd`/PTY (`session.Close()`, same) stays alive. Repeated connections without cleanup exhaust file descriptors, Docker exec sessions, and (for `host_machine`) actual host processes.
- With no `SetReadLimit`, a client can send an arbitrarily large single WebSocket frame as "stdin," which gorilla/websocket will buffer in full before `ReadMessage` returns — unbounded per-connection memory.
- Note the two output goroutines (`handleContainerPTY:165-182`, `handleHostPTY:385-410`) do call `cancel()` on remote EOF, but cancelling the request context does not unblock a synchronous `conn.ReadMessage()` call that is waiting on the network socket, so the stdin loop (and the resources it holds) do not actually terminate until the client itself disconnects.
**Remediation**: mirror `ws_hub.go`'s `SetReadLimit`/`SetReadDeadline`/ping-pong pattern in `PTYHandler.ServeHTTP`, and have the output-side `cancel()` also force-close `conn` so the blocked stdin read unblocks immediately on remote-process exit.

### 3.3 SSE streams have no Last-Event-ID replay / backlog — `pkg/api/sse.go`
`FormatSSE` writes an `id: <unixnano>` field (sse.go:113-124) and `ServeStream` registers a brand-new, empty-history `SSEClient` on every connect (sse.go:181-199) — there is no per-channel ring buffer and the `Last-Event-ID` request header is never read anywhere in the package (confirmed via grep — zero matches for `Last-Event-ID`/`LastEventID` in `pkg/api` or the spec). A client's EventSource reconnecting after a network blip (the exact scenario `id:` exists to support) silently loses every event emitted during the gap; the server has no way to know or care what the client already saw. This directly fails the "SSE reconnection / Last-Event-ID handling" design goal. Compounding this, `ServeLogsStream`'s `follow=false` branch (sse.go:268-280) is a stub whose comment claims it will "return empty or tail buffer then close" but it only ever returns empty — there is no tail buffer anywhere in the type.
**Remediation**: add a bounded per-channel ring buffer (event id → frame) in `SSEBroadcaster`, and on connect, if `Last-Event-ID` is present, replay buffered frames newer than that id before switching to live tailing.

### 3.4 Unauthenticated/unlimited request bodies on webhook + all JSON routes
- `POST /api/v1/webhooks/git/{app_id}` (routes.go:867) reads the body via `git.ParseGenericGitPush(r)` with no `http.MaxBytesReader`/`io.LimitReader` applied in this package, and this route sits outside `authWrap` (no auth, no rate limit) entirely.
- Every other JSON-body mutating route (`CreateApp`, `UpdateApp`, `SetAppEnv`, `CreateStack`, `CreateDatabase`, `SetAppTraffic`, `BindDomain`, `UploadCertificate`, `CreateBackupDestination`, ~20 call sites) does `json.NewDecoder(r.Body).Decode(&req)` with **no size cap**. Only the GitHub webhook (`io.LimitReader(...,5*1024*1024)`, routes.go:847) and the nudge-mock stub (`io.LimitReader(...,64*1024)`, routes.go:396) bound their input. `http.Server` in `cmd/pikpik/main.go:305-310` sets `ReadHeaderTimeout`/`IdleTimeout` but no body-size ceiling either.
**Remediation**: standardize on `http.MaxBytesReader(w, r.Body, N)` (pattern already correctly used in `pkg/deploy/handler.go` per the prior scope-4 audit) applied uniformly in `authWrap` or at the mux level, and add it to the two unauthenticated webhook routes specifically since they are the least trusted input.

### 3.5 `EnableCors` / `AllowedOrigins` config is dead; CORS is unconditionally wildcarded — `pkg/api/gateway.go:26-27, 81-92`
`APIGatewayOptions.EnableCors` and `.AllowedOrigins` are declared and accepted by `NewAPIGatewayWithOptions` but **never read** anywhere in the file (confirmed by grep — zero other references). The actual CORS headers are hardcoded unconditionally:
```go
w.Header().Set("Access-Control-Allow-Origin", "*")
```
regardless of what the caller configures. This is misleading — an operator setting `AllowedOrigins` believes they are restricting cross-origin access but nothing changes. Practical exploitability is softened by `SameSite=Lax` on `pikpik_session` (routes.go:142) blocking cross-site cookie use on state-changing requests, but any client holding a Bearer token (mobile app, CLI-exported token, browser extension) is fully exposed to any origin.
**Remediation**: implement the origin allow-list check using the already-declared struct fields, and stop wildcarding when `AllowedOrigins` is non-empty.

### 3.6 `WebSocketHub.broadcastMessage` holds a single `RWMutex` for the full O(n) fanout — `pkg/api/ws_hub.go:91-114`
The entire client iteration (per-client `channels` lookup, `select`/non-blocking send) happens under one `h.mu.RLock()` held for the whole loop. `register`/`unregister` require the write lock, so under sustained broadcast traffic with many connected clients, new WS connections/disconnections queue behind in-flight fanouts. Not incorrect, and the non-blocking `select default: drop` on `client.send` correctly prevents a slow consumer from stalling the broadcaster itself (good backpressure design) — but the lock-held-during-fanout shape is exactly the O(n)-under-lock pattern the review dimension flags, and it will show up as connect/disconnect latency spikes at scale, not as a correctness bug today.
**Remediation**: snapshot `h.clients` into a slice under a brief lock, then iterate/send outside the lock.

### 3.7 `RateLimiter` records are never evicted; IP key is spoofable — `pkg/api/ratelimit.go` + `pkg/api/auth.go:101-117`
`RateLimiter.records` (a `map[string]*rateRecord`) only ever grows — pruning removes stale *timestamps* from a record but no code path ever deletes a key from the map itself, and there is no background sweep. The map is keyed by `ExtractToken(r)` or, for unauthenticated requests, `ExtractClientIP(r)`, which trusts the **first** `X-Forwarded-For` entry unconditionally with no trusted-proxy allow-list (`auth.go:103-108`). An attacker can send a distinct spoofed `X-Forwarded-For` value on every request, both (a) trivially bypassing the rate limit itself (fresh key ⇒ fresh empty sliding window every time) and (b) growing `records` unboundedly — a memory-exhaustion DoS with zero authentication required, reachable on any route that passes through `authWrap` before token validation fails (the rate-limit check runs before `h(w, r)` but *inside* `authWrap`, using the pre-auth `ExtractToken`/`ExtractClientIP` value regardless of whether auth later succeeds).
**Remediation**: only trust `X-Forwarded-For` when the immediate peer is a configured trusted proxy (else use `RemoteAddr`); add a periodic sweep (or an LRU/TTL cache) to evict empty/expired `rateRecord`s from the map.

---

## 4. Minor Findings (8.5–9.4)

### 4.1 Passkey login is a non-functional mock wired into production routes — `pkg/api/routes.go:169-181`
```go
mux.HandleFunc("POST /api/v1/auth/passkey/login/finish", func(w http.ResponseWriter, r *http.Request) {
    WriteJSON(w, http.StatusOK, map[string]string{
        "token":   "pik_live_passkey_auth_session",
        "message": "passkey verified",
    }, GenerateRequestID())
})
```
This performs zero credential verification and returns a literal static string as a "token." It is **not** currently an auth bypass — `pik_live_passkey_auth_session` has the `pik_live_` prefix (`auth.DefaultTokenPrefix`), so `AuthMiddleware` (auth.go:159-183) routes it through `authSvc.ValidateAPIToken`, which will reject it since it was never actually issued/stored. But it is dead-end, misleading production API surface (a real client integrating against this endpoint gets a token that will always 401 downstream), and it should not be reachable without a feature flag if the feature is unimplemented.
**Remediation**: return `501 Not Implemented` until WebAuthn is actually wired, or gate behind a build/feature flag.

### 4.2 `getPathParam` fallback chain is fragile — `pkg/api/routes.go:55-87`
Cross-aliasing `app_id`↔`id`↔`build_id` and falling back to manual path-token splitting when Go 1.22 `r.PathValue` fails suggests route-pattern/param-name drift rather than a deliberate design. It works today (verified all routes wire correctly) but is a maintenance trap: adding a new route with a differently-named wildcard silently falls through to the generic "last path segment" branch instead of failing loudly.
**Remediation**: standardize wildcard names across all route patterns (e.g. always `{id}`) and delete the aliasing/fallback branches; let a missing param be an explicit 400 rather than a guess.

### 4.3 CLI builds WebSocket query strings via unescaped `fmt.Sprintf` — `cmd/pikpik-cli/main.go:358, 556`
```go
wsURL := client.GetWebSocketURL(fmt.Sprintf("/ws/logs?target_id=%s&tail=%d", appID, *tail))
ptyURL := client.GetWebSocketURL(fmt.Sprintf("/ws/pty?container_id=%s&cmd=%s", containerID, cmdStr))
```
Neither `appID`/`containerID` (user-supplied positional args) nor `cmdStr` (joined from `exec` arguments) is URL-escaped. A value containing `&` injects additional query parameters (e.g. a container name of `foo&target_type=host_machine` would retarget the PTY session to the host-machine handler client-side). Low severity since the operator controls their own CLI input, but a real correctness bug for scripted/templated invocations.
**Remediation**: build the URL with `net/url.Values{}.Encode()` instead of string formatting.

### 4.4 No panic-recovery middleware with a structured error envelope — `pkg/api/gateway.go`
The outer `handler` in `NewAPIGatewayWithOptions` (gateway.go:81-92) does not wrap the mux in a `recover()` middleware. `net/http`'s per-connection `Serve` goroutine recovers panics itself (so the process won't crash), but a panicking handler returns a bare connection reset instead of the package's own `WriteError` RFC-7807 envelope, breaking API consistency and losing the request-ID/structured logging the rest of the surface relies on.
**Remediation**: add a small recovery middleware in the same wrapper that already sets CORS headers, converting panics into `WriteError(w, 500, ErrCodeInternalError, ...)`.

---

## 5. What's Solid (no action needed)

- **Route/controller parity**: exhaustive cross-check of all 60+ `Controller` interface methods against `RegisterRoutes` found no missing or orphaned wiring.
- **PTY resize/signal opcode protocol** (`0x00` stdin, `0x01` resize, `0x02` SIGINT, `0xFF` exit) is implemented consistently and correctly across container, swarm-task, and host targets, including the `safeWSConn` mutex that correctly serializes concurrent writer-goroutine + reader-loop writes to the same `gorilla/websocket.Conn` (a real, easy-to-get-wrong concurrency hazard that is handled correctly here).
- **WS Hub / SSE Broadcaster backpressure**: both use non-blocking `select { case ch <- frame: default: drop }` sends to slow consumers — no unbounded buffering, no deadlock risk from a stalled client. This is the correct pattern and both dimensions asked about it explicitly.
- **CLI config storage**: `~/.pikpik/config.json` uses `os.MkdirAll(dir, 0700)`, atomic temp-file-then-`os.Rename` writes at `0600`, and reasonable env-var overrides (`PIKPIK_CONTEXT`/`PIKPIK_SERVER_URL`/`PIKPIK_TOKEN`) — solid secret-at-rest hygiene for the CLI side.
- **pflag/POSIX CLI migration**: `cmd/pikpik/main.go` and every `cmd/pikpik-cli` subcommand consistently use `github.com/spf13/pflag` with short+long forms (`-l/--listen`, `-a/--app`, etc.) — the recent POSIX/GNU flag upgrade is applied uniformly, no leftover stdlib `flag` mixed in.
- **Generic git webhook and GitHub webhook signature primitives** (`git.VerifyGitHubSignature`, `auth.HashToken`) are implemented correctly in isolation (proper HMAC-SHA256, length-checked comparison) — the vulnerabilities in §2.5/2.6 are entirely in the *caller's* fail-open guard logic, not in the crypto primitives themselves.

---

## 6. Prioritized Remediation Order

1. Fix 2.5/2.6 fail-open webhook guards (small, surgical diff, closes an unauthenticated build-trigger).
2. Remove hardcoded defaults 2.1/2.2/2.3 — require explicit operator-supplied secrets or generate-and-print-once on first boot.
3. Wire `TieredRateLimiter.loginLimiter` onto `/api/v1/auth/login` (2.4) — the struct already exists, it just needs to be constructed and applied.
4. Invert the PTY host-machine role check to deny-by-default (3.1) — one-line fix, closes a proven-live bypass path exercised by the test suite itself.
5. Add `SetReadDeadline`/`SetReadLimit` to `pty.go` (3.2) to close the fd/goroutine exhaustion path.
6. Backlog: SSE Last-Event-ID replay buffer (3.3), rate-limiter key trust + eviction (3.7), CORS allow-list wiring (3.5), uniform `MaxBytesReader` (3.4).

---

*Cross-reference note*: `cmd/pikpik-agent/main.go` was read only to confirm it does not duplicate the `pkg/api` route/auth logic reviewed here (it does not — it implements a separate agent-enrollment protocol). A deep audit of that entrypoint is out of this scope's ownership.
