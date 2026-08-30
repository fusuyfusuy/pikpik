-- ============================================================================
-- MIGRATION 00008: SERVICE RUNTIME MODE & COMPOSE BLUEPRINT
-- ============================================================================

-- 1. Add runtime_mode column to services ('swarm' vs 'standalone')
ALTER TABLE services ADD COLUMN runtime_mode TEXT NOT NULL DEFAULT 'standalone';

-- 2. Add compose_yaml column to services for storing blueprint definitions
ALTER TABLE services ADD COLUMN compose_yaml TEXT NOT NULL DEFAULT '';
