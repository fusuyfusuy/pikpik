# Scope 5 Audit — Frontend (`web/`) & Operator CLI (`cmd/pikpik-cli`)

**Auditor:** Frontend & CLI Boundary Audit
**Method:** mimori warmup (`dump --file`, `map --scope web/src`), 1-hop slices of `api.ts`, `useSSE.ts`, `usePTY.ts`, `client.go`, `config.go`, targeted reads of `main.go`, `routes.go`, all views/components via regex contract greps.

---

## Executive Summary

**Health Score: 7.5 / 10** — **Moderate risk**

The frontend API client (`web/src/lib/api.ts`) achieves **complete route coverage** against `pkg/api/routes.go` — zero concrete endpoint mismatches found. TanStack Query invalidation discipline is uniformly correct (prefix-based `['apps']` invalidation covers parameterized keys). The CLI client is typed against the backend's own DTOs (`api.Response[T]`), which eliminates drift by construction. However, the boundary has **one systemic security leak (auth token in URL query params on every SSE and PTY connection)**, **zero query-error (`isError`) handling in all 12+ views**, and a **CLI `logs` command that silently truncates non-followed output to a single line**.

### Dimension Scores

| Dimension | Score | Verdict |
|---|---|---|
| 1. Correctness | 7.5 | Moderate |
| 2. Robustness | 7.2 | Moderate |
| 3. Performance | 8.5 | Minor |
| 4. Security | 6.5 | **Critical** |
| 5. UX/DX Consistency | 8.0 | Moderate |

---

## 1. Correctness — 7.5 (Moderate)

### 1.1 API client coverage: PASS (no mismatches)

Verified every `api.*` method in `web/src/lib/api.ts` against `pkg/api/routes.go` registrations:

- Web client routes (auth/tokens L160–167, orgs L172–178, projects L182–202, apps incl. env L210–255, stacks L258–281, networks/volumes incl. `prune` L297–320, nodes/machines L324–355, databases L358–375, backups/destinations L378–400, ingress L403–422, registry L425–440, system L443–452, builds L455–469, templates L472–491, schedules L494–511) — **all have matching `mux.Handle` registrations** (routes.go:159–1409). Query params (`search`, `tag`, `org_id`, `project_id`) match backend parsing (routes.go:346–348, 262–263).
- CLI client (`cmd/pikpik-cli/client.go`) deserializes into shared `pkg/api` DTOs (`api.Response[T]`) — contract drift is impossible by construction. All 30 client methods map to live routes.

### 1.2 CLI `logs` non-follow truncation — **BUG (P1)**

`cmd/pikpik-cli/main.go:633–635`:
```go
if !*follow {
    break
}
```
The read loop `break`s after the **first** WebSocket frame when `--follow` is not set, so `pikpik logs <app> -n 100` prints at most **one line** regardless of the `tail=100` request sent in the subscribe params (main.go:583, 598–601). Server-side tail delivery arrives as a burst of frames; the client discards the rest.

### 1.3 CLI `stats` table renders wrong fields — **BUG (P2)**

`cmd/pikpik-cli/main.go:677`:
```go
fmt.Printf("%-20s %-12v %-15v Active\n", frame.TargetID, frame.Event, frame.Data)
```
The `CPU %` column prints `frame.Event` (an event *name*, not a percentage) and the `MEMORY` column prints the **entire raw `frame.Data` object**. Data arrives but is unreadable.

### 1.4 Swallowed config-load errors risk silent context wipe — **BUG (P2)**

- `main.go:218` (`runLogin`), `main.go:860` and `main.go:883` (`runContext`): `cfg, _ := cm.Load()`.
- `config.go:72–74` returns an error on JSON parse failure; discarding it yields an effectively default config, and the subsequent `cm.Save(cfg)` (main.go:272, 889) **atomically overwrites `~/.pikpik/config.json`**, destroying all stored contexts/tokens when the file is corrupt or partially written.

### 1.5 Minor correctness items

