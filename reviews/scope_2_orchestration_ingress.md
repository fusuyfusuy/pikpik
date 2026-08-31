# Scope 2 Audit Report: Orchestration, Dynamic Ingress & Builder

## Executive Summary

- **Health Score**: 8.2 / 10.0 (Moderate)
- **Invariant Breaches**: Minor architectural breaches related to Builder dependencies.
- **Top Findings**:
  1. **[Critical Security] Volume Mount Directory Escape**: Missing validation on bind mount sources in Compose parsing and Docker container deployment.
  2. **[Minor] Invariant 1 Deviation (Builder & Git)**: Relying on local executable binary calls for `git` and `nixpacks` rather than pure Docker containerized pipelines.
- **Actionable Remediations**:
  - Implement a `SanitizeMountSource` function in `pkg/orchestration/compose.go` or `containers.go` to reject `/proc`, `/sys`, `/etc`, and `/` bind mounts.
  - Containerize the `nixpacks` build and `git clone` steps to run via the Docker SDK API rather than shelling out to host binaries, strictly upholding Invariant 1.

## 1. Invariant 1 (Zero Shelling) & Builder Architecture

**Finding**: `pkg/build/nixpacks.go` (Line 123) and `pkg/git/clone.go` (Lines 145+) use `exec.CommandContext` to call local binaries (`nixpacks` and `git`).
**Analysis**: While they strictly avoid `sh -c` string interpolation (passing typed arguments and preventing command injection), this breaches the strict architectural rule: _"All container lifecycles, network attachments, exec PTYs, builds, and metrics MUST communicate directly through the typed Docker SDK."_ Running builds on the host leaks state and risks host environment contamination.
**Recommendation**: Move Git cloning and Nixpacks building into a short-lived Docker container managed via the Docker SDK.

## 2. Invariant 3 (Dynamic API Ingress)

**Finding**: `pkg/ingress/client.go` properly wraps the Caddy Admin REST API.
**Analysis**: The implementation successfully achieves the <15ms mutation goal using `PUT /id/{id}` and `POST /load`. No Caddyfile generation or `kill -HUP` signals exist. The client configures a reasonable timeout (3s) and connection pooling (`MaxIdleConns: 50`).
**Recommendation**: Exemplary implementation. No changes needed.

## 3. Correctness

**Finding**: `pkg/orchestration/compose.go` (`ResolveDeploymentOrder`) correctly parses Compose YAMLs and implements Kahn's Algorithm for topological sorting.
**Analysis**: The dependency graph solver ensures deterministic ordering and properly detects cyclic dependencies. The rolling update mechanism (`DeployStack`) tracks new vs old containers and supports robust teardown on failure.
**Recommendation**: Code is robust and operates correctly.

## 4. Robustness

**Finding**: Orchestration rollback procedures in `pkg/orchestration/compose.go` correctly clean up intermediary containers, volumes, and networks on failure. 
**Analysis**: Docker socket connections leverage the official typed client, keeping retry loops tight. Container crash recovery is deferred to Docker's native `RestartPolicy`.
**Recommendation**: Maintain current rollback implementation.

## 5. Performance

**Finding**: Low-allocation routines inside Caddy JSON configuration construction.
**Analysis**: Concurrency is managed via context cancellations and timeout boundaries. Zero-blocking IO loops in orchestrator.
**Recommendation**: Exemplary implementation.

## 6. Security

**Finding 1 (Exemplary)**: `pkg/git/webhook.go` implements HMAC validation correctly using `subtle.ConstantTimeCompare`, thwarting timing side-channels. SSRF defense via `validateHostSSRF` protects local bounds during `git clone`.
**Finding 2 (Critical)**: Volume mount directory escape vulnerability in `pkg/orchestration/compose.go` and `pkg/orchestration/containers.go`.
**Analysis**: The `ParseComposeYAML` function blindly maps volume sources:
```go
if strings.HasPrefix(parts[0], "/") || strings.HasPrefix(parts[0], ".") {
    mType = "bind"
}
```
In `containers.go`, bind mounts are passed directly to the Engine:
```go
if m.Type == "bind" { mountType = mount.TypeBind }
mounts = append(mounts, mount.Mount{ Source: m.Source })
```
A malicious tenant can submit a stack with `volumes: ["/proc:/host_proc"]` leading to complete host takeover.
**Recommendation**: Implement a strict allowlist or blocklist for bind mounts, rejecting `/proc`, `/sys`, `/etc`, `/var/run/docker.sock`, and `/` roots unless explicitly authorized via a superadmin override flag.
