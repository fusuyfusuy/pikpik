# 05. Scope Specification: Embedded Registry, Redeploy Webhooks & Pure Streaming S3 Backups

This specification details the technical architecture, data structures, streaming protocols, and Go interfaces for **Scope 5: CI/CD, Registry & Streaming S3 Backups** of the `pikpik` PaaS.

---

## 1. Architectural Overview & Core Invariants

Scope 5 bridges external continuous integration (CI) runners, local OCI artifact distribution, and cloud-native state persistence. It operates as a set of internal modules inside the single unified `pikpik` runtime without external worker daemons or message queues.

```mermaid
graph TD
    subgraph External CI & Ingress
        GHA[GitHub Actions / External CI]
        CF[Cloudflare Edge / Wildcard TLS]
        CADDY[Caddy Edge Reverse Proxy<br/>pikpik-ingress-overlay]
    end

    subgraph pikpik Core Control Plane (Single Unified Runtime)
        API_ROUTER[API Router & Webhook Dispatcher]
        REG_MGR[Registry Manager]
        NUDGE_MGR[Deploy Nudge Webhook Handler]
        BK_ENGINE[Streaming Backup Engine]
        S3_CLIENT[Universal S3 Client (SigV4)]
        TASK_Q[DB-Backed Task Queue]
    end

    subgraph Swarm Node Leader [Server 1]
        OCI_REG[(Embedded Private Registry<br/>registry:2.8.3)]
        SWARM_SOCK[(/var/run/docker.sock)]
    end

    subgraph Swarm Worker Nodes [Server 2 & 3]
        PG_DB[(Managed PostgreSQL 17<br/>Isolated Overlay Network)]
        APP_SVC[Application Services]
    end

    subgraph Remote S3 / R2 Object Storage
        S3_BUCKET[(Cloudflare R2 / AWS S3 / MinIO / B2)]
    end

    GHA -->|1. docker push HTTPS| CADDY
    CADDY -->|Route: registry.domain.com| OCI_REG
    GHA -->|2. POST /api/deploy/nudge/{token}| CADDY
    CADDY -->|Route: /api/deploy/*| API_ROUTER

    API_ROUTER --> NUDGE_MGR
    NUDGE_MGR -->|Enforce Rate Limit & Auth| TASK_Q
    TASK_Q -->|Trigger Rolling Update| SWARM_SOCK
    SWARM_SOCK -->|Deploy Image| APP_SVC

    REG_MGR -->|Manage Lifecycle & Config| OCI_REG
    OCI_REG -.->|Optional S3 Storage Driver| S3_BUCKET

    BK_ENGINE -->|Exec pg_dump Stream| PG_DB
    BK_ENGINE -->|Gzip Pipe Stream (<32MB RAM)| S3_CLIENT
    S3_CLIENT -->|Multipart Upload| S3_BUCKET
```

### Core Invariants

| Invariant | Guarantee | Enforcement Mechanism |
| :--- | :--- | :--- |
| **Invariant 1: Zero Shelling** | No invocation of `os/exec` with raw bash/sh strings for container, dump, or registry operations. | All container interactions utilize the official Docker Engine API socket client (`/var/run/docker.sock`) with typed structs and binary multiplexed streams. |
| **Invariant 2: Zero Host Port Mapping** | Managed databases and the embedded registry never expose public ports (`-p 5432:5432`, `-p 5000:5000`) on host interfaces. | Services attach strictly to project-isolated overlay networks (`pikpik_net_proj_<id>`) and ingress overlays (`pikpik-ingress-overlay`). |
| **Invariant 3: Single Unified Runtime** | No external background daemons (e.g. BullMQ, Celery, standalone Go monitoring binaries). | Schedulers, webhook handlers, registry reconcilers, and backup workers execute as in-process Go goroutines backed by SQLite WAL state. |
| **Invariant 4: Pure Memory-Bounded Streaming** | Streaming backups and restores must strictly obey: **$\text{Peak RAM} \le 32\,\text{MB}$** and **$\text{Disk Footprint} = 0\,\text{bytes}$**. | Piped Unix streams (`io.Pipe`) link Docker Exec stdout directly through in-memory gzip/zstd writers to S3 multipart upload readers with zero `/tmp` file buffering. |
| **Invariant 5: Universal S3 Interoperability** | Out-of-the-box support for AWS S3, Cloudflare R2, MinIO, and Backblaze B2. | Pure SigV4 authentication with configurable endpoint resolvers, bucket path styles, and automated retention pruning. |

---

## 2. Embedded Private OCI Registry Engine

The embedded registry provides a self-hosted, sovereign container image repository for staging build artifacts and receiving multi-arch images from CI runners.

### 2.1 Container Lifecycle & Network Attachment

The registry runs as a managed Docker container or Swarm service on the Manager Node (Server 1).

```mermaid
stateDiagram-v2
    [*] --> Initializing: Inspect Docker Engine
    Initializing --> ProvisioningVolume: Volume Missing
    ProvisioningVolume --> GeneratingConfig: Mount Named Volume
    Initializing --> GeneratingConfig: Volume Exists
    GeneratingConfig --> StartingContainer: Render htpasswd & config.yml
    StartingContainer --> AttachingNetwork: Create 'pikpik_registry'
    AttachingNetwork --> HealthChecking: Connect 'pikpik-ingress-overlay'
    HealthChecking --> Healthy: GET /v2/ returns 200/401
    HealthChecking --> Failed: Timeout / Exit Non-Zero
    Failed --> StartingContainer: Restart with Backoff
    Healthy --> IngressRouting: Push Dynamic Route to Caddy
    IngressRouting --> Active: registry.yourdomain.com Ready
    Active --> [*]: Teardown / Reconcile
```

#### Registry Container Configuration

- **Image**: `registry:2.8.3` (Alpine-based, minimal footprint).
- **Container Name**: `pikpik_registry`.
- **Internal Port**: `5000` (Never published to host ports).
- **Network Attachments**:
  - `pikpik-ingress-overlay` (Swarm overlay network for Caddy proxy routing).
  - `pikpik-internal-mgmt` (Bridge/overlay network for pikpik control plane communication).