- `main.go:740`: `fs.BoolP("all", "a", true, ...)` — default `true` means the `-a` shorthand can only ever *disable* the advertised behavior; the flag is semantically dead as written.
- `main.go:664–679` (`runStats`): read errors `break` silently and the command exits **0** even with zero stats frames — indistinguishable from success in scripts.
- `main.go:604, 664`: `_ = conn.WriteJSON(sub)` — subscription send failures are ignored; the CLI then blocks on read with no diagnosis.
- Web `App.tsx:81–84` (`handleLogout`) does not `queryClient.clear()`; the previous user's cached org/project data survives in the QueryClient until refetch. Low-severity (single-browser SPA) but a tenant-boundary smell.

### 1.6 TanStack Query keys/invalidation: PASS

- `['apps']` prefix invalidation (AppsView.tsx:166/178/193/202/211/220, AppDetailsConsole.tsx:59–109) correctly matches the parameterized key `['apps', selectedProject, selectedTag, searchQuery]` (AppsView.tsx:89) via TanStack prefix semantics.
- `['builds', app.id]` (DeploymentsTrafficTab.tsx:43/53/66) — keys and invalidations are symmetric.
- `['currentUser']` (App.tsx:42) is refetched on login success (App.tsx:78) — correct.

---

## 2. Robustness — 7.2 (Moderate)

### 2.1 Zero query-error states in the entire view tree — **P1**

`rg 'isError' web/src/views` -> **0 matches** across all views. Every `useQuery` destructures only `{ data, isLoading }` (e.g., DashboardView.tsx:42-75, BackupsView.tsx:72-77, NodesView.tsx:53-60). Consequences:

- API outage renders as an **empty cluster**: DashboardView.tsx:96-98 hardcodes `<Badge variant="success" dot pulse>Swarm Operational</Badge>` regardless of whether any query succeeded.
- BackupsView.tsx:72/77 destructure `isLoading` but render empty-table states with no distinction between "no backups" and "backup API failed" — an operator cannot tell a lost backup inventory from a healthy empty state.

### 2.2 ErrorBoundary coverage is root-only — **P2**

- Single boundary at `main.tsx:22`. `App.tsx:118-130` mounts all 12 lazy views inside one `Suspense` with **no per-view boundary**; a render crash in, e.g., `RegistryView` unmounts the entire shell including navigation. The boundary component itself is well-built (reset/reload/home actions, details disclosure, ErrorBoundary.tsx:59-155) — it is just under-deployed.

### 2.3 SSE gives up permanently with no UI recovery — **P2**

- `useSSE.ts:88-100`: after `maxReconnectAttempts` (10) failures, status settles to `'disconnected'` and the hook never retries.
- Consumers never offer recovery: `App.tsx:71-74` only forwards `status` to Layout; `LiveLogsTab.tsx:27-46` uses `status` solely for an empty-state message (L195-196) — no reconnect button (contrast: `TerminalTab.tsx:44-53` correctly exposes one for PTY). A laptop resuming from sleep past the 10-attempt backoff window permanently dead-ends the live event stream until a full page reload.

### 2.4 Xterm.js PTY lifecycle: PASS

`usePTY.ts` is exemplary: terminal + addons created per-mount (L144-189), listeners disposed and `term.dispose()` in cleanup (L213-220), window-resize listener removed, `fit()` guarded against hidden-container throws (L203-206, 232-238), `disconnect()` on unmount (L227). Binary opcode protocol (0x00 data / 0x01 resize / 0xFF exit, L99-130) matches the CLI's wire format exactly (main.go:800-827).

### 2.5 Log-stream memory: PASS

`LiveLogsTab.tsx:37-43` caps the in-memory buffer at 3000 lines with `slice`; `DeploymentsTrafficTab.tsx` uses `@tanstack/react-virtual` for both log panes. No unbounded accumulation found.

### 2.6 Minor

- `api.ts:113-115`: on 401 the token is removed but nothing triggers navigation — `currentUser` in App.tsx stays set, so the user remains on a broken dashboard issuing 401s until a hard reload.
- CLI offline messaging is good: every `getClient()` failure path prints to stderr and exits 1 (main.go:111-137); WS dial failures exit 1 (main.go:587-590, 653-656, 785-788).

---

## 3. Performance — 8.5 (Minor)

