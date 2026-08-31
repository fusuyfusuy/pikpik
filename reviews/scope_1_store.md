# Scope 1 Audit — `pkg/store` Persistence Boundary

**Auditor**: Store & Persistence Auditor
**Scope**: `pkg/store/*.go`, `pkg/store/migrations/*.sql`, `pkg/store/*_test.go`, plus persistence-adjacent callers (`pkg/config`, `pkg/templates`, `pkg/api`, `pkg/backup`, `pkg/telemetry`) where they cross the store boundary.
**Verification**: `go test -count=1 ./pkg/store/...` → **exit 0** (`ok github.com/fusuycorp/pikpik/pkg/store 0.248s`).

---

## Overall Health Score: **8.0 / 10** (Moderate — dragged down by one Critical security finding)

| Dimension | Score | Verdict |
|---|---|---|
| 1. Correctness | 7.6 | Moderate |
| 2. Robustness | 8.6 | Minor |
| 3. Performance | 8.8 | Minor |
| 4. Security | 6.4 | **Critical** |
| 5. Invariant 2 Compliance | 9.8 | Exemplary |

---

## 1. Correctness (7.6 — Moderate)

### Strengths
- **Schema ↔ model alignment is exact.** The composed schema (00001 + ALTERs in 00005–00008) produces a 22-column `services` table matching the 22-column SELECT/INSERT lists in `services.go:56-59,77` field-for-field. Same discipline in `tokens.go` (00005 `session_version`), `build_store.go`, `schedule_store.go` (`scheduleSelectCols`, `schedule_store.go:15-20`).
- **Migration runner is sound**: checksum-pinned (SHA-256, `migrations.go:69-70`), enforced on re-open (`migrations.go:109-112` → `ErrMigrationChecksumMismatch`), each migration applied in its own transaction (`migrations.go:117-135`); SQLite DDL is transactional so no partial-migration state is possible.
- **Consistent `sql.ErrNoRows` → `ErrNotFound`** in every Get path (e.g. `services.go:116-118`, `users.go:70-72`, `deployments.go:57-59`) and **`RowsAffected()==0` → `ErrNotFound`** on Update/Delete (e.g. `services.go:245-247`, `users.go:127-129`).
- **NULL handling is deliberate**: nullable columns scanned via `sql.NullString`/`sql.NullTime` (`deployments.go:49-50`, `build_store.go:78-79`, `schedule_store.go:27`); `builds.deployment_id` correctly written as `sql.NullString` on insert (`build_store.go:45-48`).
- **ID generation is solid**: 48-bit ms timestamp + 80-bit `crypto/rand` (`id.go:12-30`) — negligible collision probability.


### Findings

**C-1 (Moderate) — `ErrDuplicateKey`, `ErrOptimisticLock`, `ErrTransactionClosed` are dead sentinels.** Defined at `store.go:14-21` but **never returned anywhere** (grep: only definitions match). Every `Create` (`users.go:47-49`, `orgs.go:29-31`, `projects.go:41-43`, `services.go:69-71`, `stages.go:29-31`, `tokens.go:45-47`) wraps the raw sqlite error verbatim. Consequences: (a) API cannot map duplicate email/slug to HTTP 409 — surfaces as 500; (b) raw message (`UNIQUE constraint failed: users.email`) leaks schema internals if propagated.

**C-2 (Moderate) — Orphaned `env_vars` rows on service/project/org deletion.** `env_vars.resource_id` is polymorphic (`scope_tier` discriminator, `00001_initial_schema.sql:118-128`), so no FK is possible — and no compensating cleanup exists. `Services().Delete` is called at `pkg/api/controller.go:1030,2231` and `pkg/templates/deployer.go:538` with no env-var sweep. Worse, `EnvVars().Delete` (`env_vars.go:104-118`) has **zero callers anywhere** — secrets, once set, can never be removed through any code path.

**C-3 (Minor) — `audit_logs.user_id` written as `''` instead of `NULL`.** `audit.go:25` passes the `userID` string straight through; the column is nullable (`00001:208`) and the read path expects NULL (`audit.go:50` `sql.NullString`). Anonymous actions create two distinct "no user" representations. (Moot today — see S-3: nothing calls `Record`.)

**C-4 (Minor) — Stage deletion cascade-destroys all contained services.** `services.stage_id TEXT NOT NULL ... ON DELETE CASCADE` (`00001:94,107`) + `stages.go:84-98` means deleting a stage wipes every service in it (and their deployments, builds, volumes, backup configs via cascades) — in tension with the domain spec ("Stage: Optional & Nonbinding", AGENTS.md §1).