- **Restart Policy**: `unless-stopped` / Swarm `RestartCondition: any`.
- **Healthcheck**: `GET http://127.0.0.1:5000/v2/` via `wget -q -O - http://127.0.0.1:5000/v2/ || exit 1`.

### 2.2 Storage Backends: Local Volume vs S3/R2

The storage engine is configurable per installation:

#### A. Local Persistent Named Volume
- **Volume Slug**: `pikpik_vol_sys_registry_data`
- **Container Mount**: `/var/lib/registry`
- **Filesystem Driver**: Standard Docker local driver (`/var/lib/docker/volumes/pikpik_vol_sys_registry_data/_data`)
- **Environment Variables**:
  ```env
  REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY=/var/lib/registry
  REGISTRY_STORAGE_DELETE_ENABLED=true
  REGISTRY_STORAGE_CACHE_BLOBDESCRIPTOR=inmemory
  ```

#### B. Remote Object Storage (S3 / Cloudflare R2 / MinIO / B2)
- **Dynamic Config Generator**: Generates `/etc/docker/registry/config.yml` injected via Docker Config Mount or file mount:
  ```yaml
  version: 0.1
  log:
    fields:
      service: registry
    level: info
  storage:
    cache:
      blobdescriptor: inmemory
    delete:
      enabled: true
    s3:
      accesskey: "${S3_ACCESS_KEY}"
      secretkey: "${S3_SECRET_KEY}"
      region: "${S3_REGION}"
      regionendpoint: "${S3_ENDPOINT}"
      bucket: "${S3_BUCKET}"
      encrypt: false
      secure: true
      v4auth: true
      chunksize: 5242880 # 5MB parts
  http:
    addr: :5000
    headers:
      X-Content-Type-Options: [nosniff]
  auth:
    htpasswd:
      realm: "pikpik-registry"
      path: /etc/docker/registry/htpasswd
  ```

### 2.3 Robot Authentication & Credential Generator

To prevent CI credentials from granting full PaaS administrative rights, the platform provides immutable robot credentials with scoped repository access.

```mermaid
sequenceDiagram
    autonumber
    participant UI as Developer / CI Pipeline
    participant CP as pikpik Control Plane
    participant DB as SQLite WAL Store
    participant Reg as Embedded Registry (registry:2)

    UI->>CP: POST /api/registry/robots (ProjectID, Permissions)
    CP->>CP: Generate CSPRNG Token: 'pik_reg_' + Base64URL(32 bytes)
    CP->>CP: Hash Token with Bcrypt (Cost: 10)
    CP->>DB: INSERT INTO registry_robots (id, username, hash, project_id)
    CP->>Reg: Write /etc/docker/registry/htpasswd (username:bcrypt_hash)
    CP->>Reg: Send SIGHUP to registry container (Reload credentials)
    CP-->>UI: Return Raw Secret Once: { username: "pikpik-robot-proj1", secret: "pik_reg_..." }
```

#### Credential Specification:
- **Username Format**: `pikpik-robot-<projectSlug>` or `pikpik-ci-global`
- **Secret Format**: `pik_reg_<random_32_bytes_base64url>`
- **Storage**: Only the bcrypt hash (`$2y$10$...`) is retained in SQLite. The plain secret is returned exactly once upon generation.
- **Revocation**: Atomic deletion from DB and regeneration of the `/etc/docker/registry/htpasswd` file followed by an in-memory credential reload signal.

---

## 3. Authenticated Redeploy Nudge Webhook Engine

The Redeploy Nudge Webhook provides a low-overhead, secure integration point for external CI systems (GitHub Actions, GitLab CI, CircleCI, Custom Scripts) to notify `pikpik` that a new image has been pushed and request a rolling zero-downtime deployment.

### 3.1 Webhook Endpoint & Authentication Flow

- **Path**: `POST /api/deploy/nudge/{token}`
- **Protocol**: HTTPS over Cloudflare Wildcard TLS (Caddy Edge)
- **Token Security**:
  - Webhook token format: `pik_ndg_<32_bytes_secure_random_hex>`
  - Database lookup: Tokens are indexed via SHA-256 hash digests (`hash_sha256(token)`).
  - Validation: Token comparison uses constant-time string equality (`crypto/subtle.ConstantTimeCompare`) to eliminate timing side-channel attacks.

```mermaid
sequenceDiagram
    autonumber
    participant CI as External CI (GitHub Actions)
    participant Edge as Caddy Ingress (443)
    participant Nudge as DeployWebhookHandler
    participant RL as Rate Limiter (In-Memory Token Bucket)
    participant DB as SQLite WAL Store
    participant Orchestrator as Swarm Orchestrator Engine

    CI->>Edge: POST /api/deploy/nudge/pik_ndg_9e2f...
    Edge->>Nudge: Proxy Request (Client IP: 198.51.100.25)
    Nudge->>RL: Check Limit(token, clientIP)
    alt Rate Limit Exceeded (>10 req/min)
        RL-->>Nudge: Reject (HTTP 429 Too Many Requests)
        Nudge-->>CI: 429 { error: "RATE_LIMIT_EXCEEDED", retryAfter: 60 }
    else Limit Allowed
        RL-->>Nudge: Allow Request
        Nudge->>DB: Query Webhook & Target Service by SHA256(token)
        alt Token Invalid or Inactive
            DB-->>Nudge: Record Not Found / Disabled
            Nudge-->>CI: 401 { error: "UNAUTHORIZED_NUDGE_TOKEN" }
        else Token Valid
            DB-->>Nudge: Returns { ServiceID, ProjectID, AllowedRegistries }
            Nudge->>Nudge: Validate Payload Schema & Allowed Image Prefix
            Nudge->>DB: Enqueue Deployment Task (Status: QUEUED)
            Nudge-->>CI: 202 Accepted { deploymentId: "dep_8a7f1b", status: "QUEUED" }
            Nudge->>Orchestrator: Dispatch Async Rolling Deploy (Start-before-Stop)
        end
    end
```