### 3.1 Route-level code splitting: PASS

All 12 views are `lazy()`-loaded with per-view chunks (`App.tsx:9-20`) and a proper `Suspense` loader (`ViewLoader`, App.tsx:22-31). `recharts` and `@xterm/*` therefore land in lazy chunks, not the entry bundle.

### 3.2 Mixed realtime strategy: polling where SSE exists — **P3**

`refetchInterval` pollers run alongside the global SSE hub (`App.tsx:71-74`): `DeploymentsTrafficTab.tsx:45` (4s), `NodesView.tsx:56,63` (5s x2), `DiagnosticsTab.tsx:27` (10s). Every tab visit spawns HTTP polling even while the event stream is connected.

### 3.3 Minor

- `vite.config.ts:31-36`: no `manualChunks`; `chunkSizeWarningLimit: 1000` silences the oversized-chunk warning rather than splitting vendors (lucide-react + recharts are heavy). Default Vite splitting still applies, so impact is bounded.
- `vite.config.ts:15`: `allowedHosts: true` disables dev-server host checks — dev-only, but an open-proxy foot-gun if the dev server is bound to a network.
- `CommandPalette.tsx:45-57` fires three parallel list queries on palette mount — acceptable with staleTime 5000 (main.tsx:13), but adds burst load on open.

---

## 4. Security — 6.5 (Critical)

### 4.1 Bearer token leaked in URL query params on every stream — **P1**

- `useSSE.ts:51-55`: `url.searchParams.set('token', token)` for **every** SSE connection (global events, all log streams, stats).
- `usePTY.ts:79-80`: same for every interactive terminal.
- The backend explicitly accepts this (`pkg/api/auth.go:95`: `r.URL.Query().Get("token")`), so it is sanctioned design — but it places the long-lived `pik_live_...` bearer token into access logs, Caddy logs, browser devtools, and any intermediate proxy. This is the single most impactful boundary defect.

**Remediation:** EventSource cannot set headers, but the backend already supports `Sec-WebSocket-Protocol: pikpik-auth.<token>` (auth.go:78), used by the CLI (client.go:338-345). For SSE, issue a **short-lived single-use stream ticket** (`POST /api/v1/auth/stream-ticket` -> `?ticket=...`), or move log/stat streaming to the existing WS endpoints with subprotocol auth. Never put the primary token in a URL.

### 4.2 Secrets persisted in localStorage — **P2**

- Session token: `api.ts:62-71` stores the bearer token in `localStorage` (`pikpik_token`), readable by any XSS payload and browser extensions. Note `useSSE.ts:58` even sends `withCredentials: true`, which is dead code today — the backend cookie auth path (auth.go:70-78) is unused by the SPA.
- Cloudflare DNS-01 API token: `Dns01ProviderModal.tsx:36` writes `pikpik_cf_dns01_<domain>` (a third-party **account-scoped** credential) into localStorage with no expiry.

### 4.3 XSS sinks: mitigated, verify invariant — **PASS with caveat**

`LiveLogsTab.tsx:222-224` and `DeploymentsTrafficTab.tsx:428-430` render `ansi.ansi_to_html(...)` via `dangerouslySetInnerHTML`. `ansi_up@6` sets `escape_for_html = true` by default, so container-controlled log content is HTML-escaped before ANSI wrapping — **currently safe**. Caveat: safety rests on an unexported default of a shared module-level instance (`LiveLogsTab.tsx:14`); if anyone sets `ansi.escape_for_html = false`, both sinks become direct container-log XSS. Add a startup assert or helper that forces the flag. No other `dangerouslySetInnerHTML`/`innerHTML` sinks exist in `web/src`.

### 4.4 CSRF posture: acceptable-by-design

All auth is `Authorization: Bearer` header based (`api.ts:97-99`); no cookie is attached to `fetch`, so CSRF is structurally mitigated. The `withCredentials: true` in `useSSE.ts:58` is currently inert; document the assumption before adopting cookie auth.

### 4.5 CLI credential file handling: PASS

