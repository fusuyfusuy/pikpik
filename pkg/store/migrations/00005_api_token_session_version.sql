-- ============================================================================
-- 10. API TOKEN SESSION VERSIONING
-- ============================================================================
-- Records the users.session_version that was active when a token was issued,
-- so ValidateAPIToken can reject tokens issued before a password rotation
-- (which bumps session_version) even though the token itself has not expired.

ALTER TABLE api_tokens ADD COLUMN session_version INTEGER NOT NULL DEFAULT 1;
