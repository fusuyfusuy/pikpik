# Scope 3 Architectural & Code Quality Audit Report
## Git/GitHub Integration, Build Engine (BuildKit/Nixpacks), Embedded Registry & Template Marketplace

- **Target Subsystem**: Scope 3 — Git & GitHub App Integration, Build Manager, Embedded OCI Registry, 1-Click Template Marketplace
- **Repository**: `github.com/fusuycorp/pikpik`
- **Auditor**: Scope 3 Auditor (Git/Build/Registry/Marketplace)
- **Review Date**: August 30, 2026
- **Status**: Audit Completed — First-Ever Pass (No Prior Baseline)
- **Overall Health Score**: **6.1 / 10.0** (Critical Band)

> This subsystem was added in the last two commits (`9e089a3`, `9d4eacc`) and had zero prior review. A **critical, unauthenticated build-trigger / RCE-adjacent vulnerability chain** was found spanning `pkg/api/routes.go` → `pkg/api/controller.go` → `pkg/git/clone.go`. This single chain is severe enough to place the whole subsystem in the Critical band despite otherwise solid code (correct HMAC primitive, no shell interpolation, bcrypt + CSPRNG credentials).

---

## 1. Executive Summary & Scorecard

| Component | Target Files | Health Score | Status | Primary Findings |
| :--- | :--- | :---: | :---: | :--- |
| **Git / GitHub App Integration** | [`pkg/git/webhook.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/git/webhook.go), [`pkg/git/clone.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/git/clone.go), [`pkg/git/github.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/git/github.go) + [`pkg/api/routes.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/routes.go), [`pkg/api/controller.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/api/controller.go) | **4.8 / 10.0** | **Critical** | Webhook signature check fails open when signature header absent; no clone-URL scheme allowlist (git `ext::`/`file://` transports reachable) → unauthenticated build trigger chains into SSRF/possible RCE; generic webhook has no request-body ceiling; silent swallow of failed commit checkout. |
| **Build Engine (BuildKit + Nixpacks)** | [`pkg/build/manager.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/build/manager.go), [`pkg/build/dockerfile.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/build/dockerfile.go), [`pkg/build/nixpacks.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/build/nixpacks.go) | **7.6 / 10.0** | Moderate | No build/clone timeout — jobs can hang indefinitely on a malicious/slow repo; build context tar has no size ceiling and ignores `.dockerignore`; 1MB scanner line limit can truncate build log parsing. |
| **Embedded OCI Registry** | [`pkg/registry/manager.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/registry/manager.go), [`pkg/registry/robot_auth.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/registry/robot_auth.go), [`pkg/registry/config_generator.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/registry/config_generator.go) | **7.5 / 10.0** | Moderate | `htpasswd`/`config.yml` written world-readable (`0644`); S3 backend embeds plaintext `AccessKey`/`SecretKey` in that same world-readable config file; robot credentials never expire/rotate. Positives: CSPRNG tokens, bcrypt-10 hashing, secrets scrubbed from list responses. |
| **Template Marketplace (21 templates)** | [`pkg/templates/deployer.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/templates/deployer.go), [`pkg/templates/catalog.go`](file:///home/devhax/projects/fusuycorp/pikpik/pkg/templates/catalog.go) | **7.8 / 10.0** | Moderate | `Deploy()` has no rollback/compensation on partial mid-stack failure (orphaned network/volumes/DB rows/containers); `sanitizeResolvedVariables` is a dead-code no-op (unused `tpl` param) that returns all secrets, including auto-generated passwords, verbatim; ~10/21 catalog images pinned to `:latest`. |
| **Composite Score** | *All Scope 3 Packages* | **6.1 / 10.0** | **Critical Band** | **Fix the webhook auth-bypass + clone URL allowlist before anything else; everything else is remediable follow-up.** |

---

## 2. Invariant Breaches

### INV-1 (CRITICAL): Webhook signature verification fails open when the signature header is simply omitted
`pkg/api/controller.go:1484-1489`:
```go
func (c *DefaultController) HandleGitHubWebhook(ctx context.Context, secret string, signature string, payload []byte) (*store.Build, error) {
	if secret != "" && signature != "" {
		if !git.VerifyGitHubSignature(secret, payload, signature) {
			return nil, errors.New("invalid webhook signature")
		}
	}
	event, err := git.ParseGitHubPushEvent(payload)
```
The route (`pkg/api/routes.go:840-864`) is fully public (no `authWrap`). It reads `sig := r.Header.Get("X-Hub-Signature-256")` (falling back to the legacy header) and passes it straight through. The guard is `secret != "" && signature != ""` — **short-circuits to skip verification whenever either side is empty**. Concretely:
- If `GITHUB_WEBHOOK_SECRET` is unset (operator forgot to configure it) → verification never runs, full stop.
- **Even when the secret IS configured**, an attacker who simply does not send `X-Hub-Signature-256`/`X-Hub-Signature` bypasses verification entirely, because `signature == ""` makes the `&&` short-circuit false. `git.VerifyGitHubSignature` itself (`pkg/git/webhook.go:30-59`) is implemented correctly (constant-time compare, proper `sha256=` prefix handling) — the bug is entirely in the caller's fail-open branching, not the crypto primitive.
- Net effect: `POST /api/v1/webhooks/github` with an arbitrary JSON body and no signature header enqueues a real `BuildJob` with attacker-controlled `RepoURL`, `Branch`, `CommitSHA` (`pkg/api/controller.go:1510-1523`). This is an unauthenticated-mutation class bug per the rubric.

The generic webhook has the same fail-open shape one level up: `pkg/api/controller.go:1559-1565`
```go
if err == nil && svc != nil && svc.DeployTokenHash != "" {
    if token == "" || auth.HashToken(token) != svc.DeployTokenHash {
        return nil, errors.New("unauthorized git webhook token")
    }
}
```
If the service has never had a deploy token configured (`DeployTokenHash == ""`), the whole block is skipped — any token (including none) is accepted.

### INV-2 (CRITICAL): No scheme/transport allowlist on attacker-influenced clone URLs → SSRF / possible RCE
`pkg/git/clone.go:26-74` builds `git clone -- <cloneURL> <workDir>` via `exec.CommandContext` (good — no shell interpolation), but **`opts.RepoURL` is never validated against a URL-scheme allowlist**. The only two things it does with the URL are check it's non-empty and, if `opts.Token` is set, rewrite it *only when it already starts with `http(s)://`*. Any other scheme passes straight to `git clone` verbatim, including:
- `ext::sh -c '...'` — git's `ext` remote helper executes an arbitrary shell command as part of "cloning" it. Because pikpik invokes `git clone <url>` as a direct, explicit command (not a recursive/submodule fetch), this satisfies git's default `protocol.ext.allow=user` policy and will run.
- `file:///etc/...` or `file:///proc/self/environ` — arbitrary local file read via crafted repo content.
- Arbitrary internal `http(s)://169.254.169.254/...` or intranet hosts — SSRF against cloud metadata / internal services.

Chained with INV-1, `event.CloneURL` from the *unauthenticated* GitHub webhook JSON body (`{"repository":{"clone_url": "ext::sh -c ..."}}`) flows unmodified into `job.RepoURL` (`pkg/api/controller.go:1514`) → `git.CloneOptions.RepoURL` (`pkg/build/manager.go:350-360`) → `git.CloneRepository` (`pkg/git/clone.go:26`). **This is a full unauthenticated remote command execution chain on the build host**, not merely a theoretical SSRF.

### INV-3 (Moderate): Generic git webhook has no request-body size ceiling
The GitHub webhook route wraps the body in `io.LimitReader(r.Body, 5*1024*1024)` (`pkg/api/routes.go:847`), but the generic webhook route does not — `git.ParseGenericGitPush` (`pkg/git/webhook.go:212-219`) calls `io.ReadAll(r.Body)` directly with no limit. Combined with the fail-open token check above (INV-1), an unauthenticated caller can stream an unbounded body into memory (DoS).

### INV-4 (Moderate): Template deploy has no partial-failure compensation
`pkg/templates/deployer.go:100-291` — `Deploy()` sequentially: creates a Docker network, computes volume host paths, **persists Service/EnvVar/Volume/Deployment rows to the store**, then loops over `orderedServices` calling `Containers().Create`/`Start`. If any container after the first fails (`pkg/templates/deployer.go:279-287`), the function returns the error immediately with **no cleanup** of the already-created network, already-persisted DB rows (which still say `Status: "running"`), or already-started earlier containers. The deployment is left in a half-applied, inconsistent state with no automatic reconciliation path.

### INV-5 (Moderate): Secret redaction function is a no-op dead stub
`pkg/templates/deployer.go:525-531`:
```go
func sanitizeResolvedVariables(vars map[string]string, tpl *Template) map[string]string {
	res := make(map[string]string)
	for k, v := range vars {
		res[k] = v
	}
	return res
}
```
The `tpl` parameter is accepted but never read — a strong signal this was meant to strip `IsSecret`/auto-generated values (the sibling helper `isSecretVar`, `pkg/templates/deployer.go:515-523`, already exists and is unused here) but the redaction was never wired in. `DeployTemplateResponse.ResolvedVariables` (returned to the caller of `POST /api/v1/templates/{id}/deploy`, `pkg/api/routes.go:958-974`, gated at `RoleDeveloper`) therefore contains every auto-generated password/API key in plaintext, under a function name that falsely implies sanitization has occurred.

---

## 3. Top Findings (exact file:line references)

| # | Severity | Finding | Location |
|---|---|---|---|
| 1 | **Critical (< 7.0)** | Webhook HMAC check fails open whenever the signature header is absent, regardless of whether `GITHUB_WEBHOOK_SECRET` is configured — unauthenticated build trigger. | `pkg/api/controller.go:1485-1488`, route wiring `pkg/api/routes.go:840-864` |
| 2 | **Critical (< 7.0)** | `CloneRepository` performs zero scheme allowlisting on `opts.RepoURL`; git `ext::`/`file://` transports are reachable, enabling RCE/SSRF/arbitrary file read when chained with #1's unauthenticated `CloneURL` injection. | `pkg/git/clone.go:26-74` (no validation before line 62 `args := []string{"clone"}`) |
| 3 | **Moderate (7.0-8.4)** | Generic webhook token check silently accepts any/no token when `svc.DeployTokenHash` is empty (unconfigured), and the request body is read via unbounded `io.ReadAll` with no size cap — DoS + auth-bypass-by-default. | `pkg/api/controller.go:1559-1568`, `pkg/git/webhook.go:212-219` |
| 4 | **Moderate (7.0-8.4)** | `templates.Deploy()` has no rollback on mid-stack container/service failure — orphaned network, volumes, DB rows (`Status: "running"`), and already-started containers persist after a reported error. | `pkg/templates/deployer.go:279-291` |
| 5 | **Moderate (7.0-8.4)** | `sanitizeResolvedVariables` is an unused-parameter no-op; all auto-generated secrets (DB passwords, API keys) are returned verbatim in the deploy API response under a function name implying redaction. | `pkg/templates/deployer.go:525-531` (compare unused `isSecretVar` at `:515-523`) |

### Additional notable findings (Minor/Moderate, not in top-5 but worth tracking)
- **`pkg/git/clone.go:116-129`** — if the shallow-clone checkout of `opts.CommitSHA` fails, the code fetches and retries but discards the *second* checkout's error entirely (`_ = exec.CommandContext(...).Run()`), silently proceeding to build whatever commit happens to be checked out (branch HEAD) instead of the requested SHA. Silent correctness drift, not fail-fast.
- **`pkg/build/manager.go:285`** — `executeJob` uses `context.WithCancel(context.Background())` with no deadline; a hung `docker build` or nixpacks process (or a Dockerfile with an infinite `RUN`) blocks a worker slot forever. No build-level timeout exists anywhere in `pkg/build`.
- **`pkg/build/dockerfile.go:126-212`** — `CreateTarStream` has no build-context size ceiling and only excludes `.git`/`.DS_Store` (no `.dockerignore` support), risking large/secret-laden context uploads.
- **`pkg/registry/manager.go:81,91,317`** — `config.yml` and `htpasswd` are written with `0644` (world-readable). When S3 storage backend is configured, `config_generator.go:137-146` embeds plaintext `AccessKey`/`SecretKey` directly into that same `0644` file (`pkg/registry/config_generator.go:132-146`).
- **`pkg/registry/manager.go`** — robot credentials (`GenerateRobotCredential`, `robot_auth.go:30-64`) have no expiry/rotation mechanism; `LastUsedAt` field exists on `RobotCredential` (`types.go:47`) but is never updated anywhere in `manager.go`.
- **`pkg/templates/catalog.go`** — ~10/21 templates pin `Image: "...:latest"` (e.g. `pocketbase:latest` L193, `n8n:latest` L251, `vaultwarden/server:latest` L301, `directus:latest` L427, `wordpress:latest` L935, `clickhouse-server:latest` L1211) — no digest/version pinning means re-deploys of the "same" template can silently pull different image content over time.
- **`pkg/templates/deployer.go`** — dependency ordering (`resolveServiceOrder`) is a correct Kahn's-algorithm topological sort, but containers are `Create`+`Start`-ed in order with no wait for the dependency's `HealthCheck` to pass before starting the dependent — first-boot races (e.g., app container starting before its DB is accepting connections) are possible for multi-service templates.

---

## 4. What's Solid (for balance)

- `pkg/git/webhook.go:30-59` `VerifyGitHubSignature` itself: correct `sha256=`/`sha256:` prefix handling, `hex.DecodeString` guarded, length-checked before `subtle.ConstantTimeCompare` — a textbook-correct implementation; the defect is entirely in how callers gate its invocation (INV-1).
- `pkg/git/clone.go` and `pkg/build/nixpacks.go` both consistently use `exec.CommandContext` with argv slices — zero shell interpolation anywhere, so classic shell-metacharacter injection via branch names/env values is not reachable (branch names starting with `-` are explicitly rejected at `clone.go:57-60` to stop flag injection).
- `pkg/git/github.go` GitHub App JWT: correct RS256 construction, `iat` backdated 60s for clock skew, 10-minute expiry, installation-token caching with a 5-minute reuse buffer and mutex-guarded map (`GetInstallationToken`, `github.go:162-218`).
- `pkg/registry/robot_auth.go`: tokens generated via `crypto/rand` (32 bytes), bcrypt cost 10, secrets scrubbed from `ListRobotAccounts` (`manager.go:262-277`) — correct "show secret once" pattern.
- `pkg/templates/catalog.go` is a fully static, code-defined catalog (no dynamic/user-supplied template loading found anywhere in the package) — the "unsanitized catalog data" risk dimension called out in scope is effectively not applicable here; catalog content is developer-curated, not attacker-influenced.
- `pkg/templates/deployer.go:438-493` `resolveServiceOrder` — correct, cycle-detecting topological sort (Kahn's algorithm) for multi-service template dependency ordering.

---

## 5. Actionable Remediations (priority order)

1. **Fix the webhook auth gate (INV-1)** — invert the logic in `pkg/api/controller.go:1484-1489`: if `secret != ""` (i.e., webhook verification is configured/expected), *require* `signature != ""` and reject otherwise; never let an absent header silently skip verification. Same fix pattern for `HandleGenericGitWebhook` (`controller.go:1559-1565`): require `DeployTokenHash` to be configured for the route to be enabled, or reject when unconfigured, rather than treating "no token configured" as "no auth needed."
2. **Add a clone URL scheme allowlist (INV-2)** — in `git.CloneRepository` (`clone.go:26`), before building `args`, validate `opts.RepoURL` parses via `net/url` and its scheme is in `{"https", "ssh", "git"}` (reject `http`, `ext`, `file`, and anything else); reject if `url.Host` resolves to a loopback/link-local/private range unless explicitly allowed, to close the SSRF angle too. Also set `GIT_ALLOW_PROTOCOL=https:ssh:git` in `cmdEnv` as defense-in-depth against transport-level surprises.
3. **Cap the generic webhook body (INV-3)** — wrap `r.Body` with `io.LimitReader` before calling `git.ParseGenericGitPush`, matching the 5MB ceiling already used on the GitHub route (`routes.go:847`).
4. **Add rollback to `templates.Deploy()` (INV-4)** — track created resources (network ID, volume paths, container IDs, store row IDs) as they're created and defer a compensating cleanup (`NetworkRemove`, `Containers().Remove`, mark `Service.Status = "failed"` or delete rows) on any error path after the first side effect.
5. **Implement real secret redaction (INV-5)** — make `sanitizeResolvedVariables` actually use `isSecretVar(tpl, k)` to mask/omit secret values in `ResolvedVariables`, or explicitly document+test that full exposure is intentional (one-time-reveal pattern like the registry robot token) and rename the function so it stops claiming to sanitize.
6. Add a per-job build timeout (`context.WithTimeout`) in `pkg/build/manager.go:285` (e.g., a configurable ceiling, default 30-60 min) so a stuck build/clone can't permanently occupy a worker slot.
7. Change `os.WriteFile` permissions for `htpasswd`/`config.yml` in `pkg/registry/manager.go:81,91,317` from `0644` to `0600`; for S3-backed registries, consider not writing the S3 secret key into a file readable by the registry container's mount at all (env var injection instead), or ensure the mount and file share the tightest possible perms.

---

## 6. Verification

A machine-runnable check exercising the INV-1 auth-bypass fix (place as `pkg/api/controller_webhook_auth_test.go` or similar):

```go
func TestHandleGitHubWebhook_RejectsMissingSignatureWhenSecretConfigured(t *testing.T) {
    ctrl := NewDefaultController(...) // wire minimal deps
    payload := []byte(`{"ref":"refs/heads/main","repository":{"clone_url":"ext::sh -c id"}}`)
    _, err := ctrl.HandleGitHubWebhook(context.Background(), "configured-secret", "", payload)
    if err == nil {
        t.Fatal("expected rejection when signature header is absent but secret is configured")
    }
}
```
Currently this test would **fail to reject** (build enqueued) against the code at `pkg/api/controller.go:1485`, confirming INV-1 as reproducible today. Run: `go test ./pkg/api/... -run TestHandleGitHubWebhook_RejectsMissingSignature -v`.

**Changed**: none (audit-only pass, no code modified).
**Verified**: INV-1 and INV-2 traced end-to-end from public HTTP route through to `exec.CommandContext` call site via static read; INV-3/4/5 confirmed by direct code inspection and absence of corresponding test coverage (`grep` across `*_test.go` in scope found no rollback, no redaction, no oversized-body, and no missing-signature-header test cases).
**Deferred**: dynamic verification (actually POSTing to a running instance, actually invoking `git clone ext::...`) was not performed — this is a static/read-only audit. Recommend the remediation owner add the test above and a `go vet`/staticcheck pass before merging fixes.