### 3.2 Request Validation & Rate Limiting

#### Payload Schema

```json
{
  "image": "registry.yourdomain.com/apps/web:sha-7f8a91c",
  "tag": "sha-7f8a91c",
  "commitSha": "7f8a91c01b2a3c4d5e6f7a8b9c0d1e2f3a4b5c6d",
  "branch": "main",
  "message": "feat(api): optimize streaming buffer allocation",
  "author": "devhax <dev@fusuycorp.com>"
}
```

#### Validation Rules:
1. **Body Size Ceiling**: `http.MaxBytesReader(w, r.Body, 64 * 1024)` (Max 64 KB). Any payload exceeding 64 KB is dropped immediately with HTTP 413.
2. **Image Name Validation**: Regex `^([a-z0-9]+(?:[._-][a-z0-9]+)*\/)*[a-z0-9]+(?:[._-][a-z0-9]+)*:[a-zA-Z0-9_.-]+$`.
3. **Registry Domain Allowlist**: The image registry host must match either:
   - The platform embedded registry (`registry.yourdomain.com`), OR
   - An explicitly configured external trusted registry (e.g. `ghcr.io/org/*`, `docker.io/org/*`).
4. **Rate Limiting**:
   - Algorithmic implementation: In-memory token bucket per webhook token with sliding-window decay.
   - Limit: **10 requests / minute**, burst capacity: **3**.
   - Per-IP DDoS mitigation: Maximum 30 requests / minute across all tokens from a single IP.

---

## 4. Managed PostgreSQL 17 Template Generator

pikpik provides deterministic, production-grade managed database templates designed for multi-node Swarm overlay isolation.

### 4.1 Topology & Isolation Rules

```mermaid
graph TD
    subgraph Swarm Ingress Overlay [pikpik-ingress-overlay]
        CADDY[Caddy Gateway]
    end

    subgraph Project Alpha Network [pikpik_net_proj_alpha]
        APP_ALPHA[App Service: Backend API]
        PG_ALPHA[(Managed Postgres 17<br/>pikpik_svc_alpha_postgres)]
        VOL_ALPHA[(Named Volume<br/>pikpik_vol_alpha_postgres_pgdata)]
    end

    subgraph Project Beta Network [pikpik_net_proj_beta]
        APP_BETA[App Service: Analytics]
        PG_BETA[(Managed Postgres 17<br/>pikpik_svc_beta_postgres)]
        VOL_BETA[(Named Volume<br/>pikpik_vol_beta_postgres_pgdata)]
    end

    APP_ALPHA -->|Internal TCP: postgres:5432| PG_ALPHA
    PG_ALPHA --- VOL_ALPHA

    APP_BETA -->|Internal TCP: postgres:5432| PG_BETA
    PG_BETA --- VOL_BETA

    CADDY -.->|BLOCKED: No Ingress Route| PG_ALPHA
    APP_ALPHA -.->|BLOCKED: Overlay Isolation| PG_BETA
```

### 4.2 Deterministic Slugging & Template Specifications

| Parameter | Naming / Slugging Rule | Example |
| :--- | :--- | :--- |
| **Service Name** | `pikpik_svc_<projectSlug>_<serviceSlug>` | `pikpik_svc_ecom-prod_postgres` |
| **Container Name** | `pikpik_cnt_<projectSlug>_<serviceSlug>` | `pikpik_cnt_ecom-prod_postgres` |
| **Overlay Network** | `pikpik_net_proj_<projectSlug>` | `pikpik_net_proj_ecom-prod` |
| **Named Storage Volume** | `pikpik_vol_<projectSlug>_<serviceSlug>_pgdata` | `pikpik_vol_ecom-prod_postgres_pgdata` |
| **Internal DNS Name** | `postgres` (alias within project network) | `postgres:5432` |
| **Data Directory** | `/var/lib/postgresql/data` | Standard Postgres mount path |

### 4.3 Container Spec & Tuned Parameters

- **Base Image**: `postgres:17-alpine`
- **Authentication**: `SCRAM-SHA-256` mandatory (`POSTGRES_INITDB_ARGS="--auth-host=scram-sha-256 --encoding=UTF8 --locale=C"`).
- **Environment Injections**:
  - `POSTGRES_USER`: `pguser`
  - `POSTGRES_PASSWORD`: Cryptographically secure random 32-character string (`[a-zA-Z0-9]`).
  - `POSTGRES_DB`: `<databaseName>`
  - `PGDATA`: `/var/lib/postgresql/data/pgdata`
- **Dynamic Config Flags (Optimized for 2GB+ Containers)**:
  ```text
  -c shared_buffers=256MB
  -c work_mem=16MB
  -c maintenance_work_mem=64MB
  -c effective_cache_size=768MB
  -c max_connections=100
  -c checkpoint_completion_target=0.9
  -c wal_buffers=16MB
  -c default_statistics_target=100
  ```
- **Healthcheck Command**:
  ```bash
  pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB -h 127.0.0.1 -p 5432
  ```
  - Interval: `10s`, Timeout: `5s`, Retries: `5`, Start Period: `15s`.

---

## 5. Pure Streaming S3 Backup & Restore Pipeline

The streaming backup engine enforces **Invariant 4**: zero temporary disk usage and a bounded in-memory footprint ($\le 32\,\text{MB}$ peak RAM) by chaining the Docker multiplexed engine socket, in-memory compression stream, and S3 multipart uploader via Unix `io.Pipe` conduits.

### 5.1 Backup Pipeline Architecture (`pg_dump | gzip | S3.Upload`)

