# Scope 1 Auditor Report: Storage & Security Core

## Executive Summary
**Health Score:** 9.6 / 10.0 (Exemplary)

The storage and security core of `pikpik` demonstrates a remarkably solid architectural foundation aligned with the stated engineering invariant of a single unified runtime (SQLite WAL mode). The encryption boundaries and query models are robust and structurally sound.

### Invariant Breaches
* **Zero Breaches Found.** The core abides strictly by the "Unified Control Plane" and "Parse at the Boundary, Trust in the Core" principles. 

### Top Findings
1. **SQLite WAL & Pragma Connection Resilience (Correctness/Robustness)** 
   - *File:* `pkg/store/sqlite.go:34-40`
   - *Observation:* The use of `perConnectionPragmaParams` encoded directly into the DSN string (`_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)`) successfully guarantees that every new connection spawned by `database/sql` inherits the required WAL performance and concurrency attributes. The `PRAGMA journal_mode = WAL;` is safely executed once on the db connection.
   - *Impact:* Exemplary implementation avoiding the classic SQLite connection-pooling trap in Go.

2. **AES-256-GCM Authenticated Vault (Security)**
   - *File:* `pkg/crypto/aes_vault.go:61-75`
   - *Observation:* Field-level encryption properly implements authenticated encryption with associated data (AEAD) via AES-GCM. Unique 96-bit (12-byte) IVs are correctly sourced from `crypto/rand` per operation, and keys are securely derived using Scrypt with strong parameters (`N=32768, r=8, p=1`).
   - *Impact:* Data at rest encryption is secure against tampering and ciphertext substitution.

3. **Argon2id Password Hashing (Security)**
   - *File:* `pkg/crypto/argon2.go:20`
   - *Observation:* Passwords use RFC 9106 Argon2id with 64MB memory, 3 iterations, and 2 degrees of parallelism. Validation leverages `subtle.ConstantTimeCompare` preventing timing side-channels.
   - *Impact:* Identity management is resilient to offline cracking and meets modern compliance standards.

4. **100% Parameterized SQL / No Injection Vectors (Security)**
   - *Files:* `pkg/store/*.go`
   - *Observation:* All user inputs and dynamic identifiers are securely passed as positional arguments (`?`) to `db.ExecContext` and `db.QueryContext`. 
   - *Note:* The only `fmt.Sprintf` usage within the query builder (`pkg/store/schedule_store.go:138`) safely interpolates a predefined, non-user-controlled constant (`scheduleSelectCols`).

5. **Idempotency in Migrations (Correctness)**
   - *Files:* `pkg/store/migrations/*.sql`
   - *Observation:* Schema migrations consistently utilize `IF NOT EXISTS` for table and index creation, along with appropriate `ON DELETE CASCADE` constraints matching the domain object lifecycle.

### Actionable Remediations
- **Review Database Connection Pooling Strategy (Performance):** While `MaxOpenConns(25)` is sufficient for most workloads, consider exposing this via environment variables or configuring it dynamically based on the available CPU cores to maximize SQLite WAL throughput.
- **Audit Logging Masking (Security):** The `sqlAuditStore.Record` method receives raw `metadataJSON`. Ensure that the API/Controller layer aggressively scrubs secrets (e.g. env vars, tokens) from the metadata map before it is serialized and passed into the persistence layer. 
