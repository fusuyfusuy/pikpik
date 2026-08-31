# E2E Test Infra: pikpik

## Test Philosophy
- **Opaque-box & Requirement-Driven**: Tests derive directly from user requirements (`ORIGINAL_REQUEST.md`) and public API/CLI interfaces.
- **Methodology**: Systematic 4-tier testing hierarchy combining Category-Partition, Boundary Value Analysis (BVA), Pairwise Combinatorial Testing, and Real-World Workload Simulation.
- **Core Invariant Auditing**: Explicit assertions verifying Invariant 1 (Zero Shelling), Invariant 2 (Single SQLite WAL binary), Invariant 3 (Sub-15ms dynamic Caddy ingress, 0 Caddyfiles on disk), and Invariant 4 (Pure streaming S3 pipelines, peak RAM <32MB, 0 /tmp files).

---

## Feature Inventory & Test Matrix
| # | Feature Domain | Tier 1 (Feature) | Tier 2 (Boundary) | Tier 3 (Pairwise) | Tier 4 (Workloads) |
|---|----------------|:----------------:|:-----------------:|:-----------------:|:------------------:|
| 1 | Compute & App Deployments | 5 | 5 | ✓ | ✓ |
| 2 | Start-Before-Stop Rolling Updates | 5 | 5 | ✓ | ✓ |
| 3 | Docker Compose / Stacks | 5 | 5 | ✓ | ✓ |
| 4 | Swarm Multi-Node Workloads | 5 | 5 | ✓ | ✓ |
| 5 | Multi-DB Engines (Postgres, MySQL, Redis, Mongo) | 5 | 5 | ✓ | ✓ |
| 6 | Application Marketplace Templates | 5 | 5 | ✓ | ✓ |
| 7 | Multi-Source CI/CD Git Cloner | 5 | 5 | ✓ | ✓ |
| 8 | Build Pipelines (Dockerfile & Nixpacks) | 5 | 5 | ✓ | ✓ |
| 9 | Dynamic Ingress & Traffic Splitting | 5 | 5 | ✓ | ✓ |
| 10 | On-Demand TLS & Custom Domains | 5 | 5 | ✓ | ✓ |
| 11 | Managed Volumes & Networks | 5 | 5 | ✓ | ✓ |
| 12 | Zero-Disk S3 Streaming Backups | 5 | 5 | ✓ | ✓ |
| 13 | Worker Agent & Remote Nodes | 5 | 5 | ✓ | ✓ |
| 14 | Telemetry Scrapers & Ring Buffers | 5 | 5 | ✓ | ✓ |
| 15 | Interactive Container PTY Terminal | 5 | 5 | ✓ | ✓ |
| 16 | Multi-Tenancy & RBAC | 5 | 5 | ✓ | ✓ |

---

## Test Architecture
- **Runner**: Automated Go test harness + CLI runner (`pkg/e2e` and `tests/`) executed via:
  ```bash
  export PATH=$PATH:/usr/local/go/bin:/home/devhax/go/bin
  go test -v -race -count=1 ./pkg/... ./cmd/...
  cd web && npm run build
  mimori debt sync && mimori debt check
  ```
- **Pass/Fail Semantics**: All tests must complete with exit code 0, 0 data races under `-race`, 0 compiler warnings, and zero unhandled errors.

---

## Real-World Application Scenarios (Tier 4)
| # | Scenario | Features Exercised | Complexity |
|---|----------|--------------------|------------|
| 1 | Full-Stack E-Commerce SaaS | Next.js Frontend + Go API + Postgres DB + Redis Cache + S3 Backups | High |
| 2 | Zero-Downtime Rolling Update | Live HTTP traffic load during start-before-stop application rollout | High |
| 3 | Canary Ingress Traffic Shifting | 90/10 -> 50/50 -> 0/100 weighted traffic split via Caddy Admin REST | High |
| 4 | Streaming Disaster Recovery | Live multi-DB S3 backup stream -> point-in-time streaming restore | High |
| 5 | Multi-Tenant RBAC & PAT Revocation | Org isolation, role-based resource mutation, versioned token revocation | High |

---

## Coverage Thresholds
- Tier 1: ≥80 test cases (≥5 per feature domain across 16 domains)
- Tier 2: ≥80 test cases (boundary limits, empty inputs, network dropouts, zero/negative weights)
- Tier 3: ≥25 pairwise interaction test cases (e.g. Canary traffic split + S3 backup stream + rolling update)
- Tier 4: ≥5 realistic end-to-end full-lifecycle application scenarios
- Tier 5: Adversarial white-box stress testing and data-race verification