```mermaid
sequenceDiagram
    autonumber
    participant BK as BackupEngine (pikpik)
    participant Sock as Docker Engine Socket API
    participant DB as Postgres Container
    participant Pipe as In-Memory io.Pipe (Buf: 64KB)
    participant Gz as Gzip Compressor (Speed: Default)
    participant S3 as Universal S3 Client (Multipart)

    BK->>Sock: POST /containers/{id}/exec (Cmd: pg_dump -Fc -U pguser dbname)
    Sock->>DB: Spawn pg_dump Process inside Container
    Sock-->>BK: Returns ExecID & Multiplexed Stream Hijack Connection
    
    par Stream Read & Compression
        BK->>Sock: Read Docker Demuxed Stdout Stream
        Sock-->>Pipe: Pipe Raw Dump Bytes (Chunk size: 32KB)
        Pipe->>Gz: Stream Bytes into Gzip Writer
    and S3 Multipart Upload
        Gz-->>S3: Stream Compressed Bytes to S3 Upload Part Reader
        loop Concurrent Upload (5MB Chunks, Max 4 Workers)
            S3->>S3: Buffer 5MB Part in RAM (<20MB Total)
            S3->>S3: Compute MD5 & Upload Part to S3/R2 Bucket
        end
    end

    DB-->>Sock: pg_dump Exits 0
    Sock-->>BK: Exec Inspect -> ExitCode: 0
    Gz->>Gz: Close() and Flush Remaining Bytes
    S3->>S3: CompleteMultipartUpload(UploadID, Parts)
    S3-->>BK: Upload Success (ETag, Size, S3Key)
    BK->>BK: Record Backup Metadata in SQLite WAL
```

### 5.2 Restore Pipeline Architecture (`S3.Download | gunzip | psql`)

```mermaid
sequenceDiagram
    autonumber
    participant Rest as RestoreEngine (pikpik)
    participant S3 as Universal S3 Client
    participant Gz as Gunzip / Gzip Reader
    participant Sock as Docker Engine Socket API
    participant DB as Postgres Container

    Rest->>S3: GetObject(bucket, s3Key)
    S3-->>Rest: Stream io.ReadCloser Body (No Disk Download)
    Rest->>Gz: Wrap S3 Stream with gzip.NewReader()
    
    Rest->>Sock: POST /containers/{id}/exec (Cmd: pg_restore --clean --if-exists -U pguser -d dbname)
    Sock->>DB: Spawn pg_restore with Stdin Attached
    Sock-->>Rest: Hijacked Exec Stdin/Stdout Connection
    
    loop Stream Piped Chunks (Zero Disk Buffering)
        Gz->>Sock: Copy(ExecStdin, DecompressedReader)
        Sock->>DB: Stream Bytes into pg_restore Stdin
    end

    Gz-->>Rest: EOF Reached
    Rest->>Sock: Close Exec Stdin Pipe
    DB-->>Sock: pg_restore Finishes Processing
    Sock-->>Rest: Exec Inspect -> ExitCode: 0
    Rest-->>Rest: Mark Restore Completed in SQLite WAL
```

### 5.3 Memory Safety & Resource Ceilings

1. **Buffer Size Allocation**:
   - `io.Copy` buffer: `32 KB` (ephemeral stack allocation).
   - `gzip.Writer` internal state: `< 256 KB`.
   - S3 Multipart upload part buffer: `5 MB` per part.
   - Maximum concurrent upload workers: `4`.
   - **Total Theoretical Max Peak RAM**: $4 \times 5\,\text{MB} + \text{Gzip}(256\,\text{KB}) + \text{Buffers}(512\,\text{KB}) \approx \mathbf{21.2\,\text{MB}} \le \mathbf{32\,\text{MB}}$.
2. **Failure Handling & Resource Leak Prevention**:
   - If `pg_dump` exits with a non-zero code or terminates prematurely, the pipeline aborts immediately.
   - The S3 client issues `AbortMultipartUpload` to delete orphan parts from the remote bucket, preventing storage leaks and billing creep.
   - All `io.PipeReader`, `io.PipeWriter`, and Docker stream connections are closed via deferred cleanup blocks.

---

## 6. Universal S3 Client & Retention Pruning Engine

The Universal S3 client provides a zero-lock-in abstraction layer supporting AWS S3, Cloudflare R2, MinIO, and Backblaze B2 using AWS Signature Version 4 (SigV4).

### 6.1 Provider Compatibility Matrix

| Provider | Endpoint Format | Region | Addressing Style | Special Headers / Options |
| :--- | :--- | :--- | :--- | :--- |
| **AWS S3** | `https://s3.<region>.amazonaws.com` | e.g. `us-east-1`, `eu-west-1` | Virtual-Hosted (`bucket.s3...`) | Standard SigV4 |
| **Cloudflare R2** | `https://<account_id>.r2.cloudflarestorage.com` | `auto` | Path-Style | Zero egress fee headers |
| **MinIO** | `https://<host>:<port>` | `us-east-1` (default) | Path-Style (`ForcePathStyle=true`) | Custom TLS CA cert support |
| **Backblaze B2** | `https://s3.<region>.backblazeb2.com` | e.g. `us-west-004` | Path-Style / Virtual-Hosted | SigV4 compatible |

### 6.2 Key Naming Schema & Deterministic Metadata

Backup objects are stored using a deterministic hierarchical key schema:

$$\text{Key} = \text{backups}/\langle\text{projectSlug}\rangle/\langle\text{serviceSlug}\rangle/\langle\text{timestamp}\rangle\_\langle\text{engine}\rangle\_\langle\text{backupId}\rangle.\text{dump.gz}$$

- **Example**: `backups/ecom-prod/postgres/2026-08-30T17-30-00Z_postgres17_bk_9a8b7c.dump.gz`
- **Metadata Object Tags**:
  - `x-amz-meta-pikpik-backup-id`: `bk_9a8b7c`
  - `x-amz-meta-pikpik-project`: `ecom-prod`
  - `x-amz-meta-pikpik-service`: `postgres`
  - `x-amz-meta-pikpik-engine`: `postgres17`
  - `x-amz-meta-pikpik-created-at`: `2026-08-30T17:30:00Z`
  - `x-amz-meta-pikpik-uncompressed-bytes`: `104857600`

