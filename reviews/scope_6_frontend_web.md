# Scope 6 — Frontend Web SPA Audit

**Subsystem:** `web/src/**` (React 19 SPA), `web/embed.go`, `cmd/pikpik/static.go` (Go embed.FS SPA server)
**Status:** Previously unaudited. First pass.
**Health Score: 7.6 / 10 (Moderate)** — no RCE/critical XSS/unauthenticated-mutation found, but real security and robustness gaps around token handling, unhandled async paths, missing error boundary, and a monolithic bundle with a stubbed-out "live" log viewer.

---

## Scope Reviewed

- `web/src/lib/api.ts` (398 ln), `types.ts` (555 ln), `utils.ts` (45 ln)
- `web/src/hooks/useSSE.ts` (128 ln), `usePTY.ts` (251 ln)
- All 12 `web/src/views/*.tsx`, `Layout.tsx`, `App.tsx`, `main.tsx`
- `web/src/components/ui/*.tsx` (8 files)
- `web/embed.go`, `cmd/pikpik/static.go` (Go SPA static file server)
- `web/dist/assets/*` (built bundle), `web/package.json`, `web/vite.config.ts`, `tsconfig.json`

---

## Invariant Breaches

None of Critical severity (<7.0) were found. No RCE, no unescaped/unsanitized XSS sink, no unauthenticated mutation path, and no silent data-loss path were identified in this boundary. The findings below sit in the Moderate (7.0–8.4) and Minor (8.5–9.4) bands.

---

## Top 5 Findings

### 1. Auth token carried in URL query string for SSE and WebSocket connections (Moderate, ~7.5)
- `web/src/hooks/useSSE.ts:51-58` — `url.searchParams.set('token', token)` then opens `new EventSource(url, { withCredentials: true })`.
- `web/src/hooks/usePTY.ts:79-82` — same pattern: `url.searchParams.set('token', token)` before `new WebSocket(url)`.
- Root cause: neither `EventSource` nor the browser `WebSocket` constructor supports custom headers, so the bearer token is smuggled via query string as a workaround. This means the live session token lands in: server access logs, any intermediate reverse-proxy/CDN logs, and (for EventSource, which is fetched like any other URL) `Referer` headers if the page ever navigates from a URL containing it. It is not written to browser history since these aren't top-level navigations, but log exposure remains real for a self-hosted ops platform where access logs are often shipped to third-party log aggregators.
- Also note `withCredentials: true` on the EventSource combined with the query-string token is redundant/confusing — pick one channel (cookie or token-in-URL), not both; mixing them suggests the CSRF/session model wasn't fully settled during implementation.
- Remediation: Move to short-lived, single-use, scope-limited SSE/WS tickets minted by a dedicated `/api/v1/auth/ws-ticket` endpoint (server issues a token good for one connection attempt, expires in seconds), rather than the long-lived bearer token used for REST calls.

### 2. `request<T>()` has no protection against non-JSON error bodies — unhandled promise rejection risk (Moderate, ~7.5)
- `web/src/lib/api.ts:74-112`, specifically line 97: `const json = await response.json();` runs unconditionally, including on non-2xx responses. If a reverse proxy, WAF, or upstream returns an HTML error page (502/504/413 from nginx, a Cloudflare block page, etc.) instead of the app's JSON envelope, `response.json()` throws a `SyntaxError` that propagates out of `request()` uncaught by any of the code in this file.
- Most call sites route through React Query (`useQuery`/`useMutation`), which does catch this and exposes it via `onError`/`isError` — that path is fine. But direct call sites that are `await`ed outside of React Query's wrapper (e.g. `web/src/App.tsx:70-73` `handleLogout`, and `api.auth.logout()`'s own `try { await request(...) } finally { removeToken(); }` at `web/src/lib/api.ts:127-133`) do not catch this. In `handleLogout`, if `logout()` throws (network blip, non-JSON error body), `setCurrentUser(null)` on the next line never runs — the UI stays on the authenticated view with a now-invalid/removed token, requiring a manual refresh to reach the login screen.
- Remediation: guard the JSON parse (`response.json().catch(() => ({}))`) inside `request()`, and wrap `App.tsx`'s `handleLogout` in try/finally so `setCurrentUser(null)` always runs regardless of network outcome.