**C-5 (Minor) — Redundant secondary indexes duplicating UNIQUE auto-indexes.** `idx_users_email` (`00001:25` vs `UNIQUE` line 15), `idx_projects_slug` (`00001:76` vs line 68), `idx_api_tokens_lookup` (`00001:57` vs line 49), `idx_services_lookup` (`00001:112` vs `UNIQUE(project_id,stage_id,slug)` line 108), `idx_github_installations_inst` (`00003` vs `installation_id UNIQUE`). Pure write amplification and WAL bloat.

**C-6 (Minor) — Migration idempotency claim not honored for ALTERs.** AGENTS.md Stage-1 mandates `IF NOT EXISTS` forward idempotency; `00005`–`00008` use bare `ALTER TABLE ... ADD COLUMN`, relying solely on `schema_migrations` tracking. Safe in the single-binary model. `00007`'s backfill `UPDATE services ... WHERE project_id IS NULL` is dead code (`project_id` is `NOT NULL` since `00001:93`) — harmless.

**C-7 (Minor) — `NewID` ignores `crypto/rand` failure** (`id.go:24`: `_, _ = rand.Read(...)`); fail-fast would panic. Practically unreachable on Linux. Prefix `svc_` also diverges from the documented `app_/srv_/db_` taxonomy (cosmetic).

---

## 2. Robustness (8.6 — Minor)

### Strengths
- **Per-connection pragma strategy is the standout of this package.** `sqlite.go:59-65` encodes `foreign_keys(1)`, `busy_timeout(5000)`, `synchronous(NORMAL)`, `journal_size_limit`, `mmap_size`, `temp_store(MEMORY)`, `cache_size(-64000)` as `_pragma` DSN params, which modernc.org/sqlite re-applies on **every** pooled connection — the comment at `sqlite.go:45-58` shows precise understanding of the database/sql pooling hazard. `journal_mode=WAL` is correctly identified as a file-header property set once (`sqlite.go:88-96`).
- **Proven by test**: `TestStore_PragmasAppliedOnEveryPooledConnection` (`store_test.go:43-89`) pins 12 concurrent pooled connections and asserts `busy_timeout=5000` and `foreign_keys=1` on each.
- **WithTx is correct where it exists**: panic → rollback → re-panic (`sqlite.go:175-180`), error → rollback (`sqlite.go:183-186`), nested-tx passthrough via `isTx` (`sqlite.go:166-169`). Covered by `TestStore_WithTx` (`store_test.go:335-370`).
- **Pool tuning is sane**: MaxOpenConns 25 / MaxIdle 5 / lifetime 1h / idle 15m (`sqlite.go:99-102`). Every query forwards `ctx` — cancellation propagates.

### Findings

**R-1 (Moderate) — `WithTx` has zero production callers.** Grep across `pkg/`, `cmd/`: only tests use it (`store_test.go:342,359`, `build_store_test.go:202`). Multi-write flows like template deploy (`pkg/templates/deployer.go:155-201`: Create service → N×Set env vars → N×Create volumes → Create deployment, each error swallowed with `_ =`) are entirely non-atomic. A crash mid-deploy leaves a service with partial env/volumes and no deployment record. The transaction machinery was built and tested, then never wired in.

**R-2 (Minor) — Swallowed errors at the persistence boundary in callers.** `pkg/templates/deployer.go` uses `_ = d.st....` on every store call (lines 155, 176, 188, 192); `pkg/api/controller.go:436,649` discards `ListAll` errors (`allSvcs, _ :=`). Defeats the store's careful error wrapping.

**R-3 (Minor) — `Ping` on a transaction-scoped store silently returns nil** (`sqlite.go:195-198`). Semantic oddity; unreachable in practice.

**R-4 (Minor) — No migration application lock.** Two racing `Open()` calls (CLI + server on same file) could double-apply; `busy_timeout` turns this into a transient error rather than corruption, and single-binary makes it unlikely. Acceptable.

---

## 3. Performance (8.8 — Minor)