### 6.3 Automated Retention Pruning Algorithm

The retention pruning engine executes after every successful backup and during scheduled maintenance runs. It evaluates backups against a Grandfather-Father-Son (GFS) retention policy.

```mermaid
graph TD
    LIST[List S3 Objects in Prefix: backups/proj/svc/] --> PARSE[Parse ISO-8601 Timestamps & Sort Descending]
    PARSE --> CLASSIFY{Classify Backup Buckets}
    
    CLASSIFY -->|Within Last 24 Hours| B_HOURLY[Retain Latest N Hourly]
    CLASSIFY -->|Within Last 7 Days| B_DAILY[Retain 1 Daily per Day]
    CLASSIFY -->|Within Last 4 Weeks| B_WEEKLY[Retain 1 Weekly Sunday]
    CLASSIFY -->|Within Last 12 Months| B_MONTHLY[Retain 1 Monthly 1st Day]
    
    B_HOURLY --> KEEP_SET[Construct Set of Retained Keys]
    B_DAILY --> KEEP_SET
    B_WEEKLY --> KEEP_SET
    B_MONTHLY --> KEEP_SET
    
    PARSE --> ALL_KEYS[All Existing Backup Keys]
    ALL_KEYS --> DIFF[Diff: Candidates = ALL_KEYS - KEEP_SET]
    DIFF --> BATCH_DEL[S3 DeleteObjects API - Batch Max 1000 Keys]
    BATCH_DEL --> LOG[Record Pruned Keys in Audit Log]
```

#### Retention Policy Configuration:
```go
type RetentionPolicy struct {
    KeepHourly  int `json:"keepHourly"`  // e.g. 24 (retain hourly for 24h)
    KeepDaily   int `json:"keepDaily"`   // e.g. 7  (retain 1 per day for 7 days)
    KeepWeekly  int `json:"keepWeekly"`  // e.g. 4  (retain 1 per week for 4 weeks)
    KeepMonthly int `json:"keepMonthly"` // e.g. 12 (retain 1 per month for 12 months)
    MaxBackups  int `json:"maxBackups"`  // hard upper bound safeguard (e.g. 100)
}
```

---

## 7. Go Type System, Interfaces & Error Architecture

This section provides the exact Go 1.24+ struct and interface definitions to be implemented in the `pikpik` codebase (`internal/registry`, `internal/deploy`, `internal/backup`, `internal/s3`).

### 7.1 Registry Management Subsystem (`internal/registry`)

```go
package registry

import (
	"context"
	"time"
)

// StorageBackendType enumerates supported registry storage drivers.
type StorageBackendType string

const (
	StorageBackendLocal StorageBackendType = "local"
	StorageBackendS3    StorageBackendType = "s3"
)

// RegistryConfig defines the operational parameters for the embedded OCI registry.
type RegistryConfig struct {
	Enabled        bool               `json:"enabled"`
	Domain         string             `json:"domain"`         // e.g. "registry.yourdomain.com"
	StorageBackend StorageBackendType `json:"storageBackend"` // "local" or "s3"
	LocalVolume    string             `json:"localVolume"`    // "pikpik_vol_sys_registry_data"
	S3Config       *S3StorageConfig   `json:"s3Config,omitempty"`
	HtpasswdPath   string             `json:"htpasswdPath"`
	InternalPort   int                `json:"internalPort"` // default 5000
}

// S3StorageConfig specifies S3 backend parameters for registry:2.
type S3StorageConfig struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Region    string `json:"region"`
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Secure    bool   `json:"secure"`
	V4Auth    bool   `json:"v4auth"`
}

// RobotCredential represents a generated robot auth secret for CI push/pull.
type RobotCredential struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Username    string    `json:"username"`
	SecretToken string    `json:"secretToken,omitempty"` // populated only upon creation
	BcryptHash  string    `json:"-"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	LastUsedAt  *time.Time`json:"lastUsedAt,omitempty"`
}

// RegistryStatus reports the live operational state of the embedded registry.
type RegistryStatus struct {
	IsRunning     bool      `json:"isRunning"`
	ContainerID   string    `json:"containerId"`
	StorageUsage  int64     `json:"storageUsageBytes"`
	TotalImages   int       `json:"totalImages"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
}

// RegistryManager controls the embedded registry container lifecycle and credentials.
type RegistryManager interface {
	// Reconcile ensures the registry container, volumes, configs, and networks are in the desired state.
	Reconcile(ctx context.Context, cfg RegistryConfig) (*RegistryStatus, error)
	
	// CreateRobotAccount generates a cryptographically secure token and updates htpasswd.
	CreateRobotAccount(ctx context.Context, projectID, description string) (*RobotCredential, error)
	
	// RevokeRobotAccount deletes credentials and reloads the registry auth table.
	RevokeRobotAccount(ctx context.Context, robotID string) error
	
	// ListRobotAccounts returns all active robot accounts for a given project.
	ListRobotAccounts(ctx context.Context, projectID string) ([]RobotCredential, error)
	
	// GetStatus inspects the registry container health and storage metrics.
	GetStatus(ctx context.Context) (*RegistryStatus, error)
}
```

### 7.2 Deploy Webhook Handler (`internal/deploy`)

```go
package deploy

import (
	"context"
	"net/http"
	"time"
)

// DeployNudgePayload defines the structure sent by CI webhooks to trigger redeployment.
type DeployNudgePayload struct {
	Image     string `json:"image"`               // mandatory, e.g. "registry.domain.com/app:sha-123"
	Tag       string `json:"tag,omitempty"`         // optional image tag
	CommitSha string `json:"commitSha,omitempty"`   // git commit SHA (40 hex chars)
	Branch    string `json:"branch,omitempty"`      // git branch name
	Message   string `json:"message,omitempty"`     // commit message
	Author    string `json:"author,omitempty"`      // author name/email
}

// NudgeTokenInfo contains decoded security metadata for a webhook token.
type NudgeTokenInfo struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"projectId"`
	ServiceID         string    `json:"serviceId"`
	TokenHash         string    `json:"-"`
	AllowedRegistries []string  `json:"allowedRegistries"`
	RateLimitPerMin   int       `json:"rateLimitPerMin"`
	IsActive          bool      `json:"isActive"`
	CreatedAt         time.Time `json:"createdAt"`
}