`config.go:40` (dir `0700`), `config.go:96` (file `0600`), atomic tmp-file + `os.Rename` (L95-103), `TLSSkipVerify` opt-in per-context via `-k/--insecure` (main.go:193). Two nits:

- `Save()` does not `fsync` the tmp file before rename — a crash can rename a zero-length file, which then trips the silent-wipe bug in section 1.4. Combined risk.
- `PIKPIK_TOKEN` env override is only honored when `PIKPIK_SERVER_URL` is also set (config.go:121-127) — surprising precedence for CI; a token env alone is silently ignored.
- CLI WS auth uses the subprotocol header, not URLs (client.go:338-345) — **good**; the PTY `cmd` query param (main.go:781) contains only a command string, no secrets.

### 4.6 Secret leakage in CLI output: PASS (reviewed)

`machine enroll` prints a one-shot enrollment token (main.go:1438) — expected operator flow. `nodes join-tokens` (main.go:433) prints Swarm join secrets to stdout — inherent to the feature; no `--json` masking available (see section 5.2). No token echo in error paths found.

---

## 5. UX/DX Consistency — 8.0 (Moderate)

### 5.1 Toast/modal discipline: PASS

Every mutation across all views pairs `onSuccess` toast + `onError` toast with typed titles (AppsView.tsx:153-222, BackupsView.tsx:101-154, NodesView.tsx:86-114, SettingsView.tsx:55-67, SystemView.tsx:43-49, RegistryView.tsx:40, MarketplaceView.tsx:82-87, AppDetailsConsole.tsx:58-113). Destructive actions use pending-state buttons (`isLoading={...isPending}`). Consistent.

### 5.2 `--json` output mode is inconsistently implemented — **P2**

`--json` exists only for `stack list/inspect` (main.go:957, 992), `network list` (main.go:1106), and `volume list` (~main.go:1221). It is absent for `apps`, `projects`, `nodes`, `tags`, `db backups`, `machine list/inspect`, `machine metrics` — the most script-relevant commands. Also `stack logs` emits raw JSON unconditionally with no flag (main.go:1064). Pick one: global `--json` flag parsed in `main()` or per-command parity.

### 5.3 CLI Stage-3 coverage gaps vs Web (AGENTS.md section 3) — **P3**

The web UI can manage **ingress domains/certificates** (`api.ts:403-422`), the **registry** (L425-440), **templates/marketplace** (L472-491), and **backup schedules** (L494-511), but `cmd/pikpik-cli` has no `pikpik domain|ingress`, `pikpik registry`, `pikpik template`, or `pikpik schedule` subcommands (main.go:36-78 dispatch table). The "CLI-first, web is clothing" lifecycle invariant is violated for these four domains.

### 5.4 Minor

- `runApps` parses `-t/--tag` (main.go:357) but filters **client-side** after fetching the full list (main.go:361-380) instead of passing `?tag=` like the web client does (api.ts:214; backend supports it, routes.go:348) — wasted transfer, divergent semantics.
- Flag parsing uses `pflag` with POSIX shorthands and `--` separator support in `exec` (main.go:769-774) — good. `flag.ExitOnError` exits with code 2 on parse errors vs 1 elsewhere (main.go:141 etc.) — script detectability inconsistency.
- Table formatting is consistent aligned-`printf` style; truncation is applied to tags (main.go:347-349, 382-384) but not to names/images, so long values break column alignment.
- Web `apps.list` `search` matches only app name server-side (routes.go:371) while the UI implies a general search — minor expectation gap.

---

## Findings Register (ranked)

