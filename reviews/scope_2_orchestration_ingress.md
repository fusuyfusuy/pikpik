# Scope 2 Architectural & Code Quality Audit: Orchestration & Ingress

**Review Subject**: Scope 2 — Orchestration (`pkg/orchestration`), Ingress (`pkg/ingress`), Deploy Webhook (`pkg/deploy`)
**Auditor**: Orchestration & Ingress Boundary Auditor
**Date**: 2026-08-30 (supersedes the 18:05 revision — predates the canary traffic-splitting feature and 4 subsequent feature commits, including `pkg/deploy` and the Git/webhook integration)
**Overall Health Score**: **6.3 / 10.0** (Category: **Critical**)

> The prior revision scored this boundary 9.4/Minor. That score no longer holds: two findings below are unauthenticated/silent-failure defects that cross into Critical territory (rubric: unauthenticated resource exhaustion, silent destruction of live traffic state), and several previously-flagged Minor items (dead placement-constraint validator, `engine.labels` mismatch) turn out to be worse than previously characterized — the constraint engine isn't just imprecise on one label namespace, it is **never called** from the production service-creation path at all.

---

## 1. Executive Scorecard

| Audit Dimension | Score (0–10) | Status | Key Observations |
| :--- | :---: | :---: | :--- |
| **Invariant 1 — Zero Bash Shelling** | **10.0** | Exemplary | Confirmed zero `os/exec`/`sh -c` invocations anywhere in `pkg/orchestration`, `pkg/ingress`, `pkg/deploy`. 100% typed Docker SDK + typed Caddy REST client (`encoding/json` + `net/http`, no string-templated JSON). |
| **Correctness** | **6.5** | Moderate/Critical mix | Canary weight math is correct; blue-green state machine has a loopback-fallback defect; placement-constraint parser is fully correct but **dead code** in the real deploy path; Compose parser silently drops/mangles several valid inputs. |
| **Robustness (partial-failure handling)** | **5.5** | Critical | Docker Engine client sets a 60s wall-clock `http.Client.Timeout` shared with **streaming** log calls — kills live log follow after 60s. Stack-deploy rollback leaks volumes and swallows network/volume provisioning errors. Ingress full reconciliation silently clobbers live canary splits. |
| **Performance** | **8.0** | Minor | Bounded polling loops (250ms/200ms tickers) are fine given short windows; dead `sync.Pool` in `LogFrameProcessor`; O(n) per-request token bucket cleanup and O(n) token lookup are non-issues at expected scale. |
| **Security** | **5.0** | Critical | Deploy-nudge webhook rate limiter is trivially bypassed via spoofed `X-Forwarded-For`, compounding into unauthenticated memory-exhaustion DoS. CI image allowlist uses boundary-unsafe prefix matching (typosquat bypass). On-Demand TLS `ask` allowlist and Caddy JSON-typed route building are both sound. |
| **Consolidated Weighted Score** | **6.3** | **Critical** | Zero-shelling invariant and the Caddy typed-client design are exemplary; the boundary is let down by unwired safety features, silently-swallowed errors on failure paths, and an unauthenticated DoS vector on the public webhook surface. |

---

## 2. Invariant Compliance

### 2.1 Invariant 1 — Zero Bash Shelling: **PASS**
`grep -rn "os/exec\|exec.Command" pkg/orchestration pkg/ingress pkg/deploy` returns no matches. All container/service/stack/volume/network lifecycle calls go through `github.com/docker/docker/client` (`m.cli.ContainerCreate`, `s.cli.ServiceUpdate`, `m.cli.NetworkCreate`, `m.cli.VolumeCreate`, etc.). Caddy mutations go through typed `net/http` + `encoding/json.Marshal` on strongly-typed `CaddyRoute`/`CaddyConfig` structs (`pkg/ingress/client.go`), never raw string-templated JSON — this also forecloses the "Caddy config injection via unsanitized service names/hosts" risk named in the brief: `Host` values ride inside a marshaled struct field, not inside a hand-built JSON string.

