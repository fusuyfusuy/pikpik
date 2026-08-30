# Scope 1: Core Foundation & Store Specification

**Document Status**: `APPROVED / ARCHITECTURAL SPECIFICATION`  
**Scope**: `01 - Core Foundation, Persistence Store, Auth, Cryptography & Config Resolution`  
**Language & Runtime**: `Go 1.23+ (Pure Go / Modern SQLite driver - modernc.org/sqlite or mattn/go-sqlite3 with CGO)`  
**Target File**: `specs/01-CORE-AND-STORE-SPEC.md`

---

## 1. Executive Summary & Scope Boundary

Scope 1 forms the foundational kernel of `pikpik`. It encapsulates all persistence state, administrative authentication, API token authorization, at-rest field encryption, and hierarchical environment variable resolution.

```mermaid
graph TD
    subgraph Client / Ingress Boundary
        HTTP_REQ[HTTP API / CLI Client]
        ADMIN_REQ[Admin Dashboard Request]
    end

    subgraph Scope 1: Core Foundation & Store
        AUTH[AuthService<br/>Argon2id + Scoped API Tokens]
        VAULT[CryptoVault<br/>Scrypt KDF + AES-256-GCM]
        ENV_RES[ConfigManager<br/>4-Tier Hierarchy + DAG Resolver]
        STORE[Store Interface & SQL Repository<br/>SQLite WAL + Foreign Keys]
    end

    subgraph Persistence & Durability
        SQLITE_FILE[(paas.db)]
        SQLITE_WAL[(paas.db-wal)]
        LITESTREAM[Litestream Streamer]
        S3_TARGET[(Encrypted S3 / Cloudflare R2)]
    end

    HTTP_REQ --> AUTH
    ADMIN_REQ --> AUTH
    AUTH --> STORE
    ENV_RES --> VAULT
    ENV_RES --> STORE
    STORE --> SQLITE_FILE
    SQLITE_FILE --> SQLITE_WAL
    SQLITE_WAL --> LITESTREAM
    LITESTREAM --> S3_TARGET
```

### Scope 1 Responsibilities:
1. **SQLite Database Architecture**: High-concurrency WAL-mode SQLite database with enforced foreign keys, pragmatic connection pooling, deterministic migration engine, and continuous Litestream S3 replication.
2. **Authentication & Identity**: Single-admin setup with Argon2id async hashing, session revocation, and non-reversible scoped API tokens with SHA-256 lookup indexing.
3. **Cryptographic Secret Vault**: AES-256-GCM envelope field encryption with Scrypt key derivation function (KDF) and unique 96-bit Initialization Vectors (IV).
4. **Hierarchical Environment Engine**: 4-Tier cascading environment variable inheritance (`Organization -> Project -> Stage -> Service`) with topological DAG cycle detection and secret value masking.
5. **Contract Interfaces & Domain Types**: Clean, decoupled Go interfaces (`Store`, `AuthService`, `CryptoVault`, `ConfigManager`) and comprehensive test harnesses.

---

## 2. SQLite Database Architecture & Schema

### 2.1 Pragmas & Concurrency Tuning
The database must be initialized with explicit SQLite pragmas on connection acquisition to achieve sub-millisecond query performance, multi-reader concurrency, and foreign key integrity.

```sql
-- Executed on EVERY database connection initialization:
PRAGMA journal_mode = WAL;            -- Write-Ahead Logging for non-blocking concurrent readers
PRAGMA foreign_keys = ON;             -- Strict referential integrity enforcement
PRAGMA busy_timeout = 5000;           -- Wait up to 5000ms on lock contention before SQLITE_BUSY
PRAGMA synchronous = NORMAL;          -- Optimal durability in WAL mode without fsync bottleneck
PRAGMA journal_size_limit = 67108864; -- 64MB journal cap to prevent unbounded WAL file growth
PRAGMA mmap_size = 268435456;         -- 256MB memory-mapped I/O for high read throughput
PRAGMA temp_store = MEMORY;           -- Store temporary tables and indices in RAM
PRAGMA cache_size = -64000;           -- 64MB in-memory page cache
```

#### Go Connection Pool Configuration:
- `db.SetMaxOpenConns(25)`: Allows up to 25 concurrent read goroutines.
- `db.SetMaxIdleConns(5)`: Retains warm idle connections.
- `db.SetConnMaxLifetime(1 * time.Hour)`: Refreshes idle file descriptors.
- `db.SetConnMaxIdleTime(15 * time.Minute)`.
- *Write Lock Discipline*: All write transactions execute through a dedicated mutex or serialized transaction helper (`store.WithTx(ctx, func(tx Store) error)`) to prevent `SQLITE_BUSY` deadlocks.

---

### 2.2 Database Migration Engine

Migrations are embedded directly into the Go binary via `embed.FS` and executed sequentially inside a single transaction during service startup.

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    checksum    TEXT NOT NULL
);
```

#### Migration Invariants:
1. Migrations are numbered sequentially (`00001_init.sql`, `00002_add_volumes.sql`).
2. Each migration must be idempotent and transactional (`BEGIN TRANSACTION; ... COMMIT;`).
3. SHA-256 checksums are validated on startup; modifying an already-applied migration file triggers a fatal startup error (`ErrMigrationChecksumMismatch`).
4. Downgrade/Rollback policy: Schema rollbacks are executed by applying forward-only fix migrations (Ponytail principle: No dead migration baggage).

---

### 2.3 Relational DDL Schema (`00001_initial_schema.sql`)

```sql
-- ============================================================================
-- 1. ORGANIZATIONS & USERS
-- ============================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id          TEXT PRIMARY KEY, -- ULID / UUIDv7 (e.g. org_01J6A...)
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    id                 TEXT PRIMARY KEY,
    email              TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash      TEXT NOT NULL, -- Argon2id encoded string
    role               TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'admin', 'developer', 'viewer')),
    totp_secret        TEXT,          -- Encrypted with CryptoVault (AES-256-GCM)
    totp_enabled       INTEGER NOT NULL DEFAULT 0,
    session_version    INTEGER NOT NULL DEFAULT 1,
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- ============================================================================
-- 2. SESSIONS & SCOPED API TOKENS
-- ============================================================================

CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY, -- Session Token Hash (sha256)
    user_id      TEXT NOT NULL,
    ip_address   TEXT NOT NULL,
    user_agent   TEXT NOT NULL,
    expires_at   DATETIME NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL,
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL, -- First 12 chars e.g. "pik_live_ab12"
    token_hash   TEXT NOT NULL UNIQUE, -- SHA-256 hex digest of full secret token
    scopes       TEXT NOT NULL, -- JSON Array: ["deploy:write", "services:read"]
    last_used_at DATETIME,
    expires_at   DATETIME,      -- NULL = Never expires
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_lookup ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);

-- ============================================================================
-- 3. PROJECTS, STAGES & SERVICES
-- ============================================================================

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_projects_org ON projects(org_id);
CREATE INDEX IF NOT EXISTS idx_projects_slug ON projects(slug);

CREATE TABLE IF NOT EXISTS stages (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_stages_project ON stages(project_id);

CREATE TABLE IF NOT EXISTS services (
    id                 TEXT PRIMARY KEY,
    project_id         TEXT NOT NULL,
    stage_id           TEXT NOT NULL,
    name               TEXT NOT NULL,
    slug               TEXT NOT NULL,
    type               TEXT NOT NULL CHECK (type IN ('app', 'database', 'worker', 'job')),
    image              TEXT NOT NULL,
    replicas           INTEGER NOT NULL DEFAULT 1,
    container_port     INTEGER,
    domain_names       TEXT NOT NULL DEFAULT '[]', -- JSON Array: ["api.domain.com"]
    deploy_token_hash  TEXT,                       -- Nudge webhook token hash
    status             TEXT NOT NULL DEFAULT 'idle' CHECK (status IN ('idle', 'deploying', 'running', 'unhealthy', 'stopped', 'failed')),
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (stage_id) REFERENCES stages(id) ON DELETE CASCADE,
    UNIQUE(project_id, stage_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_services_stage ON services(stage_id);
CREATE INDEX IF NOT EXISTS idx_services_lookup ON services(project_id, stage_id, slug);

-- ============================================================================
-- 4. HIERARCHICAL ENVIRONMENT VARIABLES
-- ============================================================================

CREATE TABLE IF NOT EXISTS env_vars (
    id               TEXT PRIMARY KEY,
    scope_tier       TEXT NOT NULL CHECK (scope_tier IN ('organization', 'project', 'stage', 'service')),
    resource_id      TEXT NOT NULL, -- Target org_id, project_id, stage_id, or service_id
    key              TEXT NOT NULL,
    value_encrypted  TEXT NOT NULL, -- Encrypted format: v1:base64(iv):base64(authTag):base64(ciphertext)
    is_secret        INTEGER NOT NULL DEFAULT 1,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(scope_tier, resource_id, key)
);

CREATE INDEX IF NOT EXISTS idx_env_vars_resource ON env_vars(scope_tier, resource_id);

-- ============================================================================
-- 5. VOLUMES & CONFIG MOUNTS
-- ============================================================================

CREATE TABLE IF NOT EXISTS volumes (
    id                      TEXT PRIMARY KEY,
    project_id              TEXT NOT NULL,
    service_id              TEXT NOT NULL,
    name                    TEXT NOT NULL,
    slug                    TEXT NOT NULL, -- Deterministic slug: pikpik_vol_<projSlug>_<svcSlug>_<name>
    mount_path              TEXT NOT NULL,
    type                    TEXT NOT NULL CHECK (type IN ('named', 'bind', 'file')),
    host_path               TEXT,          -- Used if type == 'bind'
    config_content_encrypted TEXT,         -- Used if type == 'file'
    created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE CASCADE,
    UNIQUE(service_id, mount_path)
);

CREATE INDEX IF NOT EXISTS idx_volumes_service ON volumes(service_id);

-- ============================================================================
-- 6. DEPLOYMENTS, BACKUPS & AUDIT LOGS
-- ============================================================================

CREATE TABLE IF NOT EXISTS deployments (
    id             TEXT PRIMARY KEY,
    service_id     TEXT NOT NULL,
    image_tag      TEXT NOT NULL,
    commit_sha     TEXT,
    status         TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'preparing', 'starting', 'healthy', 'failed', 'rolled_back')),
    logs_summary   TEXT,
    initiated_by   TEXT NOT NULL, -- user_id or 'api_token:<prefix>' or 'webhook'
    started_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at    DATETIME,
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_deployments_service ON deployments(service_id, started_at DESC);

CREATE TABLE IF NOT EXISTS backup_configs (
    id             TEXT PRIMARY KEY,
    service_id     TEXT NOT NULL UNIQUE, -- Database service
    s3_endpoint    TEXT NOT NULL,
    s3_bucket      TEXT NOT NULL,
    s3_region      TEXT NOT NULL,
    s3_access_key  TEXT NOT NULL,
    s3_secret_key_encrypted TEXT NOT NULL,
    cron_expr      TEXT NOT NULL,        -- e.g. "0 3 * * *" (Every day at 3am)
    retention_days INTEGER NOT NULL DEFAULT 30,
    is_enabled     INTEGER NOT NULL DEFAULT 1,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS backup_executions (
    id             TEXT PRIMARY KEY,
    config_id      TEXT NOT NULL,
    service_id     TEXT NOT NULL,
    s3_key         TEXT NOT NULL,
    bytes_streamed INTEGER NOT NULL,
    duration_ms    INTEGER NOT NULL,
    status         TEXT NOT NULL CHECK (status IN ('in_progress', 'completed', 'failed')),
    error_message  TEXT,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (config_id) REFERENCES backup_configs(id) ON DELETE CASCADE,
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_backup_executions_service ON backup_executions(service_id, created_at DESC);

CREATE TABLE IF NOT EXISTS audit_logs (
    id            TEXT PRIMARY KEY,
    user_id       TEXT,
    action        TEXT NOT NULL, -- e.g. "service.deploy", "env.update", "vault.rotate"
    resource_type TEXT NOT NULL, -- e.g. "service", "project", "env_var"
    resource_id   TEXT NOT NULL,
    metadata      TEXT NOT NULL DEFAULT '{}', -- JSON payload
    ip_address    TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC);
```

---

### 2.4 Litestream Continuous S3 Backup Configuration

Litestream operates as a lightweight continuous replication daemon that monitors SQLite WAL frames and streams encrypted incremental WAL segments to Cloudflare R2 or AWS S3 every 10 seconds.

#### A. Litestream Configuration (`/etc/litestream.yml`)

```yaml
dbs:
  - path: /var/lib/pikpik/data/pikpik.db
    replicas:
      - type: s3
        bucket: ${PIKPIK_LITESTREAM_BUCKET}
        path: db-backups/pikpik-primary
        region: ${PIKPIK_LITESTREAM_REGION:-auto}
        endpoint: ${PIKPIK_LITESTREAM_ENDPOINT}
        access-key-id: ${PIKPIK_LITESTREAM_ACCESS_KEY}
        secret-access-key: ${PIKPIK_LITESTREAM_SECRET_KEY}
        sync-interval: 10s
        retention: 720h # 30 Days point-in-time recovery
        snapshot-interval: 24h
        validation-interval: 1h
```

#### B. Disaster Recovery & Cold Start Initialization Flow

```mermaid
sequenceDiagram
    autonumber
    participant Init as Container Entrypoint Script
    participant S3 as Litestream S3 / Cloudflare R2
    participant DB as Local Filesystem (/var/lib/pikpik/data)
    participant CP as Pikpik Control Plane Process

    Init->>DB: Check if pikpik.db exists
    alt DB Missing (Disaster Recovery / Fresh Host)
        Init->>S3: litestream restore -if-replica-exists /var/lib/pikpik/data/pikpik.db
        S3-->>DB: Stream & Reassemble latest Snapshot + WAL frames
        Init->>DB: Verify SQLite integrity (PRAGMA integrity_check)
    else DB Exists
        Init->>DB: Proceed with local database
    end
    Init->>CP: Start pikpik process with Litestream sidecar / goroutine
    CP->>DB: Run embedded SQL migrations
```

---

## 3. Authentication & Credential Subsystem

### 3.1 Single Admin & Password Hashing: Argon2id

`pikpik` adopts **Argon2id** (RFC 9106) for administrative password verification. Argon2id provides maximum resistance against both GPU-accelerated brute-force attacks and side-channel cache attacks.

#### A. Cryptographic Parameters
```go
const (
    Argon2Time        = 3           // Number of iterations (t)
    Argon2Memory      = 64 * 1024   // 64 MB memory cost (m)
    Argon2Parallelism = 2           // 2 concurrent OS threads (p)
    Argon2SaltLength  = 16          // 16 bytes cryptographically secure random salt
    Argon2KeyLength   = 32          // 32 bytes derived key output (256-bit)
)
```

#### B. Encoded Hash Format
Hashes are serialized using standard PHC string format:
```
$argon2id$v=19$m=65536,t=3,p=2$<base64_salt>$<base64_hash>
```

#### C. Verification Algorithm
1. Parse parameter string, salt, and expected hash from PHC format.
2. Compute `derivedKey = argon2.IDKey(password, salt, t, m, p, keyLen)`.
3. Perform constant-time byte comparison using `subtle.ConstantTimeCompare(derivedKey, expectedHash)` to eliminate timing side-channel attacks.

---

### 3.2 Scoped API Token Architecture

API Tokens enable automated CLI interactions, GitHub Actions CI/CD deployment nudges, and programmatic API access without exposing user passwords.

```mermaid
graph TD
    CLI[GitHub Actions / CLI Client] -->|Presents Token: pik_live_x8F9...| GATEWAY[Auth Middleware]
    GATEWAY -->|1. Compute sha256(token)| HASH_ENGINE[SHA-256 Hash Engine]
    HASH_ENGINE -->|2. Exact Query: WHERE token_hash = ?| DB[(SQLite: api_tokens)]
    DB -->|3. Return Token Metadata & Scopes| GATEWAY
    GATEWAY -->|4. Constant-Time Verification & Scope Check| AUTH_CHECK{Authorized?}
    AUTH_CHECK -->|Yes| PROCESS[Execute Request]
    AUTH_CHECK -->|No| REJECT[HTTP 401 / 403]
```

#### A. Token Format & Specification
- **Prefix**: `pik_live_` (Production Live Token) or `pik_test_` (Testing Token).
- **Entropy**: 256 bits of cryptographically secure random bytes generated via `crypto/rand`.
- **String Encoding**: Base62 (alphanumeric `[a-zA-Z0-9]`) length 43 characters.
- **Example Plaintext Token**: `pik_live_7mK8ZpQ2w9XyA4vB1cN6dE8fG0hJ3kL5`
- **Database Stored Prefix**: `pik_live_7mK8` (First 12 characters for UI identification).
- **Database Stored Lookup Key**: `token_hash = hex(sha256("pik_live_7mK8ZpQ2w9XyA4vB1cN6dE8fG0hJ3kL5"))`.

#### B. Scope Hierarchy & Permissions Matrix

| Scope String | Description | Allowed Actions |
| :--- | :--- | :--- |
| `admin:*` | Superadmin Wildcard | Full system access, user management, secret decryption |
| `project:read` | Read Project Metadata | View projects, stages, services, and health status |
| `project:write` | Modify Project Config | Create/update services, change stage variables |
| `deploy:write` | Deployment Trigger | Trigger service rollouts, push image tags (`/api/deploy/nudge`) |
| `database:backup` | Execute Streaming Backups | Trigger manual database snapshots to S3 |
| `logs:read` | Telemetry & Logs | Stream real-time WebSocket logs and container metrics |

---

## 4. Secret Vault & Cryptography (`CryptoVault`)

The `CryptoVault` encrypts all sensitive database fields at rest: environment secrets, git access tokens, S3 secret keys, and database passwords.

```mermaid
graph TD
    subgraph Initialization Phase
        ENV_KEY[Master Secret: PIKPIK_SECRET_KEY] --> KDF[Scrypt KDF<br/>N=32768, r=8, p=1]
        SALT[Fixed System Salt / Stored Salt] --> KDF
        KDF --> DERIVED_KEY[32-Byte Master Key]
    end

    subgraph Field Encryption Phase
        PLAIN_SECRET[Plaintext Secret: 'db_password_123']
        DERIVED_KEY --> GCM_CIPHER[AES-256-GCM Engine]
        RNG[crypto/rand] -->|Generate 96-bit| IV[Random 12-byte IV]
        IV --> GCM_CIPHER
        PLAIN_SECRET --> GCM_CIPHER
        GCM_CIPHER --> AUTH_TAG[16-byte Auth Tag]
        GCM_CIPHER --> CIPHERTEXT[Encrypted Bytes]
        
        IV & AUTH_TAG & CIPHERTEXT --> FORMAT[Formatter]
        FORMAT --> STORED[v1:base64_iv:base64_tag:base64_ciphertext]
    end
```

### 4.1 Master Key Derivation (Scrypt KDF)
To prevent attacks if `PIKPIK_SECRET_KEY` has low entropy, the encryption key is derived using **Scrypt**:
- `N = 32768` ($2^{15}$ CPU/memory cost)
- `r = 8` (Block size)
- `p = 1` (Parallelization parameter)
- `keyLen = 32` bytes (256 bits)
- `salt = []byte("pikpik-vault-master-salt-v1")` (Deterministic system salt)

### 4.2 AES-256-GCM Field Encryption
- **Cipher Mode**: Galois/Counter Mode (GCM) providing Authenticated Encryption with Associated Data (AEAD).
- **IV / Nonce Generation**: 96 bits (12 bytes) uniquely generated per encryption via `crypto/rand.Read()`. **Reusing an IV with the same key is strictly fatal in GCM.**
- **Authentication Tag**: 128 bits (16 bytes) appended or separated for integrity validation.

### 4.3 Stored String Envelope Format
All encrypted database columns conform strictly to the versioned delimited string format:
$$\text{Envelope} = \text{v1}:\text{base64}(\text{IV}):\text{base64}(\text{AuthTag}):\text{base64}(\text{Ciphertext})$$

Example:
`v1:dGhpcyBpcyBhbiBpdg==:YXV0aCB0YWcgYnl0ZXM=:Y2lwaGVydGV4dCBib2R5`

---

## 5. Hierarchical Environment Variable Engine (`ConfigManager`)

### 5.1 4-Tier Inheritance & Precedence Matrix

`pikpik` cascades environment variables across 4 distinct organizational boundaries:

$$\text{Service Level (Tier 4)} \succ \text{Stage Level (Tier 3)} \succ \text{Project Level (Tier 2)} \succ \text{Organization Level (Tier 1)}$$

```mermaid
graph TD
    ORG[Tier 1: Organization<br/>SENTRY_DSN=sentry.io/123<br/>COMPANY_DOMAIN=acme.org<br/>LOG_LEVEL=info]
    
    PROJ[Tier 2: Project (e-commerce)<br/>LOG_LEVEL=debug<br/>POSTGRES_HOST=pg-cluster<br/>POSTGRES_PORT=5432]
    
    STAGE[Tier 3: Stage (production)<br/>NODE_ENV=production<br/>REPLICAS=3]
    
    SVC[Tier 4: Service (auth-api)<br/>PORT=8080<br/>LOG_LEVEL=trace]

    ORG --> PROJ
    PROJ --> STAGE
    STAGE --> SVC
    
    SVC --> RESOLVED[Resolved Config Map<br/>PORT = 8080 (from Service)<br/>LOG_LEVEL = trace (overridden by Service)<br/>NODE_ENV = production (from Stage)<br/>POSTGRES_HOST = pg-cluster (from Project)<br/>SENTRY_DSN = sentry.io/123 (from Org)]
```

#### Collision Resolution Rule:
If the same key is defined in multiple tiers, the lower-level tier overrides the higher-level tier with zero warnings.

---

### 5.2 Dynamic Variable Interpolation & DAG Cycle Detection

Variables can reference other variables across the resolved hierarchy using `${VAR_NAME}` or `$VAR_NAME` syntax.

#### A. Interpolation Syntax Rules:
1. `${VAR_NAME}`: Standard reference syntax (e.g. `DATABASE_URL=postgres://${DB_USER}:${DB_PASS}@${DB_HOST}:${DB_PORT}/${DB_NAME}`).
2. `$VAR_NAME`: Unbraced syntax support.
3. `$$`: Escaped literal dollar sign (e.g. `COST=$$100` resolves to `COST=$100`).
4. Undefined reference: Returns error `ErrUnresolvedVariable` if a referenced variable does not exist.

#### B. Directed Acyclic Graph (DAG) Resolution Engine:
1. **Parser**: Scan each variable value for `${...}` tokens and extract dependency edges: `A -> [B, C]`.
2. **Cycle Detection**: Build an adjacency list and compute in-degrees (Kahn's Algorithm) or run DFS with three-color node marking (`White=Unvisited`, `Gray=Visiting`, `Black=Visited`).
3. If a cycle is encountered (visiting a `Gray` node), build and return `ErrCyclicDependency` with the exact cycle path (`A -> B -> C -> A`).
4. **Evaluation**: Evaluate and substitute variables in topological order.

```mermaid
graph LR
    subgraph Valid DAG
        DB_USER[DB_USER=admin] --> DB_URL[DB_URL=postgres://${DB_USER}:${DB_PASS}@...]
        DB_PASS[DB_PASS=secret] --> DB_URL
    end

    subgraph Cyclic Dependency (Fatal Error)
        VAR_A[A = ${B}_1] -->|References| VAR_B[B = ${C}_2]
        VAR_B -->|References| VAR_C[C = ${A}_3]
        VAR_C -.->|CYCLE DETECTED: A -> B -> C -> A| VAR_A
    end
```

---

### 5.3 Secret Masking Pipeline

To prevent accidental secret leaks in build logs, container stdout/stderr streams, or error traces:
1. During resolution, `ConfigManager` tags every resolved variable marked `is_secret = 1`.
2. Values longer than 3 characters are loaded into an in-memory `SecretMasker` (Aho-Corasick trie or sorted multi-string replacer).
3. All log lines passing through the deployment logger or WebSocket hub are passed through `masker.Mask(rawLog)`:
   - Example: `Connecting to postgres://admin:supersecret@10.0.0.2` $\to$ `Connecting to postgres://admin:[REDACTED]@10.0.0.2`

---

## 6. Precise Go Struct & Interface Definitions

### 6.1 Domain Entities (`internal/domain/models.go`)

```go
package domain

import (
	"time"
)

// ScopeTier represents the 4-tier inheritance level.
type ScopeTier string

const (
	TierOrg     ScopeTier = "organization"
	TierProject ScopeTier = "project"
	TierStage   ScopeTier = "stage"
	TierService ScopeTier = "service"
)

// User represents an administrative tenant.
type User struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	PasswordHash   string    `json:"-"`
	Role           string    `json:"role"`
	TOTPSecret     string    `json:"-"`
	TOTPEnabled    bool      `json:"totp_enabled"`
	SessionVersion int       `json:"session_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// APIToken represents a machine-to-machine scoped authorization token.
type APIToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`     // e.g. "pik_live_a1b2"
	TokenHash  string     `json:"-"`          // SHA-256 hex digest
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// EnvVar represents an encrypted environment variable at any hierarchy tier.
type EnvVar struct {
	ID             string    `json:"id"`
	ScopeTier      ScopeTier `json:"scope_tier"`
	ResourceID     string    `json:"resource_id"`
	Key            string    `json:"key"`
	ValueEncrypted string    `json:"-"`
	IsSecret       bool      `json:"is_secret"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Project represents an application grouping.
type Project struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Stage represents an environment stage (e.g. production, staging).
type Stage struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Service represents a workload container or database.
type Service struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	StageID         string    `json:"stage_id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Type            string    `json:"type"` // "app", "database", "worker", "job"
	Image           string    `json:"image"`
	Replicas        int       `json:"replicas"`
	ContainerPort   int       `json:"container_port"`
	DomainNames     []string  `json:"domain_names"`
	DeployTokenHash string    `json:"-"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
```

---

### 6.2 Storage Engine Interfaces (`internal/store/store.go`)

```go
package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fusuycorp/pikpik/internal/domain"
)

var (
	ErrNotFound          = errors.New("record not found")
	ErrDuplicateKey      = errors.New("duplicate key conflict")
	ErrOptimisticLock    = errors.New("optimistic lock violation")
	ErrTransactionClosed = errors.New("transaction already closed")
)

// Store aggregates all database operations and transaction lifecycle.
type Store interface {
	Users() UserStore
	APITokens() APITokenStore
	Projects() ProjectStore
	Stages() StageStore
	Services() ServiceStore
	EnvVars() EnvVarStore
	Audit() AuditStore

	// WithTx executes the supplied operation inside an atomic database transaction.
	WithTx(ctx context.Context, fn func(tx Store) error) error
	// Ping verifies database liveness and WAL accessibility.
	Ping(ctx context.Context) error
	// Close cleanly terminates the database connection pool.
	Close() error
}

type UserStore interface {
	Create(ctx context.Context, user *domain.User) error
	GetByID(ctx context.Context, id string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	UpdatePassword(ctx context.Context, id string, passwordHash string, bumpSession bool) error
	Count(ctx context.Context) (int64, error)
}

type APITokenStore interface {
	Create(ctx context.Context, token *domain.APIToken) error
	GetByHash(ctx context.Context, tokenHash string) (*domain.APIToken, error)
	ListByUser(ctx context.Context, userID string) ([]*domain.APIToken, error)
	TouchLastUsed(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

type EnvVarStore interface {
	Set(ctx context.Context, v *domain.EnvVar) error
	Get(ctx context.Context, tier domain.ScopeTier, resourceID, key string) (*domain.EnvVar, error)
	ListByResource(ctx context.Context, tier domain.ScopeTier, resourceID string) ([]*domain.EnvVar, error)
	Delete(ctx context.Context, tier domain.ScopeTier, resourceID, key string) error
}

type ProjectStore interface {
	Create(ctx context.Context, p *domain.Project) error
	GetByID(ctx context.Context, id string) (*domain.Project, error)
	GetBySlug(ctx context.Context, slug string) (*domain.Project, error)
	List(ctx context.Context, orgID string) ([]*domain.Project, error)
	Delete(ctx context.Context, id string) error
}

type StageStore interface {
	Create(ctx context.Context, s *domain.Stage) error
	GetByID(ctx context.Context, id string) (*domain.Stage, error)
	ListByProject(ctx context.Context, projectID string) ([]*domain.Stage, error)
}

type ServiceStore interface {
	Create(ctx context.Context, s *domain.Service) error
	GetByID(ctx context.Context, id string) (*domain.Service, error)
	GetBySlug(ctx context.Context, projectID, stageID, slug string) (*domain.Service, error)
	ListByStage(ctx context.Context, stageID string) ([]*domain.Service, error)
	UpdateStatus(ctx context.Context, id string, status string) error
}

type AuditStore interface {
	Record(ctx context.Context, userID, action, resType, resID, metadataJSON, ip string) error
}
```

---

### 6.3 Cryptography & Vault Interface (`internal/crypto/vault.go`)

```go
package crypto

import (
	"context"
	"errors"
)

var (
	ErrDecryptionFailed = errors.New("crypto: authenticated decryption failed or corrupt payload")
	ErrInvalidMasterKey = errors.New("crypto: invalid master secret key")
	ErrInvalidEnvelope  = errors.New("crypto: invalid envelope format")
)

// Vault handles field-level AES-256-GCM encryption with Scrypt key derivation.
type Vault interface {
	// Encrypt encrypts plaintext bytes and returns a versioned envelope string.
	// Format: v1:base64(iv):base64(authTag):base64(ciphertext)
	Encrypt(ctx context.Context, plaintext []byte) (string, error)

	// Decrypt parses an envelope string, verifies authentication tag, and returns plaintext.
	Decrypt(ctx context.Context, envelope string) ([]byte, error)

	// EncryptString is a helper for UTF-8 strings.
	EncryptString(ctx context.Context, plainText string) (string, error)

	// DecryptString is a helper for UTF-8 strings.
	DecryptString(ctx context.Context, envelope string) (string, error)
}
```

---

### 6.4 Authentication Service Interface (`internal/auth/service.go`)

```go
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/fusuycorp/pikpik/internal/domain"
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	ErrTokenExpired       = errors.New("auth: api token has expired")
	ErrTokenNotFound      = errors.New("auth: token not recognized")
	ErrInsufficientScope  = errors.New("auth: insufficient permissions for operation")
	ErrAdminAlreadyExists = errors.New("auth: initial admin already configured")
)

type GeneratedToken struct {
	Token    *domain.APIToken `json:"token"`
	RawSecret string          `json:"raw_secret"` // Displayed only ONCE to user
}

type AuthService interface {
	// BootstrapAdmin creates the initial system owner if no users exist.
	BootstrapAdmin(ctx context.Context, email, password string) (*domain.User, error)

	// AuthenticateUser verifies user credentials via Argon2id.
	AuthenticateUser(ctx context.Context, email, password string) (*domain.User, error)

	// CreateAPIToken generates a new cryptographically secure pik_live_ token.
	CreateAPIToken(ctx context.Context, userID, name string, scopes []string, expiresAt *time.Time) (*GeneratedToken, error)

	// ValidateAPIToken performs constant-time lookup and checks expiration and scopes.
	ValidateAPIToken(ctx context.Context, rawSecret string, requiredScope string) (*domain.APIToken, error)

	// HashPassword hashes a password string with Argon2id parameters.
	HashPassword(password string) (string, error)

	// VerifyPassword compares a plaintext password against an Argon2id PHC string.
	VerifyPassword(password, encodedHash string) (bool, error)
}
```

---

### 6.5 Hierarchical Config Manager Interface (`internal/configmgr/resolver.go`)

```go
package configmgr

import (
	"context"
	"errors"

	"github.com/fusuycorp/pikpik/internal/domain"
)

var (
	ErrCyclicDependency   = errors.New("configmgr: circular variable reference detected in DAG")
	ErrUnresolvedVariable = errors.New("configmgr: referenced variable does not exist")
)

// ResolvedEnv represents the computed key-value pairs ready for container injection.
type ResolvedEnv struct {
	Variables map[string]string // Key -> Decrypted Final Value
	Secrets   map[string]bool   // Key -> True if masked secret
}

// ConfigManager resolves the 4-tier cascading hierarchy with DAG interpolation.
type ConfigManager interface {
	// ResolveHierarchy fetches Org, Project, Stage, and Service variables,
	// applies cascading overrides, decrypts secrets, and expands references.
	ResolveHierarchy(
		ctx context.Context,
		orgID, projectID, stageID, serviceID string,
	) (*ResolvedEnv, error)

	// ExpandVariables performs DAG dependency sorting and cycle detection on raw map.
	ExpandVariables(raw map[string]string) (map[string]string, error)

	// BuildMasker returns a SecretMasker initialized with all secret values in this resolution.
	BuildMasker(resolved *ResolvedEnv) SecretMasker
}

// SecretMasker redacts sensitive values from logs and error messages.
type SecretMasker interface {
	Mask(input string) string
}
```

---

## 7. Concrete Implementation Specifications

### 7.1 Argon2id Password Hasher (`internal/auth/argon2.go`)

```go
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Hasher struct {
	time        uint32
	memory      uint32
	parallelism uint8
	keyLen      uint32
	saltLen     uint32
}

func DefaultArgon2Hasher() *Argon2Hasher {
	return &Argon2Hasher{
		time:        3,
		memory:      64 * 1024, // 64 MB
		parallelism: 2,
		keyLen:      32,
		saltLen:     16,
	}
}

func (h *Argon2Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate random salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.parallelism, h.keyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	// PHC string format
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.time, h.parallelism, b64Salt, b64Hash,
	)
	return encoded, nil
}

func (h *Argon2Hasher) Verify(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid argon2id hash format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}

	var memory, timeVal uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeVal, &parallelism); err != nil {
		return false, errors.New("failed to parse argon2 parameters")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errors.New("invalid salt encoding")
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errors.New("invalid hash encoding")
	}

	calculatedHash := argon2.IDKey([]byte(password), salt, timeVal, memory, parallelism, uint32(len(expectedHash)))

	// Subtle constant-time comparison against timing attacks
	if subtle.ConstantTimeCompare(calculatedHash, expectedHash) == 1 {
		return true, nil
	}
	return false, nil
}
```

---

### 7.2 AES-256-GCM Vault Implementation (`internal/crypto/aes_vault.go`)

```go
package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/scrypt"
)

type AESVault struct {
	derivedKey []byte
}

var vaultSalt = []byte("pikpik-vault-master-salt-v1")

// NewAESVault derives a 256-bit key from masterSecret using Scrypt.
func NewAESVault(masterSecret string) (*AESVault, error) {
	if len(masterSecret) < 16 {
		return nil, errors.New("master secret key must be at least 16 characters")
	}

	key, err := scrypt.Key([]byte(masterSecret), vaultSalt, 32768, 8, 1, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key via scrypt: %w", err)
	}

	return &AESVault{derivedKey: key}, nil
}

func (v *AESVault) Encrypt(ctx context.Context, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(v.derivedKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// 96-bit unique IV per operation
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to read random nonce: %w", err)
	}

	// Seal encrypts and appends the 16-byte authentication tag to ciphertext
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	tagSize := 16
	ciphertext := sealed[:len(sealed)-tagSize]
	authTag := sealed[len(sealed)-tagSize:]

	b64IV := base64.StdEncoding.EncodeToString(nonce)
	b64Tag := base64.StdEncoding.EncodeToString(authTag)
	b64Cipher := base64.StdEncoding.EncodeToString(ciphertext)

	return fmt.Sprintf("v1:%s:%s:%s", b64IV, b64Tag, b64Cipher), nil
}

func (v *AESVault) Decrypt(ctx context.Context, envelope string) ([]byte, error) {
	parts := strings.Split(envelope, ":")
	if len(parts) != 4 || parts[0] != "v1" {
		return nil, ErrInvalidEnvelope
	}

	nonce, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidEnvelope
	}

	authTag, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidEnvelope
	}

	ciphertext, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return nil, ErrInvalidEnvelope
	}

	block, err := aes.NewCipher(v.derivedKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Reconstruct sealed slice (ciphertext + authTag)
	sealed := append(ciphertext, authTag...)
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

func (v *AESVault) EncryptString(ctx context.Context, plainText string) (string, error) {
	return v.Encrypt(ctx, []byte(plainText))
}

func (v *AESVault) DecryptString(ctx context.Context, envelope string) (string, error) {
	b, err := v.Decrypt(ctx, envelope)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
```

---

### 7.3 DAG Environment Variable Resolver (`internal/configmgr/dag.go`)

```go
package configmgr

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// Matches ${VAR_NAME} or $VAR_NAME
	varRegex = regexp.MustCompile(`\$\{([a-zA-Z0-9_]+)\}|\$([a-zA-Z0-9_]+)`)
)

type DAGResolver struct{}

func NewDAGResolver() *DAGResolver {
	return &DAGResolver{}
}

// ResolveDAG takes a key-value map and resolves all dependencies topologically.
func (d *DAGResolver) ResolveDAG(vars map[string]string) (map[string]string, error) {
	// 1. Build Adjacency List: key -> dependencies
	adj := make(map[string][]string)
	for k, v := range vars {
		deps := d.extractDependencies(v)
		adj[k] = deps
	}

	// 2. Cycle Detection via 3-color DFS
	const (
		white = 0 // unvisited
		gray  = 1 // visiting
		black = 2 // visited
	)
	colors := make(map[string]int)
	for k := range vars {
		colors[k] = white
	}

	var path []string
	var detectCycle func(node string) error
	detectCycle = func(node string) error {
		colors[node] = gray
		path = append(path, node)

		for _, dep := range adj[node] {
			if _, exists := vars[dep]; !exists {
				return fmt.Errorf("%w: variable '%s' references undefined '%s'", ErrUnresolvedVariable, node, dep)
			}

			if colors[dep] == gray {
				// Cycle detected
				cycleStart := 0
				for i, p := range path {
					if p == dep {
						cycleStart = i
						break
					}
				}
				cyclePath := strings.Join(append(path[cycleStart:], dep), " -> ")
				return fmt.Errorf("%w: %s", ErrCyclicDependency, cyclePath)
			}

			if colors[dep] == white {
				if err := detectCycle(dep); err != nil {
					return err
				}
			}
		}

		path = path[:len(path)-1]
		colors[node] = black
		return nil
	}

	for k := range vars {
		if colors[k] == white {
			if err := detectCycle(k); err != nil {
				return nil, err
			}
		}
	}

	// 3. Topological Evaluation via recursive memoization
	resolved := make(map[string]string)
	var evaluate func(k string) (string, error)
	evaluate = func(k string) (string, error) {
		if val, ok := resolved[k]; ok {
			return val, nil
		}

		rawVal := vars[k]
		// Handle literal $$ escaping
		rawVal = strings.ReplaceAll(rawVal, "$$", "__LITERAL_DOLLAR_PIKPIK__")

		expanded := varRegex.ReplaceAllStringFunc(rawVal, func(match string) string {
			depKey := strings.TrimPrefix(match, "$")
			depKey = strings.TrimPrefix(depKey, "{")
			depKey = strings.TrimSuffix(depKey, "}")

			depVal, err := evaluate(depKey)
			if err != nil {
				return match
			}
			return depVal
		})

		expanded = strings.ReplaceAll(expanded, "__LITERAL_DOLLAR_PIKPIK__", "$")
		resolved[k] = expanded
		return expanded, nil
	}

	for k := range vars {
		if _, err := evaluate(k); err != nil {
			return nil, err
		}
	}

	return resolved, nil
}

func (d *DAGResolver) extractDependencies(val string) []string {
	// Ignore escaped $$
	clean := strings.ReplaceAll(val, "$$", "")
	matches := varRegex.FindAllStringSubmatch(clean, -1)
	var deps []string
	seen := make(map[string]bool)

	for _, m := range matches {
		var dep string
		if m[1] != "" {
			dep = m[1]
		} else {
			dep = m[2]
		}
		if dep != "" && !seen[dep] {
			seen[dep] = true
			deps = append(deps, dep)
		}
	}
	return deps
}
```

---

## 8. Test Specifications & Verification Harness

### 8.1 Argon2id Verification Test (`internal/auth/argon2_test.go`)

```go
package auth_test

import (
	"testing"

	"github.com/fusuycorp/pikpik/internal/auth"
)

func TestArgon2id_HashAndVerify(t *testing.T) {
	hasher := auth.DefaultArgon2Hasher()
	password := "CorrectHorseBatteryStaple#2026"

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}

	// 1. Verify exact match
	valid, err := hasher.Verify(password, encoded)
	if err != nil || !valid {
		t.Errorf("Expected password to verify successfully, valid=%v, err=%v", valid, err)
	}

	// 2. Verify wrong password failure
	valid, err = hasher.Verify("WrongPassword!99", encoded)
	if valid {
		t.Errorf("Expected wrong password to fail verification")
	}

	// 3. Verify format integrity
	if len(encoded) == 0 || encoded[:10] != "$argon2id$" {
		t.Errorf("Invalid PHC format prefix: %s", encoded)
	}
}
```

---

### 8.2 AES-256-GCM Vault Roundtrip & Tamper Test (`internal/crypto/aes_vault_test.go`)

```go
package crypto_test

