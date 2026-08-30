-- ============================================================================
-- 1. ORGANIZATIONS & USERS
-- ============================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    id                 TEXT PRIMARY KEY,
    email              TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash      TEXT NOT NULL,
    role               TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'admin', 'developer', 'viewer')),
    totp_secret        TEXT,
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
    id           TEXT PRIMARY KEY,
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
    prefix       TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    scopes       TEXT NOT NULL,
    last_used_at DATETIME,
    expires_at   DATETIME,
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
    domain_names       TEXT NOT NULL DEFAULT '[]',
    deploy_token_hash  TEXT,
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
    resource_id      TEXT NOT NULL,
    key              TEXT NOT NULL,
    value_encrypted  TEXT NOT NULL,
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
    slug                    TEXT NOT NULL,
    mount_path              TEXT NOT NULL,
    type                    TEXT NOT NULL CHECK (type IN ('named', 'bind', 'file')),
    host_path               TEXT,
    config_content_encrypted TEXT,
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
    initiated_by   TEXT NOT NULL,
    started_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at    DATETIME,
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_deployments_service ON deployments(service_id, started_at DESC);

CREATE TABLE IF NOT EXISTS backup_configs (
    id             TEXT PRIMARY KEY,
    service_id     TEXT NOT NULL UNIQUE,
    s3_endpoint    TEXT NOT NULL,
    s3_bucket      TEXT NOT NULL,
    s3_region      TEXT NOT NULL,
    s3_access_key  TEXT NOT NULL,
    s3_secret_key_encrypted TEXT NOT NULL,
    cron_expr      TEXT NOT NULL,
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
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    metadata      TEXT NOT NULL DEFAULT '{}',
    ip_address    TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC);
