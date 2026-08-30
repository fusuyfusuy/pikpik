# Scope 1 Architectural & Code Quality Audit Report — Core Store, Auth & Config

**Target**: `pkg/store/*`, `pkg/auth/*`, `pkg/crypto/*`, `pkg/config/*` (+ `pkg/store/migrations/*.sql`, all `*_test.go`)
**Spec Reference**: `specs/01-CORE-AND-STORE-SPEC.md`
**Date**: 2026-08-30 (supersedes `reviews/scope_1_core_store.md`, dated 18:06, which predates 5 subsequent feature commits including the Git/GitHub, backup-scheduling, and cron-scheduler work)
**Build/Test Status**: `go build ./pkg/store/... ./pkg/auth/... ./pkg/crypto/... ./pkg/config/...` — PASS. `go test` (all 4 packages) — PASS (23/23 tests).

**Health Score: 5.9 / 10.0 — CRITICAL** (clean, well-tested cryptography and interface design, undermined by three systemic invariant breaches that are silent in the current test suite and only surface under real concurrent load or an account-compromise response)

---

## 1. Executive Summary

Scope 1's cryptography (Argon2id, AES-256-GCM, Scrypt) and interface/domain modeling are implemented correctly and match the spec's RFC-level requirements precisely, with solid unit test coverage. However, the audit reproduced (empirically, not just by inspection) a **critical connection-pool PRAGMA bug**: `PRAGMA foreign_keys = ON` and `PRAGMA busy_timeout = 5000` are executed once against whichever single connection `sql.Open` happens to hand back during `store.Open()`, but `database/sql` silently opens up to 25 independent connections as load increases — and none of the later ones inherit those PRAGMAs. This means referential-integrity cascade deletes (the mechanism nearly every `Delete` method in this package depends on) and the documented `SQLITE_BUSY` mitigation are **not guaranteed** in production.

Compounding this, the `writeMu` write-serialization mutex that is threaded through every sub-store constructor is **only ever locked inside `Store.WithTx`**, and `WithTx` itself has **zero callers anywhere in the codebase** (verified via repo-wide grep). Every write path actually exercised by `pkg/auth`, `pkg/api`, and the store's own tests goes through the unprotected `dbExecutor` directly. The write-lock discipline the spec calls a primary defense against `SQLITE_BUSY` deadlocks is dead code.

A third systemic gap: the session-revocation invariant described in the spec (§3, "session revocation") does not function. `User.SessionVersion` is incremented by `UpdatePassword` but is never read by `AuthenticateUser` or `ValidateAPIToken`. Separately, `store.SessionStore` is fully implemented (schema, CRUD, indexes, tests) but `Sessions().Create` is called **nowhere** in the repository — the only session-related call site (`pkg/api/auth.go:187`, `Sessions().GetByID`) is consequently unreachable in the success case, because no row is ever written for it to find.

These three issues are all invariant-level breaches (silent data loss / unenforced concurrency safety / non-functional authentication lifecycle control), which is why the health score sits below the 7.0 "Critical" threshold despite otherwise strong code quality.

---

## 2. Standardized Audit Scorecard

| Dimension | Weight | Score | Status | Notes |
| :--- | :---: | :---: | :---: | :--- |
| **1. Correctness & Invariants** | 35% | 5.0 / 10.0 | ❌ CRITICAL | FK-pragma pool bug (empirically confirmed); `v1:`-prefix false-positive decryption; `session_version` invariant unenforced. |
| **2. Robustness & Concurrency** | 30% | 5.5 / 10.0 | ❌ CRITICAL | `writeMu`/`WithTx` write-serialization is entirely unused in production code paths; `WithTx` has no panic-safe rollback; TOCTOU race in `BootstrapAdmin`. |
| **3. Security** | 25% | 6.3 / 10.0 | ⚠️ MODERATE | Argon2id/AES-GCM/token generation are all correct; user-enumeration timing side-channel in `AuthenticateUser`; session revocation is non-functional. |
| **4. Performance & Cleanliness** | 10% | 9.0 / 10.0 | ✅ MINOR | Parameterized SQL throughout (no injection risk found anywhere in `pkg/store`), sensible indexes, minor allocation nit in `SecretMasker`. |
| **Weighted Total** | 100% | **5.9 / 10.0** | ❌ **CRITICAL** | **Not production-safe under concurrent write load or as an account-compromise control until Findings 1–3 are fixed.** |