import (
	"context"
	"testing"

	"github.com/fusuycorp/pikpik/internal/crypto"
)

func TestAESVault_EncryptDecryptRoundtrip(t *testing.T) {
	ctx := context.Background()
	masterSecret := "super-secure-production-master-key-32-chars!"
	vault, err := crypto.NewAESVault(masterSecret)
	if err != nil {
		t.Fatalf("Failed to init vault: %v", err)
	}

	secretPayload := "postgres://user:pass@10.0.0.5:5432/production_db"

	envelope, err := vault.EncryptString(ctx, secretPayload)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Decrypt
	decrypted, err := vault.DecryptString(ctx, envelope)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if decrypted != secretPayload {
		t.Fatalf("Decrypted mismatch: got %q, want %q", decrypted, secretPayload)
	}

	// Verify tampering detection (GCM auth tag check)
	tampered := envelope[:len(envelope)-4] + "AAAA"
	_, err = vault.DecryptString(ctx, tampered)
	if err == nil {
		t.Fatalf("Expected decryption error on tampered ciphertext, got nil")
	}
}
```

---

### 8.3 4-Tier Hierarchy & DAG Cycle Test (`internal/configmgr/dag_test.go`)

```go
package configmgr_test

import (
	"errors"
	"testing"

	"github.com/fusuycorp/pikpik/internal/configmgr"
)

