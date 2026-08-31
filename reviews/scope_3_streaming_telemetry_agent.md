# Scope 3 Audit: Pure Streaming Pipelines, Telemetry & Agent Protocol

## 1. Executive Summary

- **Health Score**: 7.2 / 10.0 (Moderate)
- **Invariant Breaches**: Invariant 4 (Pure Streaming Pipelines) is violated by unbounded memory buffers in the backup engine.
- **Top Findings**:
  1. **Unbounded Memory OOM Risk**: `engine.go` buffers massive container stdout/stderr strings in memory during restores.
  2. **Missing Backup Encryption**: Streamed database dumps are sent to S3 unencrypted, lacking AES-256-GCM.
  3. **Credential Leakage**: Webhook and notification dispatchers fail to sanitize credentials from metadata payloads.
- **Actionable Remediations**: Implement a bounded `limitWriter` for docker exec outputs, inject an AES-GCM cipher stream into the `io.Pipe` pipeline in `multi_db.go`, and add a regex-based secret scrubber to `dispatcher.go`.

---

## 2. Invariant 4 (Pure Streaming Pipelines)

### ❌ Critical Violation: Unbounded In-Memory Buffers
**Location**: `pkg/backup/engine.go` (Lines 60-61, 109-110)
**Issue**: The backup and restore execution wrappers use unbounded `strings.Builder` targets for `stdcopy.StdCopy` to capture container `stdout` and `stderr`. 
During database restores (e.g., `psql` or `mysql`), the container may output gigabytes of verbose logs or warnings to stdout/stderr. Capturing this entirely in RAM shatters the `<32MB` bounded memory limit, guaranteeing OOM kills on moderate-to-large workloads.
**Remediation**:
Replace `strings.Builder` with a constrained writer such as an `io.LimitReader` wrapper combined with a capped byte buffer, or stream logs directly to the target UI via a WebSocket pipe to maintain zero-disk and bounded-memory properties.

---

## 3. Security Review

### ❌ Critical Violation: Missing AES-256-GCM Encryption
**Location**: `pkg/backup/multi_db.go` (Lines 291-295) & `pkg/backup/s3/client.go`
**Issue**: Despite explicit security requirements dictating "Backup encryption at rest (streaming AES-256-GCM)", the pipeline chains `io.Pipe -> gzip -> S3` without any cryptographic layer. Backups are stored completely unencrypted at rest in the remote buckets.
**Remediation**: 
Inject an AES-GCM `cipher.StreamWriter` into the pipeline between `gzip.Writer` and `io.Pipe` to perform on-the-fly symmetric encryption using an organization-level master key.

### ❌ High Violation: Credential Leakage in Notifications
**Location**: `pkg/notifications/dispatcher.go` (Lines 191-209, 225-236)
**Issue**: The event dispatcher loops over `evt.Metadata` and formats the raw values directly into Slack, Discord, and Telegram payloads. There is no redaction mechanism for connection strings, passwords, or tokens that may be inadvertently included in the event payload or metadata.
**Remediation**: Implement a `maskSensitiveData(map[string]string)` helper using regex to redact values for keys containing `password`, `secret`, `token`, or `uri` before dispatching.

---

## 4. Robustness & Correctness

### ⚠️ Moderate: Aggressive WebSocket Read Timeout
**Location**: `pkg/telemetry/ws_hub.go` (Line 278)
**Issue**: `conn.Read` is wrapped in a hard 60-second context timeout. If the client fails to send a manual `ping` frame exactly within that window, the agent aggressively severs the connection. 
**Remediation**: Rely on native WebSocket ping/pong control frames rather than application-layer JSON ping frames to handle network jitter natively and reliably.

### ⚠️ Minor: Hardcoded Downsampling Intervals
**Location**: `pkg/telemetry/ring_buffer.go` (Lines 128-132)
**Issue**: The downsampler computes total byte volumes by blindly multiplying rate values by `10` (`uint64(pt.NetRxRate) * 10`). This assumes a pristine collection cadence of exactly 10 seconds. In reality, CPU contention or agent jitter will warp this interval, causing inaccurate aggregate volumes.
**Remediation**: Use the delta of actual timestamps between consecutive `MetricPoint` records instead of a hardcoded multiplier.

---

## 5. Performance (Exemplary Highlights)

### ✅ Perfect Implementation: Zero-Disk S3 Multipart Streaming
**Location**: `pkg/backup/s3/client.go` (Lines 272-366)
**Analysis**: The engine achieves exemplary pure streaming by using concurrent memory-bounded workers mapped to S3 multipart chunk uploads. The `<32MB` constraint is cleanly respected via a disciplined concurrency semaphore and strict 5MB chunk slicing directly off the `io.Reader`. Deadlocks are avoided through careful channel closure hygiene.

### ✅ Perfect Implementation: Backpressure & Slow Consumers
**Location**: `pkg/telemetry/ws_hub.go` (Lines 198-225)
**Analysis**: The telemetry broker implements flawless slow-consumer handling. Instead of blocking the core agent loop when a web client connection stalls, the dispatcher non-blockingly drops stale metric frames (`default: return`) while queuing a `warning: dropped_frames` event if non-metric messages fail delivery.

### ✅ Accurate Memory Calculation
**Location**: `pkg/telemetry/docker_collector.go` (Lines 472-496)
**Analysis**: The collector correctly deducts `inactive_file` cache memory from the raw Docker stats usage, normalizing against both Cgroups V1 and V2 footprints. This prevents false positive OOM alerts on high page-cache containers like Postgres or Redis.