### Strengths
- Hot paths are indexed: `idx_deployments_service(service_id, started_at DESC)` (`00001:172`) exactly matches `deployments.go:76-78`; `idx_builds_service(service_id, started_at DESC)` matches `build_store.go:111-115`; `idx_backup_schedules_status(is_enabled, next_run_at)` matches the scheduler's `ListDue` (`schedule_store.go:179`); `idx_env_vars_resource` covers both env-var queries; `idx_sessions_expires_at` covers `CleanExpired` (`sessions.go:68-75`).
- **No N+1**: controller list endpoints do one `ListAll` + in-memory aggregation (`pkg/api/controller.go:436-442`, `649-664`), not per-entity queries.
- Bounded reads on history endpoints via `LIMIT ?` with sane defaults (`deployments.go:71-80`, `build_store.go:106-117`, `backups.go:134-143`, `audit.go:32-41`).
- Write-side pragmas (synchronous=NORMAL, 64MB journal limit, 256MB mmap, 64MB cache) appropriate for single-node WAL.

### Findings

**P-1 (Minor) — Telemetry retention DELETE is a full-table scan.** `pkg/telemetry/downsampler.go:155` `DELETE FROM system_metrics_hourly WHERE bucket_start < ?` — the only index is `(entity_type, entity_id, bucket_start)` (`00002:22-23`), whose leftmost column isn't `bucket_start`, so every retention sweep scans the whole growing table. Add `CREATE INDEX idx_metrics_bucket ON system_metrics_hourly(bucket_start)`.

**P-2 (Minor) — Unbounded `ListAll` scans.** `services.go:164-170` (22 wide columns incl. `compose_yaml`), `networks.go:107-127`, `stacks.go:93-110`, `machines.go:76-102`. Fine at self-hosted scale (hundreds of rows); degrades unboundedly with no `# ponytail:` ceiling note.

**P-3 (Minor) — Missing FK-side indexes for cascade deletes.** `builds.deployment_id` and `backup_executions.config_id` have no index; deleting a deployment/backup-config scans those tables. Low cardinality → low impact.

**P-4 (Minor) — Redundant indexes** (C-5) also cost write throughput on every insert into `users`, `projects`, `api_tokens`, `services`, `github_installations`.

**P-5 (Minor) — No upper cap on caller-supplied `limit`.** `audit.go:33-35`, `deployments.go:72-74` etc. floor at 50 but never ceiling; `limit=10000000` is honored verbatim.

---

## 4. Security (6.4 — Critical)

### Strengths
- **Zero SQL injection surface.** Every query is parameterized; the only `fmt.Sprintf` query construction uses the package-private constant `scheduleSelectCols` (`schedule_store.go:135-179`). No user input ever reaches query text.
- **Credentials at rest are hashed where hashing is possible**: `users.password_hash` (Argon2, `cmd/pikpik/main.go:188`), `sessions.id` = SHA-256 of token (`models.go:41`), `api_tokens.token_hash` SHA-256 (`models.go:55`), `services.deploy_token_hash` (`00001:102`). Raw tokens never touch the DB.
- **JSON marshaling keeps secrets out of API responses**: `json:"-"` on `PasswordHash`, `TOTPSecret`, `TokenHash`, `DeployTokenHash`, `PasswordEncrypted`, `S3SecretKeyEncrypted` (`models.go:30,32,55,97,200,205`).
- **Audit table is append-only by design** — no Update/Delete on `AuditStore` (`audit.go`), no FK to users so entries survive user deletion (`00001:206-215`).

### Findings

**S-1 (CRITICAL) — Encryption at rest is never invoked; "encrypted" columns hold plaintext.** `crypto.Vault.EncryptString` (`pkg/crypto/aes_vault.go:121`) has **zero callers in the entire codebase**. The vault is constructed in `cmd/pikpik/main.go:194` and only ever used for *decryption* (`pkg/config/manager.go:60-61`, gated on a `v1:` prefix nothing ever produces). Concrete evidence:
- `pkg/templates/deployer.go:176-183` writes raw resolved env values straight into `EnvVar.ValueEncrypted` — including values flagged `IsSecret: true`.
- `backup_configs.s3_secret_key_encrypted` / `backup_schedules.s3_secret_key_encrypted` / `password_encrypted` are written from API input (`pkg/api/controller.go:2440`, `pkg/backup/scheduler.go:498`) with no encryption step.

The `_encrypted` column suffix is a false promise: **the SQLite file contains every env secret, DB password, and S3 secret key in cleartext.** This breaches the security/data-loss non-negotiable.

**S-2 (High) — `s3_access_key` is plaintext by schema design.** `backup_configs.s3_access_key TEXT NOT NULL` (`00001:180`), `backup_schedules.s3_access_key` (`00004`) — half of a credential pair, and exposed in API JSON (`models.go:203` has only `omitempty`, no `json:"-"`).

