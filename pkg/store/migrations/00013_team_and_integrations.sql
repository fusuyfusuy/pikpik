-- ============================================================================
-- 13. TEAM INVITATIONS, PROJECT MEMBERSHIPS & DEVELOPER INTEGRATIONS
-- ============================================================================

CREATE TABLE IF NOT EXISTS team_invitations (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL,
    email        TEXT NOT NULL COLLATE NOCASE,
    role         TEXT NOT NULL DEFAULT 'developer' CHECK (role IN ('owner', 'admin', 'developer', 'viewer')),
    token_hash   TEXT NOT NULL UNIQUE,
    invited_by   TEXT NOT NULL,
    expires_at   DATETIME NOT NULL,
    accepted_at  DATETIME,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE,
    FOREIGN KEY (invited_by) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_team_invitations_email ON team_invitations(email);
CREATE UNIQUE INDEX IF NOT EXISTS idx_team_invitations_token ON team_invitations(token_hash);
CREATE INDEX IF NOT EXISTS idx_team_invitations_org ON team_invitations(org_id);

CREATE TABLE IF NOT EXISTS project_memberships (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'developer' CHECK (role IN ('admin', 'developer', 'viewer')),
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE(project_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_proj_members_proj ON project_memberships(project_id);
CREATE INDEX IF NOT EXISTS idx_proj_members_user ON project_memberships(user_id);

CREATE TABLE IF NOT EXISTS integrations (
    id                    TEXT PRIMARY KEY,
    org_id                TEXT NOT NULL,
    name                  TEXT NOT NULL,
    type                  TEXT NOT NULL CHECK (type IN ('git_github', 'git_gitlab', 'git_gitea', 'git_generic', 'registry_dockerhub', 'registry_ghcr', 'registry_ecr', 'registry_custom', 'storage_s3', 'storage_r2', 'storage_minio')),
    credentials_encrypted TEXT NOT NULL,
    config_json           TEXT NOT NULL DEFAULT '{}',
    status                TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'error', 'disabled')),
    created_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_integrations_org ON integrations(org_id);
CREATE INDEX IF NOT EXISTS idx_integrations_type ON integrations(type);
