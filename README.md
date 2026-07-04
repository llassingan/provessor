# Provessor

> Cloud VPS provisioning, simplified.

Provessor is a self-hosted VPS automation tool for cloud admins. Spin up fully configured virtual machines from a clean admin dashboard — pick a template, customize specs, and launch. The VM phones home with credentials so you can hand them straight to your users.

**BYOK** (Bring Your Own Keys): your cloud credentials live in an encrypted local database, never in plaintext env vars. No SaaS. No lock-in.

Built with a pluggable provisioning engine — currently targeting one cloud platform with a clear path to extend to others.

---

## Tech Stack

| Layer | Technology |
|-------|-----------|
| **Backend** | Go 1.25 · Chi router · `database/sql` |
| **Database** | SQLite (encrypted at rest via Adiantum/XChaCha12) |
| **Auth** | bcrypt + AES-256-GCM encrypted session tokens |
| **Realtime** | SSE (Server-Sent Events) for provisioning status |
| **Frontend** | React 18 · TypeScript (strict) · Tailwind CSS v4 · Vite |
| **Network** | OCI Go SDK (native) |
| **Config Mgmt** | Ansible via cloud-init (`cc_ansible` + custom `write_files`) |
| **Container** | Docker Compose (single Go API container) |

---

## Prerequisites

Before running Provessor, you need:

- **Docker** and **Docker Compose** (≥ v2)
- **Go 1.25+** (for local development)
- **Node.js 20+** (for the dashboard dev server)
- A cloud account with API credentials (region, compartment/project, key pair)

---

## Quick Start

### 1. Clone and set your encryption key

```bash
git clone <repo-url>
cd vps-store

# Generate a 32-byte hex key — keep this safe, it's unrecoverable
openssl rand -hex 32 > .env
echo "DB_ENCRYPTION_KEY=$(cat .env)" >> .env
```

Your `.env` file should look like:

```env
DB_ENCRYPTION_KEY=b647bf795dddbcd6a38e529c416f1d0d064874f3a949a4f86ed4e1f3e07a08f4
CORS_ORIGINS=https://yourdomain.id,http://localhost:10001
```

| Variable | Required | Description |
|----------|----------|-------------|
| `DB_ENCRYPTION_KEY` | Yes | 64-char hex string (32 bytes). Master encryption key for the database. |
| `CORS_ORIGINS` | No | Comma-separated allowed origins. Defaults to `http://localhost:5173,http://localhost:10001`. Set to your dashboard domain in production. |

> **Warning**: `DB_ENCRYPTION_KEY` is the master encryption key. Without it, your database — including all cloud credentials and user data — is irrecoverable. Back it up.

### 2. Start the API

```bash
docker compose up --build
```

The API listens on `http://localhost:10000`. Verify it's up:

```bash
curl http://localhost:10000/api/health
# {"status":"ok","timestamp":"..."}
```

### 3. Start the dashboard (dev mode)

```bash
cd web
npm install
npm run dev
```

Open `http://localhost:10001`. The Vite dev server proxies `/api` to the Go backend.

**Frontend env vars** (set in `.env` or `web/.env`):

| Variable | Description |
|----------|-------------|
| `VITE_API_TARGET` | Backend URL that Vite proxies `/api` to. Defaults to `http://localhost:10000`. |
| `VITE_API_BASE_URL` | Override the API base path from `/api` (only needed in production builds without the Vite proxy). |

### 4. Sign up and set up

1. Visit the dashboard — create your admin account (email + password).
2. Go to **Settings** → enter your cloud API credentials.
3. Click **Set up now** to provision networking infrastructure (OCI SDK, one-time).
4. Return to the dashboard and create your first VPS.

### 5. Provision a VPS

1. **New VPS** → pick a template (WordPress, Node.js, Docker, or Ubuntu).
2. Choose your shape, OCPU count, memory, and boot volume size.
3. Click **Launch** — watch the live provisioning log stream via SSE.
4. Once ready, copy the credentials and send them to your users.

---

## Development

### Backend (Go)

```bash
# Build
make build

# Run (using .env key)
make dev

# Run tests with race detector
make test

# Lint (requires golangci-lint)
make lint
```

### Frontend (React)

