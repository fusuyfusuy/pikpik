# Project: pikpik — Self-Hosted PaaS Control Plane

## Architecture
`pikpik` is a minimalist, high-reliability open-source self-hosted PaaS control plane engineered as a lean, zero-bloat alternative to Dokploy and Coolify.
It compiles into 3 standalone Go binaries and an embedded React SPA:
- `pikpik` (`cmd/pikpik`): Unified control plane server (~14–17MB). SQLite WAL, REST API, WebSocket hub, Caddy reconciler, Backup scheduler, Telemetry aggregator.
- `pikpik-agent` (`cmd/pikpik-agent`): Lightweight worker agent (~6.2–10MB). `/proc` metrics exporter + Outbound mTLS/WSS Docker Socket proxy.
- `pikpik-cli` (`cmd/pikpik-cli`): Operator terminal CLI (~11MB). POSIX flags, human/JSON output formats, PTY terminal attachment.
- `web/`: React 18/19 SPA frontend with Tailwind CSS, TanStack Query, Xterm.js PTY, and Recharts.

### The 4 Non-Negotiable Invariants
1. **Invariant 1 (Zero Shelling)**: All container lifecycles, network attachments, exec PTYs, builds, and metrics communicate directly through typed Docker SDK (`/var/run/docker.sock`). Zero `exec.Command("sh", "-c", ...)`.
2. **Invariant 2 (Single Unified Runtime)**: Single static Go binary with embedded SQLite WAL (`PRAGMA busy_timeout=5000`, `PRAGMA foreign_keys=ON`). Zero external Redis/Postgres daemons.
3. **Invariant 3 (Dynamic API Ingress)**: Dynamic in-memory Caddy Admin REST API mutations (`http://127.0.0.1:2019/load`) in <15ms. Zero Caddyfiles on disk.
4. **Invariant 4 (Pure Streaming Pipelines)**: Streaming dataflows (`io.Pipe` -> gzip -> S3 multipart; `StdCopy` -> WebSocket/SSE). Peak RAM <32MB with zero temporary staging files on `/tmp`.