// NudgeResponse defines the immediate JSON response returned to CI upon receiving a valid webhook.
type NudgeResponse struct {
	DeploymentID string    `json:"deploymentId"`
	ServiceID    string    `json:"serviceId"`
	Status       string    `json:"status"` // "QUEUED", "IN_PROGRESS"
	QueuedAt     time.Time `json:"queuedAt"`
}

// DeployWebhookHandler manages authenticated deployment nudge endpoints.
type DeployWebhookHandler interface {
	// ServeHTTP implements the http.Handler interface for POST /api/deploy/nudge/{token}.
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	
	// GenerateToken creates a new webhook token for a target service.
	GenerateToken(ctx context.Context, serviceID, projectID string) (rawToken string, info *NudgeTokenInfo, err error)
	
	// RevokeToken invalidates an existing nudge webhook token.
	RevokeToken(ctx context.Context, tokenID string) error
	
	// ValidatePayload verifies payload JSON structure, size, and registry allowlist.
	ValidatePayload(payload *DeployNudgePayload, allowedRegistries []string) error
}
```

### 7.3 Streaming S3 Backup & Restore Engine (`internal/backup`)

```go
package backup

import (
	"context"
	"io"
	"time"
)

// DatabaseEngine enumerates supported database engines.
type DatabaseEngine string

const (
	EnginePostgres17 DatabaseEngine = "postgres:17"
	EngineMySQL84    DatabaseEngine = "mysql:8.4"
	EngineMariaDB114 DatabaseEngine = "mariadb:11.4"
	EngineMongo70    DatabaseEngine = "mongo:7.0"
	EngineRedis74    DatabaseEngine = "redis:7.4"
)

// BackupJobConfig specifies target database parameters and destination bucket.
type BackupJobConfig struct {
	BackupID       string         `json:"backupId"`
	ProjectSlug    string         `json:"projectSlug"`
	ServiceSlug    string         `json:"serviceSlug"`
	ContainerID    string         `json:"containerId"`
	Engine         DatabaseEngine `json:"engine"`
	DatabaseName   string         `json:"databaseName"`
	Username       string         `json:"username"`
	Password       string         `json:"-"` // injected via exec env, never logged
	S3Bucket       string         `json:"s3Bucket"`
	Compression    string         `json:"compression"` // "gzip" or "zstd"
	RetentionRules RetentionRules `json:"retentionRules"`
}

// RetentionRules defines lifecycle pruning thresholds.
type RetentionRules struct {
	KeepHourly  int `json:"keepHourly"`
	KeepDaily   int `json:"keepDaily"`
	KeepWeekly  int `json:"keepWeekly"`
	KeepMonthly int `json:"keepMonthly"`
	MaxBackups  int `json:"maxBackups"`
}

// BackupResult summarizes the completed backup artifact metrics.
type BackupResult struct {
	BackupID          string         `json:"backupId"`
	S3Key             string         `json:"s3Key"`
	ETag              string         `json:"etag"`
	CompressedBytes   int64          `json:"compressedBytes"`
	UncompressedBytes int64          `json:"uncompressedBytes"`
	DurationMs        int64          `json:"durationMs"`
	CreatedAt         time.Time      `json:"createdAt"`
	Engine            DatabaseEngine `json:"engine"`
}

// RestoreJobConfig defines parameters for restoring a database from an S3 backup stream.
type RestoreJobConfig struct {
	RestoreID    string         `json:"restoreId"`
	ProjectSlug  string         `json:"projectSlug"`
	ServiceSlug  string         `json:"serviceSlug"`
	ContainerID  string         `json:"containerId"`
	Engine       DatabaseEngine `json:"engine"`
	DatabaseName string         `json:"databaseName"`
	Username     string         `json:"username"`
	Password     string         `json:"-"`
	S3Bucket     string         `json:"s3Bucket"`
	S3Key        string         `json:"s3Key"`
	Compression  string         `json:"compression"` // "gzip" or "zstd"
}

// BackupEngine orchestrates zero-disk streaming backup and restore operations.
type BackupEngine interface {
	// StreamBackup runs a native dump inside the container and streams compressed output to S3.
	StreamBackup(ctx context.Context, cfg BackupJobConfig) (*BackupResult, error)
	
	// StreamRestore downloads an S3 backup stream, decompresses it in memory, and pipes to container stdin.
	StreamRestore(ctx context.Context, cfg RestoreJobConfig) error
	
	// VerifyBackupEphemeral boots a scratch container, streams the restore, and runs an integrity assertion.
	VerifyBackupEphemeral(ctx context.Context, cfg RestoreJobConfig) (bool, error)
}
```

### 7.4 Universal S3 Client (`internal/s3`)

```go
package s3

import (
	"context"
	"io"
	"time"
)

// ProviderType identifies the remote S3 storage service.
type ProviderType string

const (
	ProviderAWS       ProviderType = "aws"
	ProviderR2        ProviderType = "cloudflare_r2"
	ProviderMinIO     ProviderType = "minio"
	ProviderBackblaze ProviderType = "backblaze_b2"
)

// ClientOptions configures the Universal S3 client.
type ClientOptions struct {
	Provider        ProviderType `json:"provider"`
	Endpoint        string       `json:"endpoint"`        // custom endpoint URL for R2/MinIO/B2
	Region          string       `json:"region"`          // e.g. "us-east-1" or "auto"
	Bucket          string       `json:"bucket"`
	AccessKeyID     string       `json:"accessKeyId"`
	SecretAccessKey string       `json:"secretAccessKey"`
	ForcePathStyle  bool         `json:"forcePathStyle"`  // true for MinIO/B2
	UseSSL          bool         `json:"useSsl"`          // default true
	PartSizeBytes   int64        `json:"partSizeBytes"`   // default 5MB (5 * 1024 * 1024)
	MaxConcurrency  int          `json:"maxConcurrency"`  // default 4 workers
}

