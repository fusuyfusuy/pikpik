# Scope 6: Cross-Boundary Seam & Contract Audit

**Date**: 2026-08-31
**Auditor**: Scope 6 Auditor (Antigravity)
**Scope**: Interface Contracts, Schema Alignment, DTO boundaries, and Persistence Seams

## 1. Executive Summary & Scoring
**Score: 7.2/10 (Moderate)**
The codebase exhibits several distinct contract boundaries. While the fundamental architecture honors its constraints (SQLite WAL, streaming protocols, typed SDKs), the API-to-Frontend boundary suffers from significant schema drift. The most critical issue is an impedance mismatch in the `BackupSchedule` API, where the frontend sends a relational ID while the backend expects flat provider credentials. 

### Critical Findings
* **Backup Schedule Drift**: The TS frontend sends `s3_destination_id`, but the Go backend expects raw S3 fields (`s3_bucket`, `s3_endpoint`, etc.) and entirely ignores the ID.
* **App Webhook Secret Drift**: The TS `App` and `CreateAppRequest` reference `webhook_secret`, but this is completely missing in the Go `App` DTO and `CreateAppRequest`. The backend uses `deploy_token_hash` internally but never maps it to the API layer.
* **User DTO Drift**: Go's `UserResponse` includes `totp_enabled` and `updated_at`, but these are missing from the TypeScript `User` interface.

## 2. API Contracts & DTOs Analysis
**Boundary**: `pkg/api/types.go` vs `web/src/lib/types.ts` vs `cmd/pikpik-cli/client.go`

* **`App` / `Service` Contract**:
  * **Frontend (TS)**: Expects and sends `webhook_secret: string` for GitOps integration. 
  * **Backend (Go)**: `pkg/api/types.go` `CreateAppRequest` and `App` structs completely omit `webhook_secret`. The backend `store.Service` has `DeployTokenHash`, but it is not exposed or mapped in the DTO layer. 
* **`User` Contract**:
  * **Frontend (TS)**: `User` interface lacks `totp_enabled` and `updated_at`.
  * **Backend (Go)**: `UserResponse` explicitly returns `TOTPEnabled` and `UpdatedAt`. The frontend is dropping these security and auditing fields.
* **`BackupSchedule` Contract**:
  * **Frontend (TS)**: `CreateBackupScheduleRequest` sends `s3_destination_id: string`.
  * **Backend (Go)**: `CreateBackupScheduleRequest` lacks `s3_destination_id`. It instead requires flattened S3 credentials (`s3_bucket`, `s3_endpoint`, `s3_region`, `s3_access_key`, `s3_secret_key`). This guarantees that backups created from the UI will silently fail or reject due to missing S3 bucket parameters.

## 3. Persistence Contracts
**Boundary**: `pkg/store/migrations/*.sql` vs `pkg/store/models.go`

* **`services` Table Evolution**: The initial `00001_initial_schema.sql` was missing git-related and runtime fields, but they have been correctly backfilled via migrations (`00006`, `00007`, `00008`, `00011`). The Go domain models align perfectly with the migrated schema.
* **Computed Fields**: `AppCount` on `Project` correctly operates as a computed field in `pkg/api/controller.go` (`AppCount: counts[p.ID]`), adhering to normal form without duplicating state in the `projects` table.

## 4. Configuration & Deployment Seams
**Boundary**: CLI Config vs Server Config vs Agent Config

* **Configuration Variable Consistency**:
  * **Server**: Uses `PIKPIK_LISTEN_ADDR`.
  * **Agent**: Uses `PIKPIK_CONTROL_PLANE_URL` for outbound tunneling and shares `PIKPIK_ENROLLMENT_TOKEN`. 
  * **CLI**: Relies on `PIKPIK_SERVER_URL` and `PIKPIK_TOKEN`.
  * *Assessment*: The environment variables are semantically aligned and map correctly to the out-of-band communication model described in the architecture constraints.

## 5. Invariant Contracts
**Boundary**: `AGENTS.md` vs Runtime Implementations

* **Invariant 1 (Zero Shelling)**: Validated. All container processes utilize the Docker SDK. `pkg/api/pty.go` correctly falls back to `exec.CommandContext` exclusively for *Host* PTY fallback sessions, without using interpolated `sh -c` strings, maintaining the security boundary.
* **Invariant 2 (Unified Runtime)**: Validated. `pkg/store/sqlite.go` forcefully configures `PRAGMA journal_mode=WAL`, `busy_timeout(5000)`, and `foreign_keys(1)`. The in-process embedded requirements are flawlessly respected.