---

## 3. Invariant Verification Matrix

| Spec Section | Invariant | Status | Evidence |
| :--- | :--- | :---: | :--- |
| §2.1 SQLite Pragmas | `foreign_keys=ON`, `busy_timeout=5000` on every connection | ❌ **FAIL** | Empirically reproduced — see Finding 1. |
| §2.1 Write Lock Discipline | Writes serialized via `store.WithTx`/`writeMu` | ❌ **FAIL** | `WithTx` has 0 callers repo-wide; sub-stores never lock `writeMu` outside it. See Finding 2. |
| §2.2 Migration Engine | Sequential, transactional, SHA-256 checksummed | ✅ PASS | `pkg/store/migrations.go:28-139`; checksum-mismatch fails startup correctly. |
| §3.1 Argon2id Parameters | t=3, m=65536KB, p=2, 16B salt, 32B key, PHC format | ✅ PASS | `pkg/crypto/argon2.go:23-31`, matches spec exactly. |
| §3.1 Timing-Safe Verify | `subtle.ConstantTimeCompare` | ✅ PASS (compare) / ❌ **FAIL** (existence) | Compare is constant-time; but `AuthenticateUser` returns early (skipping Argon2id entirely) when the user doesn't exist. See Finding 4. |
| §3 Session Revocation | Password/credential rotation invalidates existing sessions | ❌ **FAIL** | `SessionVersion` written, never read. `SessionStore.Create` never called. See Finding 3. |
| §3.2 API Tokens | `pik_live_` + 256-bit entropy, Base62(43), SHA-256 lookup | ✅ PASS | `pkg/auth/token.go:23-47`; 43×log2(62)≈256 bits confirmed. |
| §4.1 Scrypt KDF | N=32768, r=8, p=1, fixed system salt | ✅ PASS (matches spec) | `pkg/crypto/aes_vault.go:17,30`. Fixed salt is a spec'd tradeoff, flagged as residual risk in Finding 9. |
| §4.2 AES-256-GCM | Unique 96-bit IV via `crypto/rand`, 128-bit tag | ✅ PASS | `pkg/crypto/aes_vault.go:52-65`. |
| §4.3 Envelope Format | `v1:b64(iv):b64(tag):b64(ciphertext)` | ✅ PASS | Round-trip and tamper-detection tests pass. |
| §5.1 4-Tier Hierarchy | Service > Stage > Project > Org precedence | ✅ PASS | `pkg/config/manager.go:41-72`, verified by `TestConfigManager_4TierCascadingResolution`. |
| §5.2 DAG Interpolation | 3-color DFS, `${VAR}`/`$VAR`, `$$` escape, cycle path reporting | ✅ PASS | `pkg/config/dag.go`. |
| §5.3 Secret Masking | Redact secrets >3 chars, longest-first substring safety | ✅ PASS (by design) | `pkg/config/masker.go:19-37` sorts descending by length before replace — correctly avoids partial-overlap leaks. |

---

## 4. Deep-Dive Findings

### Finding 1 — [CRITICAL, Correctness/Data-Integrity] Per-connection PRAGMAs are dropped on every pooled connection but one
**Location**: `pkg/store/sqlite.go:44-87`

`Open()` calls `db.ExecContext(ctx, p)` for each pragma (`journal_mode`, `foreign_keys`, `busy_timeout`, `synchronous`, `journal_size_limit`, `mmap_size`, `temp_store`, `cache_size`) against the bare `*sql.DB`. In SQLite, only `journal_mode` is persisted in the database file header; every other pragma listed here (`foreign_keys`, `busy_timeout`, `synchronous`, `mmap_size`, `temp_store`, `cache_size`) is **per-connection session state**. `db.SetMaxOpenConns(25)` (line 73) means `database/sql` will lazily open up to 25 independent physical connections as concurrent callers arrive — none of which re-run these pragmas.

**Machine-verifiable proof** (reproduced against the actual `sql.Open("sqlite", dsn)` call pattern from `pkg/store/sqlite.go`, using `modernc.org/sqlite v1.57.0` — the exact driver pinned in `go.mod`):

