# Scope 4 Audit: REST & WebSocket API Gateway & Server

**Date:** 2026-08-31
**Component:** `pikpik` API Gateway (`pkg/api`, `cmd/pikpik`)
**Health Score:** 8.8/10 (Exemplary foundation with a few moderate security/robustness findings)
**Invariant Breaches:** 0 Strict Breaches

---

## 1. Executive Summary

The `pikpik` control plane is a highly robust, well-engineered Go application that strictly adheres to its invariants. The unified runtime model (Invariant 2) is perfectly executed via embedded SQLite WAL configurations and in-memory background workers without external dependencies. The HTTP/WS layers enforce memory limits, handle panic recovery natively, and cleanly multiplex streams.

However, a subtle middleware layering issue exposes the database to potential DoS attacks via brute-force token guessing, and broad fallback behaviors for token extraction increase credential leakage risks.

### Top Findings
1. **[Moderate] Auth DB Exhaustion (DoS Vector):** In `pkg/api/routes.go`, the global `rateLimiter` is nested *inside* the `AuthMiddleware` handler wrapper. Malicious clients sending invalid tokens are rejected by the database lookup inside `AuthMiddleware` before the rate limiter ever sees the request. 
2. **[Minor] Host PTY Subprocess Execution:** `pkg/api/pty.go` spawns direct host processes (`exec.CommandContext`) for the `host_machine` target. While explicitly gated by `RoleAdmin`/`RoleOwner` and not technically a shell interpolation vulnerability, it brushes against the spirit of Invariant 1.
3. **[Minor] Universal Query Parameter Tokens:** `pkg/api/auth.go#ExtractToken` allows `?token=` for *all* endpoints. While required for WebSockets/SSE, allowing it universally risks credential leakage in server logs.
4. **[Minor] Unbounded WebSocket Session Expiration:** PTY and SSE handlers only authorize on the initial HTTP upgrade. If a token or session is revoked, existing WebSocket connections remain active indefinitely.

---

## 2. Invariant 2 (Single Unified Runtime) & Architecture
**Status: Exemplary (10/10)**

The initialization sequence in `cmd/pikpik/main.go` correctly bootstraps a unified server runtime. 
- **SQLite WAL & Pragmas**: `pkg/store/sqlite.go` expertly configures the database connection pool. By encoding pragmas (`_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)`) directly into the DSN, the engine ensures that all transient database/sql connections receive the required tuning parameters.
- **Embedded Background Workers**: The API Gateway coordinates background tasks like the `WebSocketHub` seamlessly via `gw.RunBackground(ctx)`, eliminating any need for Redis Pub/Sub.

---

## 3. Correctness & Robustness
**Status: Strong (9.0/10)**

- **Panic Recovery**: `pkg/api/gateway.go` wraps the multiplexer in a robust `defer func() { if rec := recover() ... }` block that outputs RFC-compliant 500 JSON envelopes.
- **Memory Bounding**: `gateway.go` enforces a strict 10MB limit on all incoming REST requests via `http.MaxBytesReader(w, r.Body, 10*1024*1024)`, neutralizing memory exhaustion attacks via oversized JSON payloads.
- **Error Formatting**: `pkg/api/routes.go` (`WriteError`, `WriteJSON`) strictly adheres to RFC 7807 problem details specifications.
- **WebSocket PTY Multiplexing**: `pkg/api/pty.go` implements a clean binary protocol for container terminals (0x00 stdin, 0x01 resize, 0x02 SIGINT).

---

## 4. Performance
**Status: Exemplary (9.5/10)**

- **Goroutine Lifecycle Management**: Both `ws_hub.go` and `sse.go` handle client disconnects cleanly. Broadcasters use `select` blocks with `default:` drop cases to gracefully discard frames for slow consumers, entirely preventing deadlocks and memory leaks in the primary broadcast loops.
- **Rate Limiting**: `pkg/api/ratelimit.go` implements a highly efficient sliding-window limiter with background TTL cleanup, avoiding unbounded map growth.

---

## 5. Security & RBAC
**Status: Moderate (7.5/10)**

While the core RBAC logic (`IsRoleAllowed`) properly enforces the hierarchical permissions (`Viewer` -> `Developer` -> `Admin` -> `Owner`), several implementation boundaries need tightening:

### 5.1 The Middleware Layering Flaw (routes.go)
```go
// routes.go
authWrap := func(role string, h http.HandlerFunc) http.Handler {
    mw := AuthMiddleware(authSvc, st, role)
    return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if rateLimiter != nil {
            // ... rate limiting logic ...
        }
        h(w, r)
    }))
}
```
**Issue**: `AuthMiddleware` runs first, performing database queries (`st.Sessions().GetByID` or `st.Users().GetByID`). If an attacker blasts the API with randomly generated tokens, the requests will 401 early *before* hitting the rate limiter inside the wrapped function.
**Remediation**: Hoist the rate limiter to execute *before* or *alongside* `AuthMiddleware`.

### 5.2 IP Spoofing Prevention
**Exemplary Detail**: `ExtractClientIP` in `auth.go` explicitly ignores `X-Forwarded-For` and `X-Real-IP`. As documented in the codebase, blindly trusting these headers without an established reverse-proxy topology allows attackers to mint fresh rate-limit buckets. This is a superb security design choice.

### 5.3 URL Query Token Leakage
```go
// auth.go
// 4. Query param fallback
if tok := r.URL.Query().Get("token"); tok != "" {
    return tok
}
```
**Issue**: Accepting tokens in the query string is necessary for `EventSource` (SSE) and native WebSockets, but applying this globally exposes REST API tokens to browser history, proxy logs, and `Referer` header leaks.
**Remediation**: Conditionally accept `?token=` only if `r.URL.Path` begins with `/ws/` or ends with `/stream`.

---

## Actionable Remediations (Checklist)

- [ ] **routes.go**: Refactor `authWrap` to evaluate `rateLimiter.Allow()` *before* invoking `AuthMiddleware`.
- [ ] **auth.go**: Restrict query parameter token extraction to specific WebSocket/SSE route paths.
- [ ] **ws_hub.go / sse.go**: Implement an active session re-validation check in the broadcast ticker loop (e.g., query the DB every 5 minutes to verify the underlying API Token or Session hasn't been revoked).
