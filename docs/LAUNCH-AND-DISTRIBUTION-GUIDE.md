# `pikpik` Launch, Distribution & Positioning Strategy 🚀

This document outlines the distribution, positioning, and community launch strategy for `pikpik` across GitHub, Product Hunt, Hacker News, Reddit, and developer ecosystem registries.

---

## 1. Positioning & Taglines

### Primary Value Proposition
> **"A Minimalist, High-Reliability Open-Source Alternative to Vercel, Netlify, and Heroku."**

### Core Architectural Pillars (The 4 Invariants)
1. **Zero Shelling**: 100% typed Docker Engine Socket API (`/var/run/docker.sock`), zero `sh -c` or bash string interpolation.
2. **Single Unified Runtime**: Single Go binary (`pikpik`), zero external Redis or PostgreSQL dependencies required.
3. **Sub-15ms Ingress**: Instantaneous in-memory route reconfiguration over Caddy's dynamic Admin REST API.
4. **Pure Streaming Pipelines**: Live streaming S3 database backups (`pg_dump | gzip | S3`) with `<32MB` RAM and 0-byte `/tmp` disk footprint.

### Pre-Alpha WIP Disclaimer
> ⚠️ **Notice**: `pikpik` is currently an active research and early development project and is **NOT EVEN IN ALPHA STATE**. Public APIs, wire protocols, storage schemas, and CLI subcommands are subject to rapid, breaking iterations without backward compatibility shims.

---

## 2. GitHub Feed & Search Engine Optimization (SEO)

### Configured Repository Topics (Tags)
- `paas`, `self-hosted`, `docker`, `docker-swarm`, `golang`
- `vercel-alternative`, `heroku-alternative`, `netlify-alternative`, `coolify-alternative`
- `caddy`, `devops`, `deployment`, `infrastructure`, `sqlite`

### OpenGraph Social Preview (1280×640 px)
- **Title**: `pikpik` (Bold, clean typography)
- **Subtitle**: *Minimalist, High-Reliability Self-Hosted PaaS (Vercel & Heroku Alternative)*
- **Key Badges**: `Single Go Binary (<14MB)` • `Sub-15ms Ingress` • `Pure S3 Streaming Backups` • `Zero Shelling`

---

## 3. Product Hunt Launch Blueprint

### Listing Metadata
- **Product Name**: `pikpik`
- **Tagline (<= 60 chars)**: *Minimalist open-source alternative to Vercel & Heroku*
- **Categories**: `Developer Tools`, `Open Source`, `Hosting`, `DevOps`
- **Pricing**: `Free & Open Source (MIT)`

### Visual Asset Gallery
- **Slide 1**: Architecture Blueprint (Control Plane, Remote Node Agents, Caddy Admin API, Docker Socket).
- **Slide 2**: Operator CLI Walkthrough (`pikpik init` -> `pikpik deploy` -> `pikpik logs -f`).
- **Slide 3**: Memory & Resource Benchmark Table (`<14MB` idle RAM vs 1.2GB conventional stacks).
- **Slide 4**: Streaming S3 backup pipeline without disk writes.

### Maker Comment Template
```text
Hey Product Hunt! 👋

I built pikpik because self-hosting modern web applications and databases shouldn't require running 5 polyglot daemons, waiting 3 seconds for file-watcher proxy reloads, or extracting 50GB database dumps into /tmp disk files.

pikpik is a clean-slate, high-reliability self-hosted PaaS built in Go around 4 strict invariants:
1. Zero Shelling — 100% typed Docker Engine Socket API (zero sh -c or bash interpolation)
2. Single Unified Binary — 1 static binary (<14MB) with zero external Redis or PostgreSQL dependencies
3. Sub-15ms Dynamic Ingress — Instant in-memory routing mutations over Caddy's dynamic Admin REST API
4. Pure Streaming Pipelines — Zero /tmp disk footprint backups streamed directly to S3

Note: We are in early Pre-Alpha/WIP research stage.

Check out the code: https://github.com/fusuycorp/pikpik
Would love your feedback on the architecture!
```

---

## 4. Hacker News ("Show HN") Submission Strategy

- **Title**: `Show HN: Pikpik – A minimalist, single-binary self-hosted PaaS in Go (WIP)`
- **Target Timing**: Tuesday or Wednesday at **13:00–14:00 UTC** (08:00–09:00 ET).
- **Tone**: Strictly technical, architectural, candid about Pre-Alpha status, and open to peer critique.

---

## 5. Developer Communities & Awesome-Lists

### Community Submissions
- **Reddit**:
  - `r/selfhosted`: *"Building a lightweight, single-binary alternative to Coolify/Dokploy in Go [WIP]"*
  - `r/golang`: *"Building an API-first PaaS control plane using modernc SQLite, Caddy REST, and Docker Socket"*
  - `r/devops`: Discussing zero-downtime rolling Swarm updates and streaming database backups.
- **Awesome Lists (GitHub Pull Requests)**:
  - [`awesome-selfhosted`](https://github.com/awesome-selfhosted/awesome-selfhosted) (Category: *Software Development - Hosting & PaaS*)
  - [`awesome-go`](https://github.com/avelino/awesome-go) (Category: *DevOps & Container Tools*)
