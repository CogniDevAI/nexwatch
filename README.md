# NexWatch

A unified, self-hosted monitoring platform built with Go and React. Monitor your servers, Docker containers, and infrastructure from a single real-time dashboard.

## Features

- **Real-time Dashboard** -- Live CPU, memory, disk, and network metrics with uPlot charts
- **Multi-Server Monitoring** -- Deploy lightweight agents on any Linux server
- **Docker Container Tracking** -- Monitor container status, CPU, memory, and network per container
- **Alerting Engine** -- Threshold-based alerts with configurable rules, severity levels, duration windows, and cooldowns
- **Multi-Channel Notifications** -- Email (SMTP), Webhook, Telegram, and Discord
- **Agent Management** -- Add/remove agents from the UI, generate install tokens with one click
- **Automatic Downsampling** -- Raw metrics aggregated to 1m, 5m, and 1h intervals; configurable retention policies
- **Dark Theme Dashboard** -- Cyan and purple accent palette designed for monitoring workflows
- **Single Binary Deployment** -- Hub runs as a single binary with embedded UI; zero external dependencies
- **PocketBase Backend** -- SQLite-powered with real-time subscriptions, built-in auth, and admin panel

## Architecture

NexWatch is a Go monorepo with two binaries sharing protocol types:

- **Hub** -- A custom PocketBase application that handles WebSocket connections from agents, stores metrics in SQLite, evaluates alert rules, dispatches notifications, and serves the embedded React SPA. The hub is the single server component: no Redis, no Postgres, no external queue.

- **Agent** -- A lightweight Go binary (~10 MB) that collects system metrics (CPU, memory, disk, network, OS info) and optionally Docker container stats. It connects to the hub over WebSocket using MessagePack serialization and auto-reconnects with exponential backoff.

- **Frontend** -- A React + TypeScript SPA built with Vite. Uses Zustand for state management, PocketBase SDK for real-time subscriptions, uPlot for high-performance time-series charts, and Tailwind CSS for styling.

```
Agent (Go)                Hub (Go + PocketBase)        Frontend (React)
┌──────────┐  WebSocket   ┌──────────────┐  REST/SSE  ┌─────────┐
│Collectors ├─────────────>│ WS Handler   ├───────────>│Dashboard │
│ CPU, RAM  │  MessagePack │ Metrics Svc  │            │ Charts  │
│ Disk, Net │              │ Alert Engine │            │ Alerts  │
│ Docker    │              │ Notifier     │            │Settings │
└──────────┘              └──────────────┘            └─────────┘
```

Data flows: Collector -> msgpack encode -> WS push -> Hub decode -> batch insert (SQLite WAL) -> alert engine eval -> downsample cron (1m/5m/1h) -> purge per retention policy.

## Quick Start with Docker Compose

The fastest way to run NexWatch:

```bash
git clone https://github.com/CogniDevAI/nexwatch.git
cd nexwatch
docker-compose -f deploy/docker-compose.yml up -d
```

The hub (with embedded dashboard) will be available at `http://localhost:8090`.

On first launch, visit `http://localhost:8090/_/` to create an admin account via PocketBase's admin UI. Then log in to the NexWatch dashboard at `http://localhost:8090`.

## Agent Installation

### From the Dashboard

1. Go to the **Agents** page in the NexWatch dashboard.
2. Click **Add Agent**, enter a name, and click **Generate Token**.
3. Copy the install command and run it on the target server.

### Manual Install