func TestDAGResolver_Success(t *testing.T) {
	resolver := configmgr.NewDAGResolver()

	input := map[string]string{
		"DB_HOST":      "postgres.internal",
		"DB_PORT":      "5432",
		"DB_USER":      "app_user",
		"DB_PASS":      "secret123",
		"DB_NAME":      "billing",
		"DATABASE_URL": "postgres://${DB_USER}:${DB_PASS}@${DB_HOST}:${DB_PORT}/${DB_NAME}",
		"API_PORT":     "8080",
		"FULL_URL":     "http://localhost:${API_PORT}/health",
		"LITERAL_ESC":  "Price is $$100",
	}

	resolved, err := resolver.ResolveDAG(input)
	if err != nil {
		t.Fatalf("Expected successful resolution, got: %v", err)
	}

	expectedDBURL := "postgres://app_user:secret123@postgres.internal:5432/billing"
	if resolved["DATABASE_URL"] != expectedDBURL {
		t.Errorf("DATABASE_URL mismatch: got %q, want %q", resolved["DATABASE_URL"], expectedDBURL)
	}

	if resolved["LITERAL_ESC"] != "Price is $100" {
		t.Errorf("Literal escape mismatch: got %q, want 'Price is $100'", resolved["LITERAL_ESC"])
	}
}