---

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Standalone App Deployments | Containerized application creation, rolling update, start/stop/restart/delete | M1/M2 | Survey |
| 2 | Start-Before-Stop Rolling Updates | Zero-downtime rolling deploys with 250ms probation health check | M1/M2 | Survey |
| 3 | Docker Compose / Stack Management | In-memory Compose parsing, DAG topological sort (Kahn's), container orchestration | M1/M2 | Survey |
| 4 | Docker Swarm Multi-Node Workloads | Swarm cluster init, join, service creation, replicas, rolling updates | M1/M2 | Survey |
| 5 | Multi-Database Engines | Automated provisioning of Postgres, MySQL, MariaDB, Redis, MongoDB | M1/M2 | Survey |
| 6 | Database User/DB Provisioning | Dynamic database and credential initialization via Docker SDK | M1/M2 | Survey |
| 7 | Application Marketplace Templates | 25+ one-click preconfigured application templates | M1/M2 | Survey |
| 8 | Multi-Source CI/CD Git Cloner | Public & private Git repo cloning, branch tracking, commit metadata | M1/M2 | Survey |
| 9 | Multi-Builder Pipelines | Dockerfile builds, Dockerfile fallbacks, and Nixpacks builder support | M1/M2 | Survey |
| 10 | Real-Time Build Streaming | Real-time ANSI build log multiplexing over SSE & WebSockets | M1/M2 | Survey |
| 11 | Dynamic In-Memory Caddy Ingress | Dynamic sub-15ms route configuration via Caddy Admin REST API | M1/M2 | Survey |
| 12 | Automated ACME TLS & On-Demand TLS | Let's Encrypt / ZeroSSL with `/ask` SQLite domain whitelist validation | M1/M2 | Survey |
| 13 | Custom Domains & Port Mappings | Path routing, wildcard domain bindings, and host port publishing | M1/M2 | Survey |
| 14 | Weighted Canary & Blue-Green Ingress | Dynamic traffic splitting (0-100%) across deployment versions | M1/M2 | Survey |
| 15 | Managed Volumes | Volume creation, inspect, pruning, and bind-mount attachment | M1/M2 | Survey |
| 16 | User-Defined Managed Networks | Overlay & bridge network provisioning with isolation rules | M1/M2 | Survey |
| 17 | Zero-Disk S3 Streaming Backups | Multi-DB streaming dump (`io.Pipe` -> gzip -> S3 multipart, <32MB RAM) | M1/M2 | Survey |
| 18 | Cron-Based Backup Schedules | Configurable cron schedules with retention pruning | M1/M2 | Survey |
| 19 | S3 Point-in-Time Restore Engine | Streaming S3 multipart download -> decompression -> DB restore | M1/M2 | Survey |
| 20 | Multi-Node Agent / Remote Workers | `pikpik-agent` mTLS/WSS outbound Docker socket proxying | M1/M2 | Survey |
| 21 | Private Docker Container Registries | ECR, Docker Hub, GHCR, self-hosted registry credential caching | M1/M2 | Survey |
| 22 | Host & Container `/proc` Telemetry | Real-time CPU, RAM, Disk, Network scrapers without shelling | M1/M2 | Survey |
| 23 | In-Memory Telemetry Ring Buffers | Fixed-capacity 8640-point ring buffer for 24h metrics history | M1/M2 | Survey |
| 24 | Interactive Container Exec & Host PTY | Full-duplex raw terminal sessions over WebSocket with binary framing | M1/M2 | Survey |
| 25 | 4-Tier Environment Variable Hierarchy | Global -> Project -> Service -> Deployment inheritance with AES-256-GCM | M1/M2 | Survey |
| 26 | Multi-Tenancy Organizations & Projects | Isolation by Org (`org_`) and Project (`prj_`) with optional stages | M1/M2 | Survey |
| 27 | 4-Level RBAC & Scoped API Tokens | Viewer, Developer, Admin, Owner roles with versioned session revocation | M1/M2 | Survey |
| 28 | Standalone POSIX Operator CLI | `pikpik-cli` with 22 subcommands, context switching, table/JSON formatters | M1/M2 | Survey |
| 29 | React 18/19 SPA Web Dashboard | Clean responsive Tailwind UI, TanStack Query, Xterm.js, Recharts | M1/M2 | Survey |
| 30 | Canary Traffic Weight UI Clothing | Visual slider and API binding in Web SPA for live Canary weighting | M1/M2 | Survey |

---

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | UI Clothing & CLI Verification | Frontend Canary slider & API methods in `web/`, CLI verification | none | DONE |
| M2 | E2E Test Suite Pass (Tiers 1-4) | Requirement-driven opaque-box test suite validation (Tiers 1-4) | M1, TEST_READY | DONE |
| M3 | Adversarial Coverage Hardening (Tier 5) & Parity Publication | White-box adversarial testing, 0-race audit, final parity matrix | M2 | DONE |
| M4 | Dokploy Parity Audit & Chaos Matrix (R1-R5) | Full Dokploy architectural comparison, 5-dimension chaos test matrix, <32MB streaming memory ceiling | M3 | DONE |

---

## Interface Contracts
### `web/src/lib/api.ts` ↔ `pkg/api/routes.go`
- `GET /api/v1/apps/{id}/traffic` -> `TrafficSplitResponse { app_id, domain, splits: UpstreamWeight[] }`
- `POST /api/v1/apps/{id}/traffic` -> Payload `SetTrafficSplitRequest`, updates dynamic Caddy ingress in-memory upstream weights in <15ms.

### `cmd/pikpik-cli` ↔ `pkg/api/`
- `pikpik-cli app traffic <app-id> --upstream <host:port=weight> [--reset]` -> Updates upstream weights over REST API.

---

## Code Layout
- `cmd/pikpik`: Unified server entrypoint
- `cmd/pikpik-cli`: Operator CLI entrypoint and client
- `cmd/pikpik-agent`: Worker agent daemon entrypoint
- `pkg/store`: SQLite schema migrations, models, repository interfaces
- `pkg/api`: HTTP REST handlers, WebSocket hub, RBAC middleware, DTOs
- `pkg/orchestration`: Docker SDK container lifecycles, Swarm, Compose, logs demux
- `pkg/ingress`: Dynamic Caddy Admin REST API client, traffic split, `/ask` TLS
- `pkg/backup`: Multi-database pure streaming S3 backup and restore engine
- `pkg/telemetry`: `/proc` metrics scraper, in-memory ring buffers
- `web/`: React 18/19 SPA, Tailwind, TanStack Query, Xterm.js PTY, Recharts
- `reviews/`: Parity review matrices and architectural audit reports
