-- ============================================================================
-- 9. MULTI-DATABASE CRON BACKUP SCHEDULES
-- ============================================================================

CREATE TABLE IF NOT EXISTS backup_schedules (
    id                      TEXT PRIMARY KEY,
    service_id              TEXT NOT NULL,
    cron_expr               TEXT NOT NULL,
    engine                  TEXT NOT NULL,
    database_name           TEXT NOT NULL,
    username                TEXT NOT NULL DEFAULT '',
    password_encrypted      TEXT NOT NULL DEFAULT '',
    s3_bucket               TEXT NOT NULL,
    s3_endpoint             TEXT NOT NULL DEFAULT '',
    s3_region               TEXT NOT NULL DEFAULT '',
    s3_access_key           TEXT NOT NULL DEFAULT '',
    s3_secret_key_encrypted TEXT NOT NULL DEFAULT '',
    retention_hourly        INTEGER NOT NULL DEFAULT 0,
    retention_daily         INTEGER NOT NULL DEFAULT 7,
    retention_weekly        INTEGER NOT NULL DEFAULT 4,
    retention_monthly       INTEGER NOT NULL DEFAULT 12,
    max_backups             INTEGER NOT NULL DEFAULT 30,
    compression             TEXT NOT NULL DEFAULT 'gzip',
    is_enabled              INTEGER NOT NULL DEFAULT 1,
    last_run_at             DATETIME,
    next_run_at             DATETIME,
    created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_backup_schedules_service ON backup_schedules(service_id);
CREATE INDEX IF NOT EXISTS idx_backup_schedules_status ON backup_schedules(is_enabled, next_run_at);
