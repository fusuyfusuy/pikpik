-- ============================================================================
-- MIGRATION 00011: COMMIT METADATA TRACKING FOR DEPLOYMENTS AND SERVICES
-- ============================================================================

-- 1. Add commit metadata columns to deployments
ALTER TABLE deployments ADD COLUMN last_commit_sha TEXT;
ALTER TABLE deployments ADD COLUMN last_commit_message TEXT;
ALTER TABLE deployments ADD COLUMN last_commit_author TEXT;

-- 2. Add commit metadata columns to services
ALTER TABLE services ADD COLUMN last_commit_sha TEXT;
ALTER TABLE services ADD COLUMN last_commit_message TEXT;
ALTER TABLE services ADD COLUMN last_commit_author TEXT;