```
conn[0]: foreign_keys=1 busy_timeout=5000
conn[1]: foreign_keys=0 busy_timeout=0
conn[2]: foreign_keys=0 busy_timeout=0
conn[3]: foreign_keys=0 busy_timeout=0
conn[4]: foreign_keys=0 busy_timeout=0
CONFIRMED BUG: at least one pooled connection has foreign_keys=OFF despite Open() executing 'PRAGMA foreign_keys = ON'
```

**Impact**:
- Every `ON DELETE CASCADE` in `pkg/store/migrations/*.sql` (orgs→projects→stages→services→volumes/env_vars/deployments/builds/backup_configs/backup_schedules/github_installations, sessions/api_tokens on user delete) is only enforced on whichever connection happens to be pinned as connection #0. Deleting a project, org, or service on any other pooled connection silently leaves orphaned child rows — a genuine silent-data-loss/data-integrity class bug, not merely a missing constraint.
- `busy_timeout=0` on non-primary connections means the documented "wait up to 5000ms on lock contention" (spec §2.1, and Failure Mode Matrix §9 row 1) does not apply to most connections — they fail immediately with `SQLITE_BUSY` instead, exactly the failure mode the spec claims is mitigated.
- The test suite (`pkg/store/store_test.go`) uses a real file-backed DB via `t.TempDir()`, so this is not masked by `:memory:` semantics — it's masked only because sequential single-goroutine tests happen to reuse connection #0 and never force the pool to open a second one. No test exercises cascade-delete correctness or forces >1 concurrent connection.

**Remediation**: Pass pragmas via the DSN, which `modernc.org/sqlite` applies to every new connection (confirmed present in the driver source, `sqlite.go:305-433`, `_pragma` query parameter):
```go
dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=journal_size_limit(67108864)&_pragma=mmap_size(268435456)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-64000)&_pragma=journal_mode(WAL)", dsn)
db, err := sql.Open("sqlite", dsn)
```
Add a regression test that forces `db.SetMaxOpenConns(1)` off (i.e., keep 25) and opens ≥2 concurrent `db.Conn(ctx)` handles, asserting `PRAGMA foreign_keys` reads `1` on all of them.

---

### Finding 2 — [CRITICAL, Robustness/Concurrency] `writeMu` write-serialization is dead code; `WithTx` has zero production callers
**Location**: `pkg/store/sqlite.go:89-159` (constructor + `WithTx`); every `sql*Store` type in `pkg/store/*.go` (e.g. `orgs.go:12-15`, `users.go:12-15`, `tokens.go:13-16`, `services.go:13-16`, `env_vars.go:12-15`)

Every sub-store struct (`sqlOrgStore`, `sqlUserStore`, `sqlAPITokenStore`, `sqlProjectStore`, `sqlServiceStore`, `sqlEnvVarStore`, `sqlVolumeStore`, `sqlDeploymentStore`, `sqlBackupStore`, `sqlAuditStore`, `sqlBuildStore`, `sqlGitHubInstallationStore`, `sqlScheduleStore`) carries a `writeMu *sync.Mutex` field, giving the strong visual impression that writes are serialized. A repo-wide check shows the mutex is locked in exactly one place:
```
pkg/store/sqlite.go:140:	s.writeMu.Lock()
pkg/store/sqlite.go:141:	defer s.writeMu.Unlock()
```
— inside `SQLiteStore.WithTx`. A second repo-wide check (`grep -rln "WithTx" --include="*.go" .`) shows `WithTx` itself is referenced only in its own definition (`sqlite.go`, `store.go`) and in tests (`store_test.go:TestStore_WithTx`, `build_store_test.go:TestBuildStore_TransactionRollback`). **No production code path — not `pkg/auth`, not `pkg/config`, not `pkg/api` — ever calls `WithTx`.** Every `Create`/`Update`/`Delete`/`Set` call observed in `pkg/auth/auth_service.go` and elsewhere goes straight through the plain `dbExecutor`, unserialized.

