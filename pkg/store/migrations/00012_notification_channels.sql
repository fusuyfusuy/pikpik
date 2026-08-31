-- Migration: 00012_notification_channels.sql
-- Multi-Channel Notification Destinations (Webhook, Discord, Slack, Telegram)

CREATE TABLE IF NOT EXISTS notification_channels (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    target_url TEXT NOT NULL,
    auth_token TEXT,
    events TEXT NOT NULL DEFAULT '["deploy:failure","deploy:success","backup:failure","backup:success"]',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notification_channels_org ON notification_channels(org_id);
CREATE INDEX IF NOT EXISTS idx_notification_channels_project ON notification_channels(project_id);