### 2.2 Invariant 2 — Deterministic Context Cancellation: **PARTIAL FAIL**
The spec requires "every engine interaction takes a mandatory `context.Context` parameter with bounded deadlines... network hangs... fail fast without leaking goroutines." Two violations:
- `pkg/orchestration/engine.go:28-34` installs a single `http.Client{Timeout: 60 * time.Second}` on the Docker SDK client, and that same client backs `DockerLogStreamer` (`logs.go:65-70`). Go's `http.Client.Timeout` bounds the *entire* request including body streaming — see Finding 1.
- Rollback/teardown paths (`compose.go:408-409,413`; mirrored in `blue_green.go` and `containers.go` teardown calls) use `context.Background()` instead of a bounded `context.WithTimeout`, so a stalled Docker daemon during a rollback can hang the calling goroutine indefinitely — the opposite of "fail fast."

### 2.3 Placement Constraint Engine (spec §4): **UNWIRED**
`ParseConstraint`, `ParseConstraints`, and `ValidateConstraintsAgainstNodes` in `pkg/orchestration/constraints.go` implement the documented grammar correctly and are well unit-tested (`constraints_test.go`), but `grep -rn "ParseConstraint\|ValidateConstraintsAgainstNodes" --include='*.go'` outside test files shows **zero call sites**. `DockerSwarmManager.convertSpecToSwarm` (`swarm.go:490-496`) passes `spec.Constraints` (raw, unvalidated strings) straight into `swarm.Placement{Constraints: ...}`. The entire "Validator against Cluster Node State" stage from the spec's mermaid diagram (§4) does not exist in the runtime path — see Finding 4.

---

## 3. Findings

### Finding 1 (Critical): Docker SDK client's 60s `http.Client.Timeout` silently kills live log/service streaming
- **Location**: `pkg/orchestration/engine.go:28-34`, consumed by `pkg/orchestration/logs.go:65-70,97-110`
```go
cli, err := client.NewClientWithOpts(
    client.WithHost(socketPath),
    client.WithHTTPClient(&http.Client{
        Timeout: 60 * time.Second,
    }),
    client.WithAPIVersionNegotiation(),
)
```
- **Mechanism**: `DockerLogStreamer` reuses this same `client.CommonAPIClient` for `ContainerLogs`/`ServiceLogs` with `Follow: true`. Go's `http.Client.Timeout` is a wall-clock deadline on the *whole* round trip, including reading the (potentially infinite) response body of a streaming request — it is not an idle timeout. Any `Follow`-mode log stream, `docker events`-equivalent stream, or long `docker wait`, is forcibly aborted at exactly 60 seconds regardless of whether data is actively flowing, independent of the caller's own `context.Context` deadline.
- **Impact**: This breaks live log tailing — one of the platform's flagship features per the commit history ("Dual Protocol SSE streaming engine", "unified PTY architecture") — for every stream older than 60 seconds, and does so silently: the caller sees a transport error/EOF that looks like a normal disconnect, not a configuration bug. No test exercises this because all orchestration tests run against mocked `client.CommonAPIClient` implementations (`mock_test.go`), never the real `NewDockerEngineClient` construction path.
- **Remediation**: Do not set `http.Client.Timeout` on the shared Docker SDK client; rely exclusively on the per-call `context.Context` deadline (already correctly threaded through every interface method per Invariant 2). If a connect/handshake timeout is wanted, set it on the `http.Transport.DialContext`/`ResponseHeaderTimeout`, not the client-wide `Timeout`.

---