func TestDAGResolver_CycleDetection(t *testing.T) {
	resolver := configmgr.NewDAGResolver()

	input := map[string]string{
		"VAR_A": "prefix_${VAR_B}",
		"VAR_B": "mid_${VAR_C}",
		"VAR_C": "end_${VAR_A}", // Creates A -> B -> C -> A cycle
	}

	_, err := resolver.ResolveDAG(input)
	if err == nil {
		t.Fatalf("Expected cyclic error, got nil")
	}

	if !errors.Is(err, configmgr.ErrCyclicDependency) {
		t.Errorf("Expected ErrCyclicDependency, got %v", err)
	}
}
```

---

## 9. Failure Modes & Recovery Matrix

| Failure Scenario | Root Cause | Impact | Automated Recovery / Mitigation |
| :--- | :--- | :--- | :--- |
| **`SQLITE_BUSY` Lock Contention** | Multiple concurrent goroutines attempting write transactions. | Write request fails with lock error. | `PRAGMA busy_timeout = 5000` + Go `store.WithTx` mutex locks write operations to a single queue. |
| **Disk Corruption / Host Loss** | Host SSD failure or VPS termination. | Database file unavailable on local disk. | Litestream point-in-time restore downloads latest snapshot and incremental WAL frames from S3/R2 on container cold start. |
| **Database Encryption Key Mismatch** | `PIKPIK_SECRET_KEY` changed or invalid. | Decryption of secrets fails (`ErrDecryptionFailed`). | System validates master key against a known encrypted canary record (`test_canary_key`) on boot; fails fast before servicing requests. |
| **Circular Env Var Configuration** | Developer configures interdependent variables (`A -> B -> A`). | Infinite recursion during container startup. | ConfigManager DAG cycle detector rejects service deployment with `400 Bad Request` and returns explicit cycle chain path. |
| **API Token Timing Attack** | Attacker probes token validation endpoint with varying prefixes. | Potential token exfiltration. | Lookup occurs via `sha256(token)` in $O(1)$ constant time; byte comparisons use `subtle.ConstantTimeCompare`. |

---

## 10. Implementation Plan & Checklist

- [ ] Initialize Go module `github.com/fusuycorp/pikpik`.
- [ ] Implement `internal/domain` models and ULID/UUID generator.
- [ ] Implement SQLite driver connection pool and `PRAGMA` initializer (`internal/store/sqlite`).
- [ ] Write SQL migrations in `internal/store/migrations/00001_initial_schema.sql`.
- [ ] Implement `Argon2Hasher` in `internal/auth/argon2.go`.
- [ ] Implement `APITokenStore` and token generator with `pik_live_` prefix and SHA-256 hash.
- [ ] Implement `AESVault` with Scrypt KDF in `internal/crypto/aes_vault.go`.
- [ ] Implement `DAGResolver` and `ConfigManager` with 4-tier cascading overrides.
- [ ] Implement `SecretMasker` log redaction engine.
- [ ] Write comprehensive unit tests for all components with 100% test pass guarantee.
