-- 00010_managed_machines.sql: Remote Machine Management & Standalone/Swarm Nodes Table

CREATE TABLE IF NOT EXISTS managed_machines (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'worker',
    public_ip TEXT NOT NULL DEFAULT '',
    private_ip TEXT NOT NULL DEFAULT '',
    os_kernel TEXT NOT NULL DEFAULT '',
    cpu_arch TEXT NOT NULL DEFAULT '',
    docker_version TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'offline',
    last_seen DATETIME,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_managed_machines_status ON managed_machines(status);
CREATE INDEX IF NOT EXISTS idx_managed_machines_role ON managed_machines(role);
CREATE INDEX IF NOT EXISTS idx_managed_machines_hostname ON managed_machines(hostname);