Run the install script on any Linux server (amd64 or arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.sh | bash -s -- \
  --hub ws://YOUR_HUB:8090/ws/agent \
  --token YOUR_TOKEN
```

The script will:
- Download the latest agent binary from GitHub Releases
- Create a config file at `/etc/nexwatch/agent.yaml`
- Set up and start a systemd service

### Agent Configuration

The agent reads configuration from a YAML file, environment variables, and CLI flags (in that priority order):

```yaml
# /etc/nexwatch/agent.yaml
hub_url: "ws://hub-server:8090/ws/agent"
token: "your-agent-token"
interval: 10s
docker_socket: /var/run/docker.sock
collectors_enabled:
  - cpu
  - memory
  - disk
  - network
  - sysinfo
  - docker
```

**Environment variables**: `NEXWATCH_HUB_URL`, `NEXWATCH_TOKEN`, `NEXWATCH_INTERVAL`.

**CLI flags**: `--hub`, `--token`, `--interval`, `--config`, `--docker-socket`.

## Configuration

### Hub

The hub is configured via CLI flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--http` | `0.0.0.0:8090` | HTTP listen address |
| `--retention` | `30` | Metric data retention in days |

### Settings (via Dashboard)

From the **Settings** page in the dashboard you can configure:

- **Data Retention** -- How many days of raw metric data to retain (7-90 days)
- **Default Collection Interval** -- Recommended interval for new agents

## Development

### Prerequisites

- Go 1.24+
- Node.js 22+ with pnpm
- Docker (optional, for container monitoring)

### Build

```bash
# Install Go dependencies
go mod tidy

# Install frontend dependencies
cd ui && pnpm install && cd ..

# Start the hub (in one terminal)
make dev-hub

# Start the frontend dev server (in another terminal)
make dev-ui

# Start an agent (in another terminal)
make dev-agent
```

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make build-all` | Build UI, hub, and agent |
| `make build-ui` | Build React frontend |
| `make build-hub` | Build hub binary (includes UI build) |
| `make build-agent` | Build agent binary |
| `make dev-hub` | Run hub in development mode |
| `make dev-ui` | Run Vite dev server with HMR |
| `make dev-agent` | Run agent against local hub |
| `make clean` | Remove build artifacts |
| `make tidy` | Run go mod tidy |
| `make fmt` | Format Go and TypeScript code |

### Docker

```bash
# Build and run hub + example agent
cd deploy && docker compose up -d
```

The Docker Compose setup includes a hub and a sample agent that monitors the host (with Docker socket mounted).

## Project Structure

```
nexwatch/
├── cmd/
│   ├── hub/               # Hub entry point (PocketBase app)
│   └── agent/             # Agent entry point
├── internal/
│   ├── hub/
│   │   ├── api/           # Custom API routes and PocketBase hooks
│   │   ├── ws/            # WebSocket handler and agent registry
│   │   ├── metrics/       # Metric ingestion, queries, and downsampling
│   │   ├── alerts/        # Alert evaluation engine
│   │   ├── notify/        # Notification service and channels
│   │   └── migrations/    # PocketBase collection migrations
│   ├── agent/
│   │   ├── collector/     # Metric collectors (CPU, RAM, disk, net, Docker)
│   │   ├── transport/     # WebSocket client with auto-reconnect
│   │   └── config/        # YAML config + CLI flag parsing
│   └── shared/
│       ├── protocol/      # MessagePack message types
│       └── models/        # Shared data models
├── ui/                    # React frontend (Vite + TypeScript + Tailwind)
│   └── src/
│       ├── components/    # Reusable UI components
│       ├── pages/         # Route pages
│       ├── stores/        # Zustand state stores
│       ├── lib/           # PocketBase SDK instance
│       └── types/         # TypeScript type definitions
├── scripts/               # Install script for agent
├── deploy/                # Dockerfiles and docker-compose
├── .github/workflows/     # CI/CD pipelines
└── Makefile
```

## Tech Stack

| Component | Technology |
|-----------|------------|
| Hub | Go + [PocketBase](https://pocketbase.io) |
| Agent | Go (single binary, ~10 MB) |
| Frontend | React + TypeScript + Vite + Tailwind CSS |
| Charts | [uPlot](https://github.com/leeoniya/uPlot) |
| State Management | Zustand + PocketBase SDK real-time |
| Wire Protocol | WebSocket + MessagePack |
| Database | SQLite (via PocketBase, WAL mode) |
| CI/CD | GitHub Actions |
| Containers | Docker (GHCR) |

## CI/CD

- **CI** (`.github/workflows/ci.yml`): Runs on every push to `main` and on pull requests. Lints Go code with golangci-lint, runs tests with race detector, builds Go binaries for linux/amd64 and linux/arm64, and builds the React frontend.

- **Release** (`.github/workflows/release.yml`): Triggered by version tags (`v*`). Cross-compiles hub and agent for linux/{amd64,arm64} and darwin/{amd64,arm64}, creates a GitHub Release with attached binaries and SHA256 checksums, and builds + pushes multi-arch Docker images to `ghcr.io/cognidevai/nexwatch-hub` and `ghcr.io/cognidevai/nexwatch-agent`.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes and ensure tests pass (`go test ./...`)
4. Commit using [conventional commits](https://www.conventionalcommits.org/) format
5. Push to your branch and open a Pull Request

Please ensure:
- Go code passes `golangci-lint`
- TypeScript compiles without errors (`pnpm exec tsc --noEmit`)
- New features include appropriate tests

## License

[MIT](LICENSE) - CogniDevAI