| ID | Severity | Finding | Location |
|---|---|---|---|
| F-1 | **P1 / Critical** | Bearer token in URL query params on all SSE + PTY connections; lands in logs/proxies | `useSSE.ts:54`, `usePTY.ts:80`, sanctioned by `auth.go:95` |
| F-2 | **P1** | Zero `isError` handling in all views; dashboard shows hardcoded "Swarm Operational" during total API failure | `DashboardView.tsx:96-98`, all `useQuery` sites |
| F-3 | **P1** | CLI `logs` (no `--follow`) prints only 1 line regardless of `--tail` | `main.go:633-635` |
| F-4 | P2 | Corrupt config + ignored `Load()` error -> `Save()` wipes all stored contexts | `main.go:218, 860, 883` + `config.go:72-74` |
| F-5 | P2 | CLI `stats` table prints event name in CPU column, raw object in MEMORY column | `main.go:677` |
| F-6 | P2 | Root-only ErrorBoundary; one view crash kills whole shell | `main.tsx:22`, `App.tsx:118-130` |
| F-7 | P2 | SSE permanently dead after 10 failed reconnects; no reconnect UI for event/log streams | `useSSE.ts:88-100`, `LiveLogsTab.tsx:27-46` |
| F-8 | P2 | Secrets in localStorage: session token + Cloudflare API token | `api.ts:62-71`, `Dns01ProviderModal.tsx:36` |
| F-9 | P2 | `--json` only on stack/network/volume; missing for apps/projects/nodes/db/machine | `main.go:957-1221` |
| F-10 | P3 | No fsync before config rename (combines with F-4) | `config.go:96-103` |
| F-11 | P3 | 401 removes token but no navigation/cache-clear; logout doesn't `queryClient.clear()` | `api.ts:113-115`, `App.tsx:81-84` |
| F-12 | P3 | Polling intervals where SSE hub already streams | `DeploymentsTrafficTab.tsx:45`, `NodesView.tsx:56,63` |
| F-13 | P3 | CLI missing ingress/registry/templates/schedules subcommands (Stage-3 invariant) | `main.go:36-78` |
| F-14 | P3 | `ansi_up` escape default is the only XSS guard on 2 sinks; not asserted | `LiveLogsTab.tsx:14,222`, `DeploymentsTrafficTab.tsx:428` |
| F-15 | P3 | `prune --all` default `true` makes `-a` meaningless; exit-code 2 vs 1 inconsistency; client-side tag filter | `main.go:740, 141, 361-380` |

---

## Remediation Plan

**P1 (this week)**

1. F-1: Add short-lived stream tickets (`POST /api/v1/auth/stream-ticket`) or reuse WS subprotocol auth for SSE/PTY; delete `token` from all client-constructed URLs (`useSSE.ts:54`, `usePTY.ts:80`) and the query-param fallback in `auth.go:95` once web clients migrate.
2. F-2: Add a shared `<QueryStateBoundary>` that renders error+retry when `isError`; wire into every `useQuery` view; make the dashboard status badge reflect real query health.
3. F-3: In `runLogs`, when `!follow`, read until the server closes the stream (server-sent close frame / read deadline after tail burst) instead of breaking on the first message.

**P2 (next sprint)**

4. F-4 + F-10: Propagate `Load()` errors (exit 1 with message); fsync tmp file before rename in `config.go:Save`.
5. F-5: Decode the stats frame's typed metrics payload (`api.WSMessage.Data` -> metrics struct) before tabulating.
6. F-6: Wrap each lazy view in `<ErrorBoundary>` at `App.tsx:119-129`.
7. F-7: Expose `reconnect` from `useSSE` in the Layout status pill and LiveLogsTab; reset attempt counter on `visibilitychange`.
8. F-8: Move session token to HttpOnly cookie (backend already supports it); stop persisting the Cloudflare token — keep in memory with an explicit "not saved" notice.
9. F-9: Global `--json` flag in `main()` before dispatch; apply to every list/inspect command.

**P3 (backlog)**

10. F-11 to F-15: logout cache clear, drop redundant pollers, add CLI `domain/registry/template/schedule` commands, ANSI escape assertion, unify exit codes, `prune --all` default fix.

---

## Verification

- API coverage cross-check: every method in `web/src/lib/api.ts` (16 namespaces) matched against `mux.Handle` registrations in `pkg/api/routes.go` — 0 mismatches.
- CLI client methods (`client.go:101-524`, 30 methods) all deserialize shared `pkg/api` DTOs — 0 contract drift.
- Slices used: `api.ts:1-512`, `useSSE.ts:1-127`, `usePTY.ts:1-250`, `client.go:1-525`, `config.go:1-134`, `main.go` targeted regions, `routes.go` route table, view contract greps (`isError` -> 0 hits).
