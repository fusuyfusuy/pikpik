-- ============================================================================
-- 11. SERVICE GIT INTEGRATION & BUILD STRATEGY FIELDS
-- ============================================================================
-- Adds Git repository URL, branch, build strategy, Dockerfile path, and
-- publish directory configuration to services for Git-driven deployments.

ALTER TABLE services ADD COLUMN git_repo_url TEXT;
ALTER TABLE services ADD COLUMN git_branch TEXT;
ALTER TABLE services ADD COLUMN build_strategy TEXT;
ALTER TABLE services ADD COLUMN dockerfile_path TEXT;
ALTER TABLE services ADD COLUMN publish_directory TEXT;
