-- ============================================================================
-- MIGRATION 00007: APPS, PROJECTS & TAGS REFINEMENT
-- ============================================================================

-- 1. Ensure Default Organization exists
INSERT OR IGNORE INTO organizations (id, name, slug, created_at, updated_at)
VALUES ('org_default', 'Default Organization', 'default', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- 2. Ensure Default Project exists
INSERT OR IGNORE INTO projects (id, org_id, name, slug, description, created_at, updated_at)
VALUES ('prj_default', 'org_default', 'Default Project', 'default', 'Default workspace for applications', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- 3. Ensure Default Stages exist
INSERT OR IGNORE INTO stages (id, project_id, name, slug, created_at, updated_at)
VALUES ('stg_default_prod', 'prj_default', 'Production', 'production', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO stages (id, project_id, name, slug, created_at, updated_at)
VALUES ('stg_default_staging', 'prj_default', 'Staging', 'staging', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- 4. Add tags column to services if not present
ALTER TABLE services ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';

-- 5. Add tags column to projects if not present
ALTER TABLE projects ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';

-- 6. Backfill any existing unassigned services
UPDATE services SET project_id = 'prj_default' WHERE project_id IS NULL OR project_id = '';
UPDATE services SET stage_id = 'stg_default_prod' WHERE stage_id IS NULL OR stage_id = '';
