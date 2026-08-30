# 12. Daemon & Systemd Architecture: Comparison with Dokploy & Coolify

> Technical comparison of self-hosting daemon models and the native systemd daemon architecture in `pikpik`.

---

## 1. How Existing Open-Source PaaS Platforms Install

### A. Dokploy (Docker Swarm Manager Service)
- **Bootstrap**: Runs `docker swarm init` and creates an attachable overlay network `dokploy-network`.
- **Runtime**: Deployed as a Swarm manager service mounting `/var/run/docker.sock` and host `/etc/dokploy`.
- **Auxiliary Containers**: Requires a standalone `dokploy-traefik` container (in host network mode), `dokploy-postgres` (Postgres 16 Swarm service), and `dokploy-monitoring` (`network: host` container).
- **Pain Points**: Requires 1:1 path mirroring between container and host, bundles bulky CLI toolchains inside the Node.js Docker image, and depends on external Postgres.

### B. Coolify (5-Container Docker Compose Stack with SSH Loopback)
- **Bootstrap**: Reconfigures `/etc/docker/daemon.json` subnet pools and injects an ed25519 key into the host's `~/.ssh/authorized_keys`.
- **Runtime**: Runs 5 persistent containers (`coolify` PHP 8.4/Horizon/Nginx, `coolify-db` Postgres 15, `coolify-redis` Redis 7, `coolify-realtime` Soketi/node-pty, `coolify-proxy` Traefik).
- **Communication**: The main PHP container does not mount `/var/run/docker.sock`. Instead, it SSHs into its own host (`host.docker.internal`) using OpenSSH `ControlMaster` multiplexing domain sockets (`/data/coolify/ssh/mux/`).
- **Pain Points**: Idling consumes `~800MB–1.5GB` RAM, stale SSH socket locks freeze operations, and self-upgrades require out-of-band detached `nohup` helper containers.

---

## 2. The `pikpik` Daemon Philosophy: Native Systemd Service

`pikpik` compiles to single static Go binaries (`pikpik` ~14MB, `pikpik-agent` ~6.2MB). Running `pikpik` as a native **Systemd unit** on the host provides fundamental architectural advantages:

1. **Zero Path Translation**: Directly accesses `/var/run/docker.sock` and `/var/lib/pikpik` with native kernel file descriptors without volume mapping or UID/GID mismatch.
2. **Zero SSH Loopback Fragility**: Eliminates SSH keys, `authorized_keys` pollution, and stale `ControlMaster` socket lockups entirely.
3. **Sub-25MB Footprint & <50ms Boot**: Consumes `<25MB` idle RAM with no container runtime wrapper, PHP-FPM, or S6 supervisor overhead.
4. **Direct Linux `/proc` Scraper**: Telemetry reads host `/proc/stat`, `/proc/meminfo`, `/proc/diskstats`, and `/proc/net/dev` directly without `--privileged` or `--net=host` container bridging.
5. **Atomic Self-Upgrades**: In-place binary replacement (`os.Rename`) followed by `systemctl restart pikpik`.

---

## 3. Production Systemd Unit Definitions

### `/etc/systemd/system/pikpik.service` (Control Plane)
```ini
[Unit]
Description=pikpik Minimalist Self-Hosted PaaS Control Plane
Documentation=https://github.com/fusuyfusuy/pikpik
After=network.target docker.service caddy.service
Requires=docker.service
Wants=caddy.service

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=/var/lib/pikpik
ExecStart=/usr/local/bin/pikpik \
  --listen :8080 \
  --data-dir /var/lib/pikpik \
  --docker-socket /var/run/docker.sock \
  --caddy-url http://127.0.0.1:2019

Restart=always
RestartSec=3s
LimitNOFILE=65535
LimitNPROC=65535

# Security hardening
ProtectSystem=full
ProtectHome=read-only
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

### `/etc/systemd/system/pikpik-agent.service` (Remote Worker Node)
```ini
[Unit]
Description=pikpik Remote Worker Node Agent
Documentation=https://github.com/fusuyfusuy/pikpik
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=root
Group=root
ExecStart=/usr/local/bin/pikpik-agent \
  --control-plane-url wss://cp.example.com/agent/connect \
  --token pik_node_enrollment_secret_token \
  --docker-socket /var/run/docker.sock \
  --node-role worker

Restart=always
RestartSec=5s
LimitNOFILE=32768

[Install]
WantedBy=multi-user.target
```