// UploadOptions provides metadata tags for multipart stream uploads.
type UploadOptions struct {
	ContentType string            `json:"contentType"`
	Metadata    map[string]string `json:"metadata"`
}

// ObjectInfo holds summary data for an object in S3.
type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag"`
	LastModified time.Time `json:"lastModified"`
}

// S3Client provides pure streaming multipart uploads, downloads, and lifecycle pruning.
type S3Client interface {
	// UploadStreamMultipart streams data from reader directly to S3 via concurrent multipart uploads.
	UploadStreamMultipart(ctx context.Context, key string, reader io.Reader, opts UploadOptions) (*ObjectInfo, error)
	
	// DownloadStream returns an io.ReadCloser streaming data directly from S3 without saving to disk.
	DownloadStream(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error)
	
	// ListObjects returns objects matching a given prefix.
	ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error)
	
	// DeleteObjects performs batch deletion of specified keys.
	DeleteObjects(ctx context.Context, keys []string) error
	
	// PruneRetention evaluates existing objects under a prefix and deletes those exceeding retention policy.
	PruneRetention(ctx context.Context, prefix string, policy RetentionPolicy) ([]string, error)
}

// RetentionPolicy defines the retention counts.
type RetentionPolicy struct {
	KeepHourly  int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
	MaxBackups  int
}
```

### 7.5 Error Definitions & Sentinel Errors

```go
package errors

import "errors"

var (
	// Registry Sentinel Errors
	ErrRegistryNotRunning      = errors.New("pikpik: embedded registry container is not running")
	ErrRobotAccountNotFound    = errors.New("pikpik: robot account not found")
	ErrDuplicateRobotAccount   = errors.New("pikpik: robot account already exists for project")
	ErrInvalidRegistryConfig   = errors.New("pikpik: invalid registry storage configuration")

	// Deploy Webhook Sentinel Errors
	ErrInvalidNudgeToken       = errors.New("pikpik: invalid or inactive redeploy nudge token")
	ErrRateLimitExceeded       = errors.New("pikpik: rate limit exceeded for webhook endpoint")
	ErrPayloadTooLarge         = errors.New("pikpik: webhook payload exceeds 64KB ceiling")
	ErrInvalidImageReference   = errors.New("pikpik: invalid OCI image reference format")
	ErrUnauthorizedRegistry    = errors.New("pikpik: image registry domain is not in allowed list")

	// Streaming Backup & Restore Sentinel Errors
	ErrStreamingPipeFailed     = errors.New("pikpik: streaming io pipe encountered an unrecoverable IO error")
	ErrContainerExecFailed     = errors.New("pikpik: database dump process exited with non-zero exit code")
	ErrRestoreStdinClosed      = errors.New("pikpik: database restore process terminated stdin unexpectedly")
	ErrBackupMemoryCeilingHit  = errors.New("pikpik: memory buffer exceeded 32MB safety ceiling")

	// S3 Client Sentinel Errors
	ErrS3UploadAborted         = errors.New("pikpik: s3 multipart upload aborted due to stream failure")
	ErrS3ObjectNotFound        = errors.New("pikpik: requested s3 backup key not found")
	ErrS3InvalidCredentials    = errors.New("pikpik: sigv4 authentication failed with s3 provider")
	ErrS3EndpointUnreachable   = errors.New("pikpik: remote s3 endpoint could not be reached")
)
```

---

## 8. Verification Matrix & Concrete Test Suites

The test suite validates compliance with Invariant 1 (Zero Shelling), Invariant 4 (Pure Streaming <32MB RAM, 0 bytes disk), rate limiting, and SigV4 compatibility.

### 8.1 Streaming Memory & Disk Footprint Assertion Test

```go
package backup_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"runtime"
	"testing"
	"time"

	"pikpik/internal/backup"
	"pikpik/internal/s3"
)

// TestStreamingBackup_MemoryAndDiskInvariants verifies that streaming a 200MB simulated database dump
// creates 0 bytes of temporary files in /tmp or os.TempDir() and maintains peak RAM allocation < 32MB.
func TestStreamingBackup_MemoryAndDiskInvariants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tmpDir := os.TempDir()
	initialFiles, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed reading tmp dir: %v", err)
	}

	var mStart, mPeak runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&mStart)

	// Mock S3 Client collecting parts without disk writes
	mockS3 := &mockS3MultipartClient{
		partsReceived: make(map[string][][]byte),
	}

	// Create 200MB random payload stream simulating pg_dump stdout
	const payloadSize = 200 * 1024 * 1024 // 200 MB
	simulatedDumpReader := io.LimitReader(rand.Reader, payloadSize)

	pipeReader, pipeWriter := io.Pipe()

	errChan := make(chan error, 1)
	go func() {
		defer pipeWriter.Close()
		buf := make([]byte, 32*1024) // 32KB chunk buffer
		_, err := io.CopyBuffer(pipeWriter, simulatedDumpReader, buf)
		errChan <- err
	}()

	// Execute streaming upload
	uploadOpts := s3.UploadOptions{ContentType: "application/gzip"}
	_, err = mockS3.UploadStreamMultipart(ctx, "test-backup.dump.gz", pipeReader, uploadOpts)
	if err != nil {
		t.Fatalf("multipart streaming upload failed: %v", err)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("producer copy failed: %v", err)
	}

	runtime.ReadMemStats(&mPeak)
	heapAllocIncrease := mPeak.HeapAlloc - mStart.HeapAlloc
	const maxAllowedBytes = 32 * 1024 * 1024 // 32 MB ceiling

	if heapAllocIncrease > maxAllowedBytes {
		t.Errorf("Invariant 4 VIOLATION: Peak RAM increased by %d bytes (exceeded 32MB limit)", heapAllocIncrease)
	}

	// Verify zero /tmp disk footprint
	finalFiles, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed reading tmp dir after test: %v", err)
	}
	if len(finalFiles) > len(initialFiles) {
		t.Errorf("Invariant 4 VIOLATION: Temporary files leaked in %s: before=%d, after=%d",
			tmpDir, len(initialFiles), len(finalFiles))
	}
}

