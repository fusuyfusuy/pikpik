# Scope 5 Audit Report: Operator CLI & Web SPA Frontend

## Executive Summary
**Health Score:** 8.8 / 10.0 (Minor Issues)

### Invariant Breaches
None detected. The CLI uses `os.Exit(1)` appropriately on failures, preventing false successes. Direct PTY/SSE connections to the backend rely on the WebSocket/SSE APIs accurately, maintaining the invariant of unified streaming and direct integrations. 

### Top Findings & Remediations
1. **CLI Secret Exposure Potential**:
   - *Location*: `cmd/pikpik-cli/main.go:606`
   - *Observation*: Manager/Worker Join tokens are printed to `os.Stdout` without strict checking of `--show-secrets`.
   - *Remediation*: Ensure token output commands explicitly require a `--show-secrets` flag.

2. **Web SPA Route Caching Invalidation**:
   - *Location*: `web/src/views/AppsView.tsx:164-224`
   - *Observation*: TanStack Query cache invalidations are used actively during mutations, preventing stale views.

3. **Web SPA WebSocket PTY Hook `usePTY` Error Handling**:
   - *Location*: `web/src/hooks/usePTY.ts:79`
   - *Observation*: Connect logic relies on `getToken()` but doesn't handle token expiration gracefully across the PTY lifecycle.
   - *Remediation*: Include reconnect logic with token refresh or explicitly trigger a re-auth toast on 401/403 WebSocket closes.

4. **CLI Config Persistence**:
   - *Location*: `cmd/pikpik-cli/config.go:82`
   - *Observation*: `ConfigManager.Save` uses atomic `tmpPath` and `os.Rename`, satisfying robustness requirements.

## Review Dimensions

### 1. Correctness
- **CLI**: Implements comprehensive subcommands for standard operations. Fails safely with `os.Exit(1)`.
- **Web UI**: Uses hash-based routing. Uses TanStack query effectively to invalidate caches after mutations (`queryClient.invalidateQueries`).

### 2. Robustness
- **CLI**: Configuration writes are atomic.
- **Web UI**: `useSSE` hook includes robust exponential backoff and reconnection logic (`maxReconnectAttempts=10`). PTY doesn't auto-reconnect, but correctly triggers disconnected state.

### 3. Performance
- **CLI**: Lightweight parsing with standard `os.Args` and `flag` package logic. Minimal startup delay.
- **Web UI**: SSE uses standard Fetch stream processing and manual buffer updates for logs.

### 4. Security
- **CLI**: Stores token in `~/.pikpik/config.json` with 0600 permissions.
- **Web UI**: Uses bearer tokens correctly in SSE/WS handshakes using Subprotocols (`pikpik-auth.<token>`).

## Actionable Next Steps
- Implement `--show-secrets` requirement on CLI output printing tokens.
- Add PTY auto-reconnect or clear auth-error signaling on WebSocket drops.