**Impact**: Combined with Finding 1's per-connection `busy_timeout=0`, there is currently no effective mitigation for `SQLITE_BUSY` under concurrent writes anywhere in the running system — the two mechanisms the spec names as the mitigation (busy_timeout pragma, write mutex) are each individually broken, and neither backstops the other. This is the kind of gap that manifests as production crash-loops/500s under real multi-request write concurrency but never in a single-threaded test run.

**Remediation**: Either (a) have every sub-store write method acquire `s.writeMu.Lock()`/`defer Unlock()` when not already inside a `Tx` (`writeMu != nil` check), or (b) remove the unused mutex plumbing entirely and rely solely on the DSN-level `busy_timeout` from Finding 1 plus SQLite's own locking — but not leave both present and inert, which is strictly worse than either alone (false sense of safety).

---

### Finding 3 — [CRITICAL, Security] Session revocation invariant is non-functional
**Location**: `pkg/store/users.go:111-133` (`UpdatePassword`), `pkg/auth/auth_service.go:58-73` (`AuthenticateUser`), `pkg/auth/auth_service.go:117-145` (`ValidateAPIToken`), `pkg/store/sessions.go` (entire file), `store.User.SessionVersion` (`pkg/store/models.go:34`)

Two independent facts, both confirmed by repo-wide grep:
1. `session_version = session_version + 1` is written by `UserStore.UpdatePassword` (`pkg/store/users.go:115`), but `SessionVersion` is never read anywhere outside `pkg/store` itself — not in `AuthenticateUser`, not in `ValidateAPIToken`, not in `pkg/api`. The field exists purely to be incremented; nothing ever consults it to reject a stale credential.
2. `SessionStore.Create` (`pkg/store/sessions.go:17-28`) is never called anywhere in the repository. The only session-related call site in the entire codebase is `st.Sessions().GetByID(ctx, rawSecret)` in `pkg/api/auth.go:187` — which, since no row is ever inserted, always returns `store.ErrNotFound` in production. The `sessions` table, its two indexes, and its full CRUD implementation are effectively dead code; real browser-session auth is implemented elsewhere as a non-expiring, wildcard-scoped API token (`pkg/api/controller.go:211`, `CreateAPIToken(ctx, user.ID, "Web Session", []string{"*"}, nil)`).

**Impact**: There is currently no way, anywhere in this codebase, to revoke a live admin session or previously-issued API token as a response to a password change or suspected compromise — the exact scenario `session_version` and `SessionStore` exist to solve per spec §3. This is a genuine security-control gap, not merely dead code: an attacker who has obtained a valid session/token retains access indefinitely across password rotations.

**Remediation**: Either wire `ValidateAPIToken`/session validation to compare a per-token/session `session_version` snapshot against the live `User.SessionVersion` and reject on mismatch, or — if the "Web Session = wildcard API token" design is intentional — bind `CreateAPIToken`'s issued tokens to `user.SessionVersion` at mint time and check it on every `ValidateAPIToken` call, and finally either wire up `SessionStore` or remove it (Ponytail: delete unused code rather than leave a second, unreachable auth mechanism installed).

---

### Finding 4 — [MODERATE, Security] User-enumeration timing side-channel in `AuthenticateUser`
**Location**: `pkg/auth/auth_service.go:58-73`

```go
user, err := s.store.Users().GetByEmail(ctx, email)
if err != nil {
    if errors.Is(err, store.ErrNotFound) {
        return nil, ErrInvalidCredentials      // <- fast path, no hashing
    }
    ...
}
valid, err := s.hasher.Verify(password, user.PasswordHash)  // <- ~tens of ms (Argon2id, 64MB/t=3)
```
When the email doesn't exist, the function returns almost immediately. When it exists, `Argon2Hasher.Verify` runs a full Argon2id computation (64MB memory cost, t=3) before returning. This is a classic, measurable timing side-channel that lets an unauthenticated caller enumerate registered admin emails purely from response latency — notable here because pikpik is explicitly a single/few-admin system, so email enumeration materially narrows the attack surface for credential stuffing.

**Remediation**: On `ErrNotFound`, still run `hasher.Verify(password, <fixed dummy PHC hash>)` (or hash a fixed dummy string) before returning `ErrInvalidCredentials`, so both branches take comparable wall-clock time.

---