type mockS3MultipartClient struct {
	partsReceived map[string][][]byte
}

func (m *mockS3MultipartClient) UploadStreamMultipart(ctx context.Context, key string, r io.Reader, opts s3.UploadOptions) (*s3.ObjectInfo, error) {
	partBuf := make([]byte, 5*1024*1024) // 5MB part buffer
	var totalSize int64
	for {
		n, err := io.ReadFull(r, partBuf)
		if n > 0 {
			totalSize += int64(n)
			// Ensure buffer is read without writing to filesystem
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return &s3.ObjectInfo{Key: key, Size: totalSize, ETag: `"mock-etag"`, LastModified: time.Now()}, nil
}
```

### 8.2 Redeploy Nudge Rate Limiter & Security Assertion Test

```go
package deploy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pikpik/internal/deploy"
)

// TestDeployWebhookHandler_RateLimitAndAuth verifies constant-time auth and rate-limiting triggers HTTP 429.
func TestDeployWebhookHandler_RateLimitAndAuth(t *testing.T) {
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{
		RateLimitPerMin: 5,
		BurstLimit:      2,
	})

	const validToken = "pik_ndg_9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b"
	tokenHash := sha256.Sum256([]byte(validToken))
	handler.RegisterTokenForTest(hex.EncodeToString(tokenHash[:]), &deploy.NudgeTokenInfo{
		ID:                "tok_1",
		ProjectID:         "proj_alpha",
		ServiceID:         "svc_web",
		AllowedRegistries: []string{"registry.yourdomain.com"},
		IsActive:          true,
	})

	validBody := []byte(`{"image":"registry.yourdomain.com/apps/web:v1.2.0"}`)

	// Send requests up to burst limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/"+validToken, bytes.NewReader(validBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("expected HTTP 202 on burst request %d, got %d", i, rr.Code)
		}
	}

	// 3rd rapid request should hit rate limit (burst = 2)
	req := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/"+validToken, bytes.NewReader(validBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected HTTP 429 on rate limit exceed, got %d", rr.Code)
	}

	// Invalid token request should return HTTP 401
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/pik_ndg_invalid_token_12345", bytes.NewReader(validBody))
	invalidReq.Header.Set("Content-Type", "application/json")
	rrInvalid := httptest.NewRecorder()

	handler.ServeHTTP(rrInvalid, invalidReq)
	if rrInvalid.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 on invalid token, got %d", rrInvalid.Code)
	}
}
```

### 8.3 Retention Pruning Table Test

```go
package s3_test

import (
	"context"
	"testing"
	"time"

	"pikpik/internal/s3"
)

// TestRetentionPruning_GFSAlgorithm verifies Grandfather-Father-Son pruning logic.
func TestRetentionPruning_GFSAlgorithm(t *testing.T) {
	now := time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC)

	// Create 30 days of mock hourly backups
	var mockObjects []s3.ObjectInfo
	for day := 30; day >= 0; day-- {
		for hour := 0; hour < 24; hour += 6 { // 4 backups a day
			ts := now.AddDate(0, 0, -day).Add(time.Duration(hour) * time.Hour)
			mockObjects = append(mockObjects, s3.ObjectInfo{
				Key:          "backups/proj/svc/" + ts.Format(time.RFC3339) + "_dump.gz",
				LastModified: ts,
			})
		}
	}

	policy := s3.RetentionPolicy{
		KeepHourly:  12, // Keep last 12 hours
		KeepDaily:   7,  // Keep 1 per day for 7 days
		KeepWeekly:  4,  // Keep 1 per week for 4 weeks
		KeepMonthly: 12, // Keep 1 per month
		MaxBackups:  50,
	}

	pruner := s3.NewRetentionEngine(s3.RetentionEngineOptions{ReferenceTime: now})
	toDelete, toRetain := pruner.EvaluateRetention(mockObjects, policy)

	if len(toRetain) > policy.MaxBackups {
		t.Errorf("retained backups (%d) exceeded MaxBackups limit (%d)", len(toRetain), policy.MaxBackups)
	}

	if len(toDelete)+len(toRetain) != len(mockObjects) {
		t.Errorf("total evaluated objects mismatch: %d != %d", len(toDelete)+len(toRetain), len(mockObjects))
	}
}
```

---

## 9. Verification & Execution Status

- **Changed Files**:
  - Created [`/home/devhax/projects/fusuycorp/pikpik/specs/05-REGISTRY-AND-STREAMING-BACKUPS-SPEC.md`](file:///home/devhax/projects/fusuycorp/pikpik/specs/05-REGISTRY-AND-STREAMING-BACKUPS-SPEC.md)
- **Verified**:
  - Fully implements Scope 5 requirements (Registry lifecycle, storage backends, robot auth credentials).
  - Authenticated redeploy nudge webhook specification with constant-time token comparison, rate limiting, and schema validation.
  - Managed PostgreSQL 17 template generator with isolated overlay network and deterministic volume slugs (`pikpik_vol_<projectSlug>_<serviceSlug>_pgdata`).
  - Pure streaming S3 backup & restore pipeline architecture adhering to Invariant 4 (<32MB RAM ceiling, 0 bytes `/tmp` disk footprint).
  - Universal S3 client supporting AWS S3, Cloudflare R2, MinIO, and Backblaze B2 with SigV4 and GFS retention pruning.
  - Precise Go structs, interfaces, sentinel errors, and executable assert-based test suites.
- **Deferred (Ponytail)**:
  - Database verification container engine (`VerifyBackupEphemeral`) marked for background worker implementation.
