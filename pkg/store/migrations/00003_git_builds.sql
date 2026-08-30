-- ============================================================================
-- 8. GIT BUILDS & GITHUB APP INSTALLATIONS
-- ============================================================================

CREATE TABLE IF NOT EXISTS builds (
    id             TEXT PRIMARY KEY,
    service_id     TEXT NOT NULL,
    deployment_id  TEXT,
    repo_url       TEXT NOT NULL,
    branch         TEXT NOT NULL,
    commit_sha     TEXT NOT NULL,
    commit_message TEXT,
    author         TEXT,
    status         TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'cloning', 'building', 'pushing', 'success', 'failed', 'cancelled')),
    logs_path      TEXT,
    image_tag      TEXT,
    error_message  TEXT,
    duration_ms    INTEGER NOT NULL DEFAULT 0,
    started_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at    DATETIME,
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE CASCADE,
    FOREIGN KEY (deployment_id) REFERENCES deployments(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_builds_service ON builds(service_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_builds_status ON builds(status);

CREATE TABLE IF NOT EXISTS github_installations (
    id                   TEXT PRIMARY KEY,
    org_id               TEXT NOT NULL,
    installation_id      INTEGER NOT NULL UNIQUE,
    account_name         TEXT NOT NULL,
    account_type         TEXT NOT NULL DEFAULT 'User',
    repository_selection TEXT NOT NULL DEFAULT 'all',
    permissions          TEXT NOT NULL DEFAULT '{}',
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_github_installations_org ON github_installations(org_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_github_installations_inst ON github_installations(installation_id);