**S-3 (High) — Audit trail is completely dead code.** `Audit().Record` (`audit.go:14`) has **zero callers** anywhere in `pkg/` or `cmd/`; no file outside `pkg/store` even contains the string "audit". For a multi-user PaaS with RBAC, every mutating action (deploy, delete service, rotate token, change password) is currently unaudited — the table stays empty forever. Forensics/compliance gap.

**S-4 (Moderate) — Orphaned secret rows are undeletable** (compounds C-2): env vars can be written but never deleted (`EnvVars().Delete` unused), so S-1's plaintext persists even after the owning service is destroyed.

**S-5 (Minor) — Wrapped constraint errors leak schema detail** upstream (C-1): `UNIQUE constraint failed: users.email` will surface in API error bodies; the API can't sanitize because the store never translates to `ErrDuplicateKey`.

**S-6 (Minor) — `user_id` empty-string vs NULL in audit** (C-3) weakens forensic attribution queries (`WHERE user_id IS NULL` misses `''`).

---

## 5. Invariant 2 Compliance (9.8 — Exemplary)

| Requirement | Status | Evidence |
|---|---|---|
| SQLite WAL sole source of truth | ✅ | `store.Open` is the only DB open path (`cmd/pikpik/main.go:181`); `PRAGMA journal_mode = WAL` at `sqlite.go:92-96`; zero external daemons |
| `PRAGMA foreign_keys=ON` on every pooled connection | ✅ | DSN `_pragma=foreign_keys(1)` (`sqlite.go:59`), test-proven on 12 pinned connections (`store_test.go:78-85`) |
| `PRAGMA busy_timeout=5000` on every pooled connection | ✅ | DSN `_pragma=busy_timeout(5000)` (`sqlite.go:60`), test-proven (`store_test.go:69-76`) |
| In-process workers/schedulers on same pool | ✅ | Downsampler/scheduler share the pooled `*sql.DB` |

Deduction (-0.2): journal_mode isn't verified post-set (in-memory DSNs silently stay in `memory` mode). Strongest dimension of the package — the DSN-pragma approach with a dedicated concurrency test is textbook-correct handling of a notorious SQLite+Go pitfall.

---

## Consolidated Remediation Plan

### P1 — Critical (fix before next release)
1. **Wire encryption on the write path (S-1).** Route env-var values, `password_encrypted`, and both S3 secret keys through `vault.EncryptString` at the API/service boundary (`pkg/api/controller.go:2440`, `pkg/templates/deployer.go:176`, `pkg/backup/scheduler.go:498`). The decrypt path already exists — a missing-call bug, not missing infrastructure. Add a test asserting persisted bytes carry the `v1:` prefix.
2. **Activate the audit trail (S-3).** Call `Audit().Record` from mutating controller methods (or one middleware wrapper) — store, schema, and indexes already exist.

### P2 — Moderate
3. **Translate constraint violations (C-1/S-5).** Map sqlite extended code 2067 to `ErrDuplicateKey` in a shared `mapWriteErr` helper at every Create/Update; delete or implement `ErrOptimisticLock`/`ErrTransactionClosed`.
4. **Make multi-write flows transactional (R-1).** Wrap template deploy (`pkg/templates/deployer.go:155-201`) and service-delete cascades in `WithTx`; stop swallowing store errors with `_ =` (R-2).
5. **Env var lifecycle (C-2/S-4).** Add `DeleteByResource(tier, resourceID)` to `EnvVarStore`, call from service/project delete paths inside the same `WithTx`; expose a delete endpoint.
6. **Encrypt `s3_access_key` too (S-2)** or justify with a `# ponytail:` ceiling; add `json:"-"` to the API-exposed field.

### P3 — Minor
7. Add `idx_metrics_bucket ON system_metrics_hourly(bucket_start)` (P-1); drop the five redundant UNIQUE-shadowing indexes (C-5/P-4).
8. Normalize audit `userID` `""` → NULL at `audit.go:14-25` (C-3/S-6).
9. Ceiling caller-supplied `limit` (e.g. 500) in List* methods (P-5); `# ponytail:` markers on unbounded `ListAll`s (P-2).
10. Panic on `crypto/rand` failure in `NewID` (C-7); guard stage deletion when services exist, or document the cascade (C-4).
11. Add idempotency guards or a documented non-idempotency note to ALTER migrations (C-6).

---

*Verification: `mimori dump --file` warmup; `mimori slice` on `sqlite.go`/`store.go`; leaf reads of all 20 non-test store files and all 10 migration files; targeted greps for injection/`WithTx`/audit/encryption callers; `go test -count=1 ./pkg/store/...` (exit 0).*