### Finding 2 (Critical): Deploy-nudge webhook rate limiting is bypassed by spoofed `X-Forwarded-For`, enabling unauthenticated memory-exhaustion DoS
- **Location**: `pkg/deploy/handler.go:307-322` (`extractClientIP`), combined with `pkg/deploy/rate_limiter.go:31-91` (`TokenBucketLimiter.Allow`) and its call site at `handler.go:190-198`
```go
func extractClientIP(r *http.Request) string {
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        parts := strings.Split(xff, ",")
        if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
            return strings.TrimSpace(parts[0])
        }
    }
    if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
        return strings.TrimSpace(xrip)
    }
    ...
}
```
- **Mechanism**: `ServeHTTP` calls `h.limiter.Allow("ip:"+clientIP, ...)` **before** any token is validated (step 2, before step 3's token lookup). `extractClientIP` trusts `X-Forwarded-For`/`X-Real-IP` unconditionally with no notion of a trusted reverse-proxy hop count or allowlist. Since `POST /api/deploy/nudge/{token}` is a public CI webhook endpoint, any unauthenticated caller can set a distinct spoofed `X-Forwarded-For` value on every request, giving each request its own fresh token-bucket key (`"ip:1.2.3.4"`, `"ip:1.2.3.5"`, ...) and thus unlimited effective request volume against the *only* throttle protecting invalid-token guesses (the per-token limiter in step 4 only applies once a token has already matched).
- **Compounding issue**: `TokenBucketLimiter.Allow` (`rate_limiter.go:38-44`) only attempts cleanup when `len(l.buckets) > 10000`, and even then only evicts buckets idle for `> 10 * time.Minute`. An attacker sending a new spoofed IP on every request keeps every bucket "fresh" (never idle), so the map grows without bound — each `*tokenBucket` is small, but at attacker-controlled request rates this is a straightforward unauthenticated memory-exhaustion vector against the whole `pikpik` process (a crash/OOM loop, matching the rubric's Critical bucket).
- **Impact**: Both the intended per-IP throttle and the intended memory bound of the limiter are defeated by a header any client fully controls, on an endpoint requiring no authentication to reach.
- **Remediation**: Only trust `X-Forwarded-For`/`X-Real-IP` when the immediate peer (`r.RemoteAddr`) is a configured trusted proxy (e.g., the local Caddy instance); otherwise use `r.RemoteAddr` directly. Additionally cap `TokenBucketLimiter`'s total bucket count with a hard ceiling (evict oldest/least-recently-used on overflow, not just size-and-idle gated) so an attacker cannot force unbounded growth even from genuinely distinct source IPs.

---

### Finding 3 (Moderate): `ReconcileFromStore`/`ReconcileAll` silently destroys live canary/blue-green traffic splits
- **Location**: `pkg/ingress/manager.go:117-131` (`ReconcileAll`), `manager.go:158-207` (`ReconcileFromStore`), consumed from `pkg/api/controller.go:1262-1267` (`ReconcileIngress`)
- **Mechanism**: `ReconcileAll` performs a full atomic swap via `POST /load` (`BuildCaddyConfig(routes, tlsCfg)`), where `routes` is built purely from the `services` table (`ReconcileFromStore`, `manager.go:163-199`) — one plain single-upstream route per registered domain. It has no awareness of the separate `route_split_<slug>` routes created by `HTTPCaddyClient.SetTrafficSplit` (`traffic_split.go:144-173`) for an in-flight canary or blue-green rollout. If an operator (or any future automation wired to `ReconcileIngress`) triggers a reconcile while a canary is live, the weighted-upstream split route is silently replaced by a plain 100%-to-"stable" route built from `slug:containerPort` — traffic instantly and silently reverts, with no error surfaced anywhere.
- **Compounding cache-staleness issue**: `HTTPCaddyClient.GetTrafficSplit` (`traffic_split.go:223-260`) checks its own in-memory `c.splits` map first (line 230) and returns that cached value without ever verifying it against live Caddy state. There is no `RemoveTrafficSplit`/cache-invalidation path at all — `grep -n "delete(c.splits"` finds nothing — so once any `ReconcileAll`/`DeleteRoute` call removes the actual route out from under the cache, `GetTrafficSplit` keeps reporting the stale split as active indefinitely.
- **Impact**: A cross-cutting reconciliation feature and a headline canary-deployment feature are not integrated; whichever runs last silently wins, with the loser's state invisible to callers.
- **Remediation**: Either (a) have `ReconcileFromStore` also enumerate and re-emit any active `c.splits` entries into the generated `CaddyConfig`, or (b) track split routes as first-class `RouteSpec`s inside `DefaultIngressManager.routes` so a single reconciliation path covers both. Invalidate/refresh `c.splits` whenever the underlying route is deleted or a reconcile runs.

---

### Finding 4 (Moderate): Placement-constraint parser/validator is fully implemented, well-tested, and never invoked
- **Location**: `pkg/orchestration/constraints.go` (whole file) vs. call sites — none outside `constraints_test.go`; production path is `pkg/orchestration/swarm.go:376-496` (`convertSpecToSwarm`), specifically lines 490-496:
```go
var placement *swarm.Placement
if len(spec.Constraints) > 0 {
    placement = &swarm.Placement{
        Constraints: spec.Constraints,   // raw strings, never parsed/validated
    }
}
```
- **Mechanism**: `CreateService`/`UpdateService` never call `ParseConstraints` to populate `ServiceSpec.ParsedConstraints`, and nothing calls `ValidateConstraintsAgainstNodes` before committing a service to the cluster. The spec's documented pre-flight checks — `ErrInvalidConstraintSyntax`, `ErrUnsupportedConstraintOp`, `ErrNoMatchingNodeAvailable`, `ErrResourceCapacityExceeded` — are all defined (`errors.go:14-27`) but structurally unreachable from any deploy path in `pkg/orchestration` or `pkg/api`.
- **Impact**: A caller can submit a syntactically invalid or logically unsatisfiable constraint (e.g., referencing a node label that exists on zero nodes) and the only feedback is whatever generic error Docker Engine itself returns for the raw constraint string, instead of pikpik's own typed, actionable errors. There is no early rejection before a service spec is committed.
- **Remediation**: In `CreateService`/`UpdateService`, call `ParseConstraints(spec.Constraints)` and (when node data is available/cheap) `ValidateConstraintsAgainstNodes` before calling `s.cli.ServiceCreate`/`ServiceUpdate`, surfacing pikpik's typed errors to the API layer.

---

### Finding 5 (Moderate): `engine.labels.<key>` constraints match against the wrong label namespace
- **Location**: `pkg/orchestration/constraints.go:99-104` (`MatchesNode`) vs. `pkg/orchestration/swarm.go:328-340` (`ListNodes`)
- **Mechanism**: `NodeStatus.Labels` is populated exclusively from `n.Spec.Annotations.Labels` (Swarm *node* labels). `MatchesNode` resolves both `node.labels.<key>` **and** `engine.labels.<key>` against this same field:
```go
case strings.HasPrefix(c.Field, "engine.labels."):
    key := strings.TrimPrefix(c.Field, "engine.labels.")
    if node.Labels != nil {
        actualValue, exists = node.Labels[key]   // wrong source
    }
```
Real Docker Engine daemon labels (configured in `/etc/docker/daemon.json`, exposed as `n.Description.Engine.Labels`) are never collected into `NodeStatus` at all.
- **Impact**: `engine.labels.*` constraints silently evaluate against node annotation labels instead of daemon labels; a constraint that should fail to match (because the daemon label isn't mirrored as a node label) may spuriously match or vice versa, with no error — a silent placement-correctness bug. (Currently moot in production since Finding 4 means this validator isn't even called before deploy, but it will resurface the moment Finding 4 is fixed.)
- **Remediation**: Add `EngineLabels map[string]string` to `NodeStatus`, populate it from `n.Description.Engine.Labels` in `ListNodes`, and branch `MatchesNode` on it for the `engine.labels.` prefix.

---

### Finding 6 (Moderate): Compose stack rollback leaks Docker volumes; network/volume provisioning errors are silently swallowed
- **Location**: `pkg/orchestration/compose.go:361-395` (network/volume reconcile), `compose.go:404-417` (rollback closure)
```go
_, err := m.cli.NetworkCreate(ctx, fullNetName, types.NetworkCreate{...})
if err == nil {
    createdNets = append(createdNets, fullNetName)
}   // no else — error silently discarded, deployment proceeds regardless
...
_, err := m.cli.VolumeCreate(ctx, volume.CreateOptions{...})
if err == nil {
    createdVols = append(createdVols, fullVolName)
}   // same pattern

rollback := func(deployErr error) (*StackDeploymentResult, error) {
    for _, cid := range deployedContainers { ... }
    for _, nid := range createdNets {
        _ = m.cli.NetworkRemove(context.Background(), nid)
    }
    // createdVols is never referenced here — volumes are never cleaned up
    result.Errors = append(result.Errors, deployErr.Error())
    return result, deployErr
}
```
- **Mechanism**: Two independent defects: (1) `NetworkCreate`/`VolumeCreate` errors are never checked — if creation fails, deployment simply continues, attaching subsequent containers to networks/volumes that don't exist, turning a clear provisioning error into a confusing "failed to create service container" error several steps later; (2) the rollback closure — which *does* correctly clean up containers and networks (this was fixed since the previous audit) — never tears down `createdVols`, so every stack deployment that fails after volume provisioning but before completion leaks one or more named Docker volumes permanently.
- **Impact**: Silent resource leak (orphaned volumes accumulate disk usage over repeated failed deploys) plus a fail-fast violation (Ponytail: "crash/raise immediately on unexpected internal invariant violations; never swallow errors").
- **Remediation**: Check and propagate `NetworkCreate`/`VolumeCreate` errors (abort the deployment via the same `rollback` path used for container failures), and add a `for _, vol := range createdVols { _ = m.cli.VolumeRemove(context.Background(), vol, true) }` loop to `rollback`.

---

### Finding 7 (Moderate): CI image-registry allowlist uses boundary-unsafe prefix matching (typosquat bypass)
- **Location**: `pkg/deploy/handler.go:139-155` (`ValidatePayload`), specifically line 147:
```go
if strings.HasPrefix(image, allowed) || strings.HasPrefix(image, strings.TrimSuffix(allowed, "/*")) {
    matched = true
    break
}
```
- **Mechanism**: `HasPrefix` has no notion of a path-segment boundary. If an operator configures `AllowedHosts: []string{"ghcr.io/fusuycorp"}` (a natural way to write "our org," without a trailing slash), an attacker-controlled image reference `ghcr.io/fusuycorpevil/backdoor:latest` also satisfies `HasPrefix`, since it merely needs to start with the same characters — there's no requirement that the next character be `/` or end-of-string.
- **Impact**: The webhook's core security control — "only deploy images from registries we trust" — can be silently bypassed by registering a sibling namespace/typosquat repository, letting an attacker with a valid (possibly leaked or brute-forced) nudge token deploy an arbitrary image.
- **Remediation**: Normalize `allowed` to always end in `/` before comparison (or explicitly check `image == allowed || strings.HasPrefix(image, allowed+"/")`), and apply the same boundary-safe check to the `/*`-suffixed wildcard variant.

---

### Finding 8 (Moderate): Blue-green deploy falls back to probing/routing `127.0.0.1` when the new container has no IP yet
- **Location**: `pkg/orchestration/blue_green.go:208-220,284-285`
```go
greenIP := greenStatus.IPAddress
if greenIP == "" {
    greenIP = "127.0.0.1"
}
probeURL := fmt.Sprintf("http://%s:%d%s", greenIP, cfg.ContainerPort, cfg.HealthCheckPath)
...
greenDial := fmt.Sprintf("%s:%d", greenIP, cfg.ContainerPort)   // later used as the Caddy upstream
```
- **Mechanism**: If `ContainerStatus.IPAddress` comes back empty right after `Start()` (e.g., host-networking mode, a network attachment race, or a transient `NetworkSettings` gap), the code substitutes the orchestrator host's own loopback address for *both* the health probe target and — on success — the live Caddy `reverse_proxy` upstream dial address.
- **Impact**: A "successful" health check in this branch is checking whatever happens to be listening on `127.0.0.1:<ContainerPort>` on the **host**, not the new container — it can spuriously pass (if something else is bound to that port on the host) or spuriously fail. If it passes, production traffic gets cut over to `127.0.0.1:<port>` on the ingress/orchestrator host itself instead of the actual green container, which is both a correctness failure and a potential host-service exposure.
- **Remediation**: Treat an empty `IPAddress` post-start as a hard failure (`return nil, fmt.Errorf("green container has no assigned network address")`) rather than silently substituting loopback; retry the `Inspect` call briefly if this is a known race rather than proceeding with a wrong address.

---

### Finding 9 (Moderate): Canary step failures during blue-green rollout are silently discarded
- **Location**: `pkg/orchestration/blue_green.go:289-304`, specifically line 298:
```go
for _, pct := range cfg.CanarySteps {
    if pct > 0 && pct < 100 {
        stepCfg := ingress.TrafficSplitConfig{ ... }
        _ = d.ingress.SetTrafficSplit(ctx, cfg.Domain, stepCfg)   // error discarded
        if cfg.StepDelay > 0 {
            time.Sleep(cfg.StepDelay)
        }
    }
}
// Cutover 100% traffic to Green — runs unconditionally, regardless of step outcomes above
```
- **Mechanism**: Every intermediate canary weight shift ignores `SetTrafficSplit`'s returned error. If Caddy is briefly unreachable during, say, the 10%-canary step, the loop proceeds straight through remaining steps and then unconditionally performs the full 100% cutover as though every gradual step succeeded — defeating the entire purpose of a staged canary (catching a bad deploy before it gets full traffic).
- **Impact**: Robustness gap in the one place the review brief specifically asks about ("partial-failure handling in multi-container deploys / rollback on partial failure") — this is exactly such a partial failure, and it is swallowed rather than escalated.
- **Remediation**: Check the error from each `SetTrafficSplit` call in the canary loop; on failure, abort the rollout (remove the green container, leave blue's 100% traffic in place) instead of proceeding to full cutover.

---

### Finding 10 (Minor): Compose parser silently drops IP-bound port syntax and never parses `deploy:` resource limits
- **Location**: `pkg/orchestration/compose.go:241-266` (port parsing)
- **Mechanism**: The port-mapping parser only handles 1-part (`"80"`) and 2-part (`"8080:80"`) colon-split forms (`compose.go:250,258`). Standard Compose IP-bound syntax (`"127.0.0.1:8080:80"`, 3 colon-separated segments) falls through both branches and is silently dropped — no error, no log, the binding simply never reaches `ContainerSpec.ExposedPorts`. Separately, `rawComposeService`/`ComposeServiceDef` has no `deploy:` field at all, so `deploy.resources.limits.{cpus,memory}` declared in a v2/v3 Compose file are never parsed — every compose-deployed container silently gets zero resource limits regardless of what the file specifies.
- **Impact**: Malformed/unsupported input is tolerated without diagnostics (contrary to "malformed compose file tolerance" expectations — tolerance should mean *graceful rejection with a clear error*, not silent partial application), and a commonly-used Compose feature (resource limits) is a no-op.
- **Remediation**: Return a parse error (or at minimum a warning surfaced in `StackDeploymentResult`) for unrecognized port-mapping shapes; add `deploy.resources` parsing into `ComposeServiceDef.Resources`.

---

### Finding 11 (Minor): `$$`-escaped literal dollar signs are not honored during compose interpolation
- **Location**: `pkg/orchestration/compose.go:20,79-98`
- **Mechanism**: `envVarRegex` matches `${VAR}`, `${VAR:-default}`, and bare `$VAR`, but has no case for Compose's standard `$$` literal-dollar escape. A compose file value containing a literal `$` (e.g., a bcrypt hash `$2b$12$...` used for basic-auth, or a shell-style price string) gets misinterpreted as an unset-variable reference and silently replaced with an empty default.
- **Impact**: Silent data corruption of any compose value containing `$`, most commonly hashed secrets.
- **Remediation**: Special-case `$$` → literal `$` in `interpolateEnv` before/alongside the existing variable-substitution regex.

---

### Finding 12 (Minor, previously flagged, still open): Dead `sync.Pool` buffer allocation in `LogFrameProcessor`
- **Location**: `pkg/orchestration/logs.go:16-31`
- Unchanged from the prior audit: `bufferPool` is allocated but `stdcopy.StdCopy` manages its own buffers internally, so the field is inert. Cosmetic; remove or wire it in.

---

## 4. Dimension-by-Dimension Summary (per review brief)

- **Correctness**: Canary percentage math (`traffic_split.go:76-104`) is arithmetically correct (`100 - CanaryPercent` / `CanaryPercent` weight split, clamped 0/100 edge cases handled explicitly). Blue-green state machine is correct on the happy path but has a real loopback-fallback defect (Finding 8) and a swallowed-error defect (Finding 9). Compose parsing has multiple silent-drop edge cases (Finding 10, 11). Placement-constraint matching logic is internally correct but structurally disconnected from the deploy path (Finding 4, 5).
- **Robustness**: This is the weakest dimension. Partial-failure handling in `DeployStack` is *mostly* there (containers + networks correctly torn down) but leaks volumes and swallows provisioning errors (Finding 6). Context cancellation is well-threaded through the public interfaces but undermined by a client-wide HTTP timeout that breaks streaming (Finding 1) and by `context.Background()` in rollback/teardown paths. There is no retry/backoff anywhere on Docker or Caddy API calls — a single transient network blip fails the whole operation; given the codebase's otherwise careful error handling this is a design gap worth tracking, though not scored as a standalone finding since neither spec explicitly mandates retries.
- **Performance**: Bounded polling (200–250ms tickers capped by short overall timeouts) is appropriate for the deploy-time windows involved; no evidence of N+1 Docker API calls or unbounded log buffering. Not a significant concern in this boundary.
- **Security**: The two Critical findings (1, 2) and the allowlist bypass (Finding 7) are the load-bearing items. The On-Demand TLS `ask` gate (`pkg/ingress/ask.go`) and the typed-JSON Caddy client are both genuinely solid and correctly close off the injection/ACME-abuse vectors named in the brief.

---

## 5. Prioritized Remediations

| Priority | Finding | Description | Location | Effort |
| :---: | :---: | :--- | :--- | :---: |
| **P0** | 1 | Remove client-wide `http.Client.Timeout`; rely on per-call `context.Context` | `pkg/orchestration/engine.go:31` | 15 min |
| **P0** | 2 | Stop trusting unauthenticated `X-Forwarded-For`/`X-Real-IP`; cap rate-limiter bucket count | `pkg/deploy/handler.go:307-322`, `pkg/deploy/rate_limiter.go:38-44` | 1-2 hrs |
| **P1** | 7 | Boundary-safe registry allowlist prefix check | `pkg/deploy/handler.go:147` | 20 min |
| **P1** | 3 | Reconcile traffic-split routes into `ReconcileAll`/invalidate `c.splits` cache on delete | `pkg/ingress/manager.go:117-207`, `pkg/ingress/traffic_split.go:169,230` | 2-4 hrs |
| **P1** | 6 | Check `NetworkCreate`/`VolumeCreate` errors; clean up `createdVols` in rollback | `pkg/orchestration/compose.go:365-395,404-417` | 30 min |
| **P1** | 9 | Abort canary rollout on `SetTrafficSplit` step failure instead of proceeding to cutover | `pkg/orchestration/blue_green.go:298` | 20 min |
| **P1** | 8 | Fail fast on empty green-container IP instead of falling back to `127.0.0.1` | `pkg/orchestration/blue_green.go:214-220` | 20 min |
| **P2** | 4 | Wire `ParseConstraints`/`ValidateConstraintsAgainstNodes` into `CreateService`/`UpdateService` | `pkg/orchestration/swarm.go:111-143` | 1-2 hrs |
| **P2** | 5 | Add `NodeStatus.EngineLabels`, fix `engine.labels.` matching | `pkg/orchestration/constraints.go:99-104`, `swarm.go:328-340` | 20 min |
| **P3** | 10 | Reject/warn on unsupported Compose port syntax; parse `deploy.resources` | `pkg/orchestration/compose.go:241-266` | 1-2 hrs |
| **P3** | 11 | Honor `$$` escape in compose env interpolation | `pkg/orchestration/compose.go:20` | 15 min |
| **P3** | 12 | Remove or wire the dead `bufferPool` in `LogFrameProcessor` | `pkg/orchestration/logs.go:16-31` | 5 min |

---

## 6. Test Coverage Notes

All orchestration/ingress/deploy tests pass against mocked `client.CommonAPIClient`/`CaddyClient` implementations (`mock_test.go`, in-process `httptest` servers). This is why Finding 1 (real `http.Client.Timeout` on the real SDK client) and Finding 2 (real `extractClientIP` header trust) are invisible to the existing suite — neither test exercises the actual construction path (`NewDockerEngineClient`) or sends spoofed proxy headers. No test asserts on `createdVols` cleanup during rollback, on `ReconcileAll` behavior while a traffic split is active, or on constraint validation being invoked during `CreateService`. Recommend adding: (a) an integration-style test that spins up `NewDockerEngineClient` against a `httptest`-backed fake Docker socket and asserts a `Follow`-mode log stream survives past 60s; (b) a `handler_test.go` case sending two requests with different spoofed `X-Forwarded-For` values against an invalid token and asserting both are still rate-limited as the same client; (c) a `DeployStack` failure-injection test where `VolumeCreate` succeeds but a later container create fails, asserting the volume is removed.

---

## 7. Conclusion

The zero-shelling invariant holds without exception, and the typed-JSON Caddy client design correctly forecloses the injection risks named in the review brief. However, this boundary is not the 9.4/Minor system the previous audit described: the newer canary-traffic-splitting and deploy-webhook code (introduced since that audit) carry a genuine unauthenticated DoS vector and a silently-broken live-log-streaming defect, both scored Critical per the standardized rubric, plus a cluster of Moderate-severity silent-failure and dead-code issues in the rollback and placement-constraint paths. None of the Critical/Moderate findings require an architectural rewrite — all are localized, mechanical fixes (a client config, a header-trust check, a few missing error checks and cleanup loops) — but they should be treated as release-blocking given the DoS and streaming-breakage findings sit on paths already shipped to users.