### Finding 5 — [MODERATE, Correctness] `ResolveHierarchy` treats any plaintext value starting with `"v1:"` as an encrypted envelope
**Location**: `pkg/config/manager.go:58-71`

```go
decryptedValue := v.ValueEncrypted
if strings.HasPrefix(v.ValueEncrypted, "v1:") && m.vault != nil {
    decrypted, err := m.vault.DecryptString(ctx, v.ValueEncrypted)
    if err != nil {
        return nil, fmt.Errorf("config: failed to decrypt secret key %q: %w", v.Key, err)
    }
    decryptedValue = decrypted
}
```
This branch is keyed only on the string prefix, not on `v.IsSecret`. A legitimate, non-secret env var value that happens to start with the literal string `v1:` (e.g. `API_VERSION=v1:stable`, `TAG=v1:beta`) will be misinterpreted as an AES-GCM envelope, fail `gcm.Open` (auth tag won't match, since it isn't really an envelope), and abort the **entire** hierarchy resolution with a hard error — a functional denial-of-service for that deployment/service triggered purely by an innocuous plaintext value.

**Remediation**: Gate decryption on `v.IsSecret && strings.HasPrefix(...)`, matching the write-side invariant that only secret values are ever encrypted.

---

### Finding 6 — [MODERATE, Robustness] `WithTx` leaks the transaction/connection on panic
**Location**: `pkg/store/sqlite.go:134-159`

```go
func (s *SQLiteStore) WithTx(ctx context.Context, fn func(tx Store) error) error {
    ...
    s.writeMu.Lock()
    defer s.writeMu.Unlock()
    tx, err := s.db.BeginTx(ctx, nil)
    ...
    if err := fn(txStore); err != nil {
        _ = tx.Rollback()
        return err
    }
    if err := tx.Commit(); err != nil { ... }
    return nil
}
```
If `fn(txStore)` panics (e.g. a nil-pointer bug in caller business logic), `writeMu.Unlock()` runs via `defer`, but `tx.Rollback()` does not — it's only reached through the normal `if err != nil` path. The panic propagates up holding an open, uncommitted transaction bound to a checked-out `*sql.Conn`, which is never returned to the pool. Repeated occurrences slowly exhaust the (currently 25-connection) pool. Note: this finding becomes materially more likely once `WithTx` actually gets wired into call sites per Finding 2's remediation.

**Remediation**: `defer tx.Rollback()` immediately after a successful `BeginTx` (a no-op error, safely ignorable, once `Commit` has already succeeded).

---

### Finding 7 — [MODERATE, Robustness] TOCTOU race in `BootstrapAdmin`
**Location**: `pkg/auth/auth_service.go:29-56`

```go
count, err := s.store.Users().Count(ctx)
...
if count > 0 { return nil, ErrAdminAlreadyExists }
hash, err := s.hasher.Hash(password)   // ~tens of ms, Argon2id
...
if err := s.store.Users().Create(ctx, user); err != nil { ... }
```
Two concurrent `BootstrapAdmin` calls (e.g. an operator double-submitting the setup form, or two setup requests racing during first boot) can both observe `count == 0` before either `Create` completes, since there is no unique constraint preventing a second "owner" row (only `email` is unique, and the two calls may use different emails) and no locking around the check-then-act sequence. Result: two "owner" role users instead of the single-admin invariant the spec assumes.