```bash
cd web
npm run dev     # Dev server on :10001 with API proxy
npm run build   # Production build
npm run lint    # ESLint (strict rules)

# Override API target (if BE is on a different host)
VITE_API_TARGET=http://api-provessor.example.com npm run dev

# Production build pointing at external API
VITE_API_BASE_URL=https://api-provessor.example.com/api npm run build
```

### Full rebuild from scratch

```bash
make clean
docker compose down
docker compose up --build
```

---

## Project Structure

```
vps-store/
├── cmd/api/main.go              # Go entry point
├── internal/
│   ├── config/                  # Env var loading + validation
│   ├── db/                      # Encrypted SQLite + migrations
│   ├── model/                   # Domain types (User, VPS, Template, Settings)
│   ├── repository/              # Database access layer
│   ├── service/                 # Business logic (auth, provision, network, validator)
│   ├── handler/                 # HTTP handlers (auth, VPS, settings, templates, SSE)
│   ├── server/                  # Chi router, middleware, CORS, routes
│   ├── sse/                     # Event broker for real-time provisioning status
│   └── validator/               # Cloud shape limit definitions
├── web/                         # React admin dashboard
├── ansible/                     # Playbooks + cloud-init templates
│   ├── templates/               # WordPress, Node.js, Docker, Ubuntu
│   └── cloud-init/              # cc_ansible YAMLs for each stack
├── docker-compose.yml           # Production-like local dev
└── Makefile                     # Build, test, lint, docker
```

---

## Security

- **Database**: encrypted at rest with Adiantum (XChaCha12+AES+NH+Poly1305) via the `vfs/adiantum` VFS layer. The encryption key is never written to disk in plaintext — it lives only in your `.env`.
- **Credentials**: cloud API keys are stored in the encrypted database, entered through the Settings UI. They are never logged and never exposed in API responses.
- **Sessions**: AES-256-GCM encrypted tokens with random 12-byte nonces per token. HttpOnly cookies with `SameSite=Lax`.
- **Passwords**: bcrypt with cost factor 12.
- **VM communication**: instances phone home via a bearer token. SSH (port 22) is intentionally open in the VCN security list as a **recovery backdoor** — root password login is disabled (`PermitRootLogin prohibit-password` in cloud-init), only key-based root access works. The API server holds the SSH private key for emergency recovery. fail2ban is installed by default to mitigate brute-force attempts. Per-VPS firewall rules (HTTP/HTTPS + user rules) are enforced via Network Security Groups — independent per instance.

---

## Templates

Provessor ships with four curated application stacks:

| Template | Stack |
|----------|-------|
| **WordPress** | nginx + PHP 8.1-FPM + MariaDB + WP-CLI |
| **Node.js** | nginx reverse proxy + Node.js 20 + PM2 |
| **Docker** | Docker CE + docker-compose + UFW |
| **Ubuntu** | UFW + fail2ban + unattended-upgrades |

Custom templates are also supported — paste your own Ansible playbook YAML in the dashboard and Provessor embeds it directly into cloud-init.

---

## Architecture

```
┌──────────────────────┐     ┌──────────────────────────────────────┐
│  Admin Dashboard      │────▶│  Go API (Chi, port 10000)            │
│  React + Tailwind    │     │  ┌──────────┐ ┌───────────┐          │
└──────────────────────┘     │  │ Handlers │ │ SSE Broker│          │
                               │  └────┬─────┘ └─────┬─────┘          │
                               │       │               │               │
                               │  ┌────▼───────────────▼───┐         │
                               │  │   Service Layer         │         │
                               │  └────────┬───────────────┘         │
                               │       │          │                   │
                               │  ┌────▼────┐ ┌──▼────────┐         │
                               │  │ SQLite  │ │ Cloud SDK │────────┐│
                               │  │(encryp) │ │(Provision)│        ││
                               │  └─────────┘ └───────────┘        ││
                               └────────────────────────────────────│─┘
                                       │                             │
                               ┌───────▼────┐            ┌──────────▼──────────┐
                               │  SQLite DB  │            │ Cloud Provider       │
                               │  (encrypted)│            │ ┌──────────────────┐ │
                               │  local file  │            │ │  Network (OCI SDK)│ │ once
                               │  .env=key   │            │ └──────────────────┘ │
                               └────────────┘            │ ┌──────────────────┐ │
                                                          │ │  Compute VMs      │ │
                                                          │ │  (SDK + Ansible)  │ │ per request
                                                          │ └──────────────────┘ │
                                                          └─────────────────────┘
```

---

## License

MIT