### 3. No React error boundary anywhere in the tree (Moderate, ~7.8)
- Confirmed via search: zero occurrences of `ErrorBoundary`/`componentDidCatch` across `web/src/`. `web/src/main.tsx:19-27` wraps `App` only in `QueryClientProvider` and `ToastProvider`, no error boundary.
- Given 12 fairly large, independently-authored views (`AppsView.tsx` alone is 1617 lines with several nested subcomponents doing manual JSON shape-sniffing, e.g. `LiveBuildStreamModal`'s `handleSSEMessage` at `web/src/views/AppsView.tsx:1007-1029`), an uncaught render-time exception in any one view (e.g. a `TypeError` from an unexpected null field returned by a backend endpoint under active development) white-screens the entire dashboard rather than degrading gracefully.
- Remediation: add a top-level `ErrorBoundary` in `main.tsx`/`App.tsx` (and optionally a per-view boundary so one broken tab doesn't take down navigation/logout).

### 4. `AppLogsViewer` renders synthetic, hardcoded log lines while presenting itself as a live stream (Moderate/correctness, ~7.5)
- `web/src/views/AppsView.tsx:1521-1580`: the component's initial state is `Array.from({ length: 200 }, (_, i) => ...)`  — 200 fabricated log lines with fake timestamps and a hardcoded "latency: 1.2ms" — and the state is a plain `useState` with no setter ever called from a live source (no SSE hook is wired up here, unlike `LiveBuildStreamModal` which does use `useSSE`). The UI nonetheless displays a pulsing `<span>Live</span>` badge (`web/src/views/AppsView.tsx:1542-1545`).
- This is a functional gap, not just cosmetic: an operator using this tab to debug a running app sees plausible-looking but entirely fake log output and may believe they are observing real container logs. Given this is a PaaS ops console, presenting fabricated telemetry as live data is a correctness/trust issue worth flagging even though it's clearly a stubbed placeholder from active development.
- Remediation: either wire `AppLogsViewer` to the same `useSSE`/log-stream endpoint pattern used by `LiveBuildStreamModal`, or clearly label it "Demo/Preview data" until wired up, so operators don't mistake mock output for real signal.

### 5. Single monolithic JS bundle, no route-based code splitting (Moderate/performance, ~8.0)
- `web/dist/assets/index-BI-xMCn-.js` is 1.18 MB (uncompressed) for a 12-view dashboard; `web/dist/assets/index-GaIUHsyY.css` is 40 KB.
- `web/src/App.tsx:9-20` statically imports all 12 views (`LoginView`, `DashboardView`, `MarketplaceView`, `AppsView`, `StacksView`, `NodesView`, `DatabasesView`, `IngressView`, `BackupsView`, `RegistryView`, `SystemView`, `SettingsView`) — none behind `React.lazy`/`Suspense` (confirmed zero matches for both across `web/src/`).
- `web/vite.config.ts:26-31` raises `chunkSizeWarningLimit` to 1000 instead of splitting — i.e., the oversized-bundle warning was suppressed rather than addressed. Heavy libraries (`@xterm/xterm`, `recharts`, `@tanstack/react-virtual`, `ansi_up`) are only needed by 2-3 of the 12 views (terminal/build-log/dashboard-chart views) but ship to every user on first load, including someone who only ever opens `SettingsView`.
- Remediation: convert the view imports in `App.tsx` to `React.lazy(() => import('./views/X'))` behind a `Suspense` boundary keyed by `currentView`, and let Vite's default chunking split `xterm`/`recharts`/`ansi_up` into per-view chunks.

---

## Additional Observations (Minor, 8.5–9.4 band, not in top 5)

- **Token storage in `localStorage`** (`web/src/lib/api.ts:45-58`, `TOKEN_KEY = 'pikpik_token'`): standard XSS-exfiltration tradeoff vs. httpOnly cookies. Given finding #1 already routes the token through URL query strings for SSE/WS, an httpOnly-cookie-based session (with CSRF token for state-changing REST calls) would close both gaps at once, but is a larger architectural change than this pass's scope.
- **`ansi_up` XSS surface is actually fine**: `web/src/views/AppsView.tsx:1194` and `:1571` use `dangerouslySetInnerHTML={{ __html: ansi.ansi_to_html(line) }}` on build logs / mock log lines. Verified in `web/node_modules/ansi_up/ansi_up.js` (v6.0.6) that `ansi_to_html` HTML-escapes `&`, `<`, `>` before emitting markup — so this is not an active XSS vector against current build-log content. Flagging only because `dangerouslySetInnerHTML` sinks should be re-verified if `ansi_up` is ever upgraded or replaced.
- **`usePTY` and `useSSE` cleanup is actually solid**: both hooks correctly close `EventSource`/`WebSocket` in `useEffect` cleanup (`web/src/hooks/useSSE.ts:115-118`, `web/src/hooks/usePTY.ts:224-229`), clear pending reconnect timers, and `usePTY`'s terminal-lifecycle effect (`web/src/hooks/usePTY.ts:143-221`) properly disposes `xterm` data/resize listeners and calls `term.dispose()`. No leak found here — noted as a positive control, not a defect.
- **Mutation error handling is consistently wired**: nearly every `useMutation` call site in the views sets `onError` with a toast (e.g. `web/src/views/AppsView.tsx:83-142`), so React Query already surfaces most API failures to the user; the gap is specifically the handful of direct `await api.x.y()` calls outside React Query (see finding #2).
- **`api.ts`/`types.ts` spot-check against backend contracts**: sampled `App`, `Build`, `BackupSchedule`, `TrafficSplitConfig` — field names and optionality look consistent with typical Go JSON tags (snake_case matches), though a few fields carry redundant/legacy aliases (`BackupSchedule.database_type` vs `engine`, `cron_expression` vs `cron_expr` at `web/src/lib/types.ts:230-244`) suggesting a backend rename mid-flight with both old and new field names kept client-side "just in case." Full cross-check against `pkg/api/types.go` is explicitly out of scope for this pass (Seam auditor's job) — flagging only as a smell worth reconciling.
- **`cmd/pikpik/static.go` SPA server looks solid**: uses `path.Clean` + `fs.Sub`/`http.FS` (embed.FS is immune to path traversal since it has no `..` semantics beyond what `fs.FS` allows), correctly excludes `/api/`, `/ws`, `/healthz` from the HTML fallback (returns JSON 404 instead of leaking `index.html`), sets `immutable` cache headers only for hashed `/assets/` paths, and forces `no-cache` on `index.html` itself so SPA updates propagate. No issues found here.
- **No `React.StrictMode`-related double-fetch bugs observed**: `main.tsx:20` wraps in `StrictMode`; hooks use refs (`onMessageRef`, `onExitRef`) to avoid stale-closure re-subscription churn, which also happens to make StrictMode's double-invoke behavior safe.
- **Minor missing memoization**: outside `MarketplaceView.tsx:146` (which does use `useMemo` for template filtering), none of the other list-heavy views (`BackupsView`, `DatabasesView`, `RegistryView`, `NodesView`, `IngressView`) memoize their `.map()` render lists or derived filter/sort results. Given typical list sizes for a self-hosted PaaS (tens, not thousands, of apps/nodes/backups), this is cosmetic today but would matter if any of these lists grow large — not worth a dedicated top-5 slot.
- **Client-side secret generation is done correctly**: `generateSecureToken()` in `web/src/views/MarketplaceView.tsx:34-52` uses `window.crypto.getRandomValues`, not `Math.random()`, for auto-generated template secrets — correct choice, flagged as a positive control.

---

## Actionable Remediations (priority order)

1. **(Security)** Replace token-in-query-string for SSE/WS (`useSSE.ts`, `usePTY.ts`) with short-lived, single-use connection tickets minted server-side; stop mixing `withCredentials` cookie auth with URL-token auth on the same EventSource.
2. **(Robustness)** Guard `response.json()` in `api.ts:97` against non-JSON bodies; fix `App.tsx`'s `handleLogout` to always clear `currentUser` via `finally`, not just on success.
3. **(Robustness)** Add a top-level `ErrorBoundary` around `<App />` in `main.tsx` so a render-time exception in one view doesn't white-screen the whole dashboard.
4. **(Correctness)** Wire `AppLogsViewer` (`AppsView.tsx:1521`) to a real log-stream endpoint via `useSSE`, or relabel it clearly as demo/placeholder data — it currently misrepresents fabricated data as "Live."
5. **(Performance)** Convert the 12 view imports in `App.tsx` to `React.lazy` + `Suspense`; let Vite code-split `xterm`, `recharts`, `ansi_up`, and `@tanstack/react-virtual` per-view instead of shipping them all in one 1.18 MB bundle.
6. **(Follow-up, lower priority)** Reconcile the duplicate/legacy field aliases in `types.ts` (`BackupSchedule.database_type`/`engine`, `cron_expression`/`cron_expr`) with the actual backend DTOs — hand off to the Seam auditor for the authoritative cross-check.

---

## Verification

- Confirmed no `ErrorBoundary`/`componentDidCatch` in `web/src/` via full-tree grep (zero matches).
- Confirmed `dangerouslySetInnerHTML` usage is limited to two call sites (`AppsView.tsx:1194`, `:1571`), both fed through `ansi_up`'s `ansi_to_html`, and verified `ansi_up@6.0.6`'s source escapes `&`/`<`/`>` before returning HTML — not an active injection vector today.
- Confirmed `useSSE`/`usePTY` both correctly tear down `EventSource`/`WebSocket` and timers in `useEffect` cleanup — no leak found (documented as a positive control, not a gap).
- Confirmed zero `React.lazy`/`Suspense` usage repo-wide and inspected `web/dist/assets/` sizes directly (1.18 MB JS / 40 KB CSS) plus `vite.config.ts`'s raised `chunkSizeWarningLimit`.
- Confirmed no hardcoded secrets/API keys or `import.meta.env`/`VITE_`-prefixed variables anywhere in `web/src/` (would indicate build-time secret leakage into the client bundle) — none found.
- Read `cmd/pikpik/static.go` in full; SPA-fallback and caching logic verified line-by-line, no path-traversal or route-shadowing issue found for `/api/`, `/ws`, `/healthz`.

**Deferred to other scopes:** full field-by-field cross-check of `web/src/lib/types.ts` against `pkg/api/types.go` (Seam auditor); backend-side rate limiting/CORS/session model for the token-in-URL SSE/WS pattern (Backend/API auditor).
