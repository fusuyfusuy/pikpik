-- ============================================================================
-- MIGRATION 00009: MULTI-CONTAINER STACKS, DUAL-TIER NETWORKS & MANAGED VOLUMES
-- ============================================================================

-- 1. Stacks Table
CREATE TABLE IF NOT EXISTS stacks (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    compose_yaml TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'stopped',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_stacks_project_id ON stacks(project_id);

-- 2. Managed Networks Table
CREATE TABLE IF NOT EXISTS managed_networks (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    driver TEXT NOT NULL DEFAULT 'bridge',
    scope TEXT NOT NULL DEFAULT 'project',
    is_external INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_managed_networks_project_id ON managed_networks(project_id);

-- 3. Managed Volumes Table
CREATE TABLE IF NOT EXISTS managed_volumes (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    driver TEXT NOT NULL DEFAULT 'local',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_managed_volumes_project_id ON managed_volumes(project_id);