**Remediation**: Wrap the count-check-and-create in `s.store.WithTx` (once WithTx's own issues from Findings 1/2/6 are fixed), or add an application-level singleton guard (e.g. a `UNIQUE` partial index / sentinel row) enforced at the DB layer rather than the Go layer.

---

### Finding 8 — [MINOR, Performance] `SecretMasker.Mask` allocates one intermediate string per secret
**Location**: `pkg/config/masker.go:40-50`

```go
func (m *multiStringMasker) Mask(input string) string {
    result := input
    for _, secret := range m.secrets {
        result = strings.ReplaceAll(result, secret, "[REDACTED]")
    }
    return result
}
```
This is called on every log line passing through the deployment/WebSocket logger per spec §5.3 — a hot path. Each `ReplaceAll` call allocates a new string even when no match is found. For N secrets this is N allocations/scans per line regardless of hit rate.

**Remediation**: Build a single `strings.NewReplacer` once in `NewSecretMasker` (already has the sorted, deduplicated `secrets` slice available) and call it once per `Mask` invocation.

---

### Finding 9 — [MINOR, Correctness] `builds.UpdateStatus` cannot clear `error_message`/`image_tag`/`duration_ms` back to zero values
**Location**: `pkg/store/build_store.go:155-172`

```sql
error_message = CASE WHEN ? != '' THEN ? ELSE error_message END,
image_tag     = CASE WHEN ? != '' THEN ? ELSE image_tag END,
duration_ms   = CASE WHEN ? > 0  THEN ? ELSE duration_ms END,
```
A caller can never intentionally reset `error_message`/`image_tag` to `""` (e.g. a retried build succeeding after a prior failure recorded an error) or `duration_ms` to `0` through this method — an empty/zero argument is indistinguishable from "leave unchanged." Low impact today since retries appear to create new `Build` rows rather than reuse one, but it's a latent trap for the next caller.

**Remediation**: Use `sql.NullString`/pointer semantics (as `DeploymentStore.UpdateStatus` already correctly does with `*time.Time`/`*string` + `COALESCE`) so "unset" is representable independent of the zero value.

---

### Finding 10 — [MINOR/Residual-Risk, Security] Fixed Scrypt salt is a spec'd but structurally weak KDF choice
**Location**: `pkg/crypto/aes_vault.go:17` (`defaultVaultSalt = []byte("pikpik-vault-master-salt-v1")`)

This matches the spec exactly (§4.1: "Fixed system salt") and is not a deviation, so it is not scored as a correctness bug — but it is worth recording as residual risk: every pikpik installation using the same `PIKPIK_SECRET_KEY` low-entropy value derives an identical AES key (no per-install salt), and the fixed salt is public (it's in the source). If `PIKPIK_SECRET_KEY` entropy is ever below what the 16-character length check (`aes_vault.go:26`) implies, this KDF configuration offers less protection than a per-install random salt would. Recommend either deriving/storing a random per-install salt in `schema_migrations`/a system table at first boot, or explicitly documenting the entropy requirement on `PIKPIK_SECRET_KEY` more strongly than "≥16 characters" (16 low-entropy characters is not 16 bytes of entropy).

---

## 5. Findings Not Reproduced From the Prior (Superseded) Report

The stale `reviews/scope_1_core_store.md` (18:06) previously flagged the same Finding 1 and Finding 2 root causes (confirmed independently in this pass and now backed by an executable repro), the same timing side-channel (Finding 4), the same `v1:` false-positive (Finding 5), the same `WithTx` panic-safety gap (Finding 6), and the same `BootstrapAdmin` race (Finding 7) — **none of these have been fixed across the 5 intervening feature commits**; they are unrelated to git/build/backup-scheduler work and were simply not addressed. This pass additionally surfaces the session-revocation gap (Finding 3), which the prior report did not identify, by tracing `SessionVersion` and `SessionStore.Create` call sites end-to-end.

---

## 6. Remediation Priority

1. **(Critical)** Move all SQLite pragmas to the DSN (`_pragma=...`) so every pooled connection inherits them — Finding 1.
2. **(Critical)** Decide and implement one write-serialization story: either lock `writeMu` in every sub-store write path, or delete the unused mutex plumbing — Finding 2.
3. **(Critical)** Wire `session_version` (or equivalent) into `ValidateAPIToken`/session validation so credential rotation actually revokes prior access; either activate or delete `SessionStore` — Finding 3.
4. **(Moderate)** Normalize `AuthenticateUser` timing with a dummy-hash fallback — Finding 4.
5. **(Moderate)** Gate `ResolveHierarchy`'s decryption branch on `v.IsSecret` — Finding 5.
6. **(Moderate)** Add `defer tx.Rollback()` in `WithTx`; wrap `BootstrapAdmin` in a transaction/DB-level singleton guard — Findings 6, 7.
7. **(Minor)** `strings.NewReplacer` in `SecretMasker`; nullable fields in `builds.UpdateStatus`; document/harden `PIKPIK_SECRET_KEY` entropy expectations — Findings 8, 9, 10.
