# 09. Architectural Decision Records (ADRs)

This document records the foundational architecture decisions, trade-offs evaluated, alternatives rejected, and long-term consequences for the next-generation PaaS.

---

## ADR-001: Direct Docker Engine Socket API vs CLI Shelling

### Context
Legacy PaaS tools (Dokploy, CapRover, Coolify) construct string commands (`"docker run -d " + appName`) and execute them via `exec()`. During audits, this was proven to be the #1 cause of Remote Code Execution (RCE) and command injection vulnerabilities.

### Decision
Strictly forbid shelling out to `sh -c` or `docker` CLI for container runtime operations. All interactions with containers, images, volumes, networks, and stats must use the direct Unix domain socket API (`/var/run/docker.sock`) via typed SDK structs.

### Alternatives Considered
- *Shell-Quote Sanitization*: Wrapping every interpolated parameter with `shell-quote.quote()`. Rejected: Human error will inevitably cause unquoted variables in future PRs; does not solve zombie processes or streaming memory bloat.

### Consequences
- **Positive**: 100% immunity to shell injection; typed error responses; native context cancellation and timeouts; zero intermediate subprocess overhead.
- **Negative**: Requires implementing binary Docker multiplexed stream decoders for stdout/stderr multiplexing.

---

## ADR-002: Caddy Dynamic Admin API vs Traefik/Nginx File Generation

### Context
Writing YAML/JSON config files to disk for Traefik or Nginx introduces filesystem race conditions, config syntax reload crashes, and slow reload propagation (1–3 seconds).

### Decision
Adopt **Caddy** as the edge reverse proxy, controlling routes dynamically via its JSON REST Admin API (`http://127.0.0.1:2019/config/...`).

### Alternatives Considered
- *Traefik Dynamic File Provider*: Rejected due to file watching race conditions and config parsing crash risks.
- *Nginx with Lua / OpenResty*: Rejected due to complex dynamic ACME certificate handling.

### Consequences
- **Positive**: Atomic, in-memory route updates in `<15ms`; zero-downtime Let's Encrypt / ZeroSSL TLS; invalid configs return HTTP 400 without breaking active traffic.
- **Negative**: Control plane must maintain an internal domain-to-route state store to push full route reconciliations upon Caddy restart.

---

## ADR-003: Embedded SQLite (WAL) + Litestream vs Standalone PostgreSQL

### Context
Running an external PostgreSQL container for a single-node PaaS control plane adds ~150MB baseline RAM usage, connection pool tuning, and circular boot dependencies.

### Decision
Use **SQLite** with Write-Ahead Logging (`WAL` mode) as the primary state store, continuously replicated to S3 / Cloudflare R2 via **Litestream**.

### Alternatives Considered
- *PostgreSQL Database Container*: Maintained as an optional adapter for large multi-node distributed deployments, but SQLite is the default for single-node sovereign installs.

### Consequences
- **Positive**: Zero configuration, zero startup latency, zero memory overhead, 100% atomic transactions, sub-millisecond local reads, continuous disaster recovery to S3.
- **Negative**: Single-writer constraint (well within requirements for a PaaS control plane averaging <50 writes/second).

---

## ADR-004: In-Process DB-Backed Task Queue vs External Redis + BullMQ

### Context
Dokploy relies on BullMQ + Redis for background schedules and backups. This adds an external Redis daemon dependency, connection recovery bugs (`maxRetriesPerRequest`), and worker queue configuration drifts.

### Decision
Implement an embedded, database-backed transactional task queue (or in-memory worker pool with persistent DB job states).

### Alternatives Considered
- *Redis + BullMQ*: Rejected due to operational complexity, additional memory footprint, and non-transactional job dispatch (jobs can be enqueued while database transaction rolls back).

### Consequences
- **Positive**: Eliminates Redis dependency; transactions are atomic (job is only dispatched if database record commits); zero inter-service network flakiness.
- **Negative**: Slightly higher database query polling overhead (mitigated by lightweight index polling and listen/notify mechanisms).

---

## ADR-005: Single Unified Runtime vs Polyglot Micro-Daemons

### Context
Dokploy split its responsibilities into Next.js + Hono API + Go monitoring daemon + Node BullMQ worker + Redis + PostgreSQL + SQLite. The Go daemon became disconnected from main server health scraping, and workers suffered from duplicate queue subscriptions.

### Decision
Consolidate all control plane responsibilities (API gateway, WebSocket hub, background scheduler, telemetry collector, and static UI delivery) into a **single runtime service** (Go binary or Node.js/Bun Fastify service).

### Alternatives Considered
- *Microservices Architecture*: Rejected as premature over-engineering for a PaaS control plane whose primary duty is managing external user containers.

### Consequences
- **Positive**: Massive reduction in cold start time (<800ms vs >30s); memory consumption drops from ~1.2GB to <120MB; zero contract drifts or orphaned daemons.
- **Negative**: Core engineering discipline required to maintain strict internal module boundaries within the single codebase.

---

## ADR-006: Pure Piped Streaming S3 Backups vs Intermediate Disk Buffer Extraction

### Context
Legacy backup implementations dump database archives to `/tmp` on disk before uploading to S3, causing out-of-disk `ENOSPC` host crashes during large database restores or backups.

### Decision
Mandate **Pure Piped Streaming**: `db_dump_stream | compression_stream | s3_multipart_upload` and `s3_download_stream | decompression_stream | db_restore_stdin`.

### Alternatives Considered
- *Disk-Buffered Dumps with Cleanups*: Rejected because disk space cannot be guaranteed on memory/disk-constrained VPS hosts.

### Consequences
- **Positive**: Zero temporary disk space requirement; constant `<32MB` memory footprint regardless of whether the database is 100 MB or 100 GB.
- **Negative**: Requires streaming archive formats (`pg_dump -Fc`, `mongodump --archive`) that support sequential non-seekable stdio pipes.

---

## ADR-007: BuildKit Secret Mounts vs Docker Build Args

### Context
Passing secrets as Docker `ARG` or `ENV` bakes credentials into the permanent image metadata layers, leading to secret exfiltration.

### Decision
Strictly enforce **BuildKit Secret Mounts** (`--secret id=...` and `RUN --mount=type=secret`) for all build pipelines requiring private Git tokens, npm tokens, or API credentials.

### Alternatives Considered
- *Multistage Build Scratch Stripping*: Rejected because intermediate builder layer caches often retain credentials on the host daemon.

### Consequences
- **Positive**: Secrets exist strictly in ephemeral RAM mounts during the compilation step and are never committed to image layer metadata.
- **Negative**: Requires BuildKit to be enabled on the host Docker daemon (default in modern Docker Engine).
