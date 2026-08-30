-- ============================================================================
-- 7. SYSTEM TELEMETRY & HOURLY DOWNSAMPLING
-- ============================================================================

CREATE TABLE IF NOT EXISTS system_metrics_hourly (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type       TEXT NOT NULL,       -- 'node' | 'service' | 'container'
    entity_id         TEXT NOT NULL,       -- 'node-alpha-1' | 'svc_backend_api'
    bucket_start      INTEGER NOT NULL,   -- Unix epoch timestamp (hourly boundary)
    cpu_avg           REAL NOT NULL,
    cpu_min           REAL NOT NULL,
    cpu_max           REAL NOT NULL,
    cpu_p95           REAL NOT NULL,
    mem_avg           INTEGER NOT NULL,
    mem_max           INTEGER NOT NULL,
    net_rx_total      INTEGER NOT NULL,
    net_tx_total      INTEGER NOT NULL,
    disk_read_total   INTEGER NOT NULL,
    disk_write_total  INTEGER NOT NULL,
    sample_count      INTEGER NOT NULL,
    created_at        INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_metrics_entity_time 
ON system_metrics_hourly(entity_type, entity_id, bucket_start);
