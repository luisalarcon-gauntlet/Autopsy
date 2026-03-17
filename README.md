# Autopsy

**AI-powered Kubernetes support bundle analyzer**

Autopsy accepts a Replicated Troubleshoot support bundle (`.tar.gz`), extracts and
parses the cluster diagnostics, runs a multi-phase Claude-powered analysis, and
presents findings via a streaming web dashboard with a chat interface.

---

## Quickstart (Docker — recommended)

No Go installation required.

```bash
export ANTHROPIC_API_KEY=sk-ant-...   # optional: omit to run in stub mode
docker-compose up
# Open http://localhost:8080
```

## Quickstart (local Go dev)

Requires Go 1.22+, `air` (installed automatically by `make dev`).

```bash
cp .env.example .env        # add your API key
make dev                    # starts server on :8080 with hot-reload
```

---

## Prerequisites

| Path          | Docker        | Local dev                          |
|---------------|---------------|------------------------------------|
| Docker        | required      | optional                           |
| Go 1.22+      | not required  | required                           |
| air           | not required  | auto-installed by `make dev`       |
| kind          | not required  | required for `make demo-cluster`   |
| kubectl       | not required  | required for `make demo-bundle`    |

---

## Environment Variables

| Variable              | Default | Description                                          |
|-----------------------|---------|------------------------------------------------------|
| `ANTHROPIC_API_KEY`   | —       | Claude API key. If empty, runs in stub mode.         |
| `PORT`                | `8080`  | TCP port the server listens on.                      |
| `MAX_BUNDLE_MB`       | `250`   | Maximum upload size in megabytes.                    |
| `STUB_MODE`           | `false` | Force stub mode even when API key is present.        |
| `SESSION_TTL_MINUTES` | `30`    | How long sessions (and temp dirs) are retained.      |

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                       Browser                           │
│  HTMX + Tailwind CSS (CDN, zero build step)             │
└────────────┬──────────────────────────┬─────────────────┘
             │ HTTP / SSE               │ HTTP POST /upload
             ▼                          ▼
┌────────────────────────────────────────────────────────┐
│                  Go HTTP Server (stdlib)                │
│  handlers.go  ·  sse.go  ·  middleware.go              │
└──────┬──────────────┬──────────────────────────────────┘
       │              │
       ▼              ▼
┌──────────┐   ┌─────────────────────────────────────────┐
│ session/ │   │             analysis/                   │
│ store.go │   │  Phase 1: Triage  (structured JSON)     │
└──────────┘   │  Phase 2: Timeline (chronological)      │
               │  Phase 3: RCA     (streaming markdown)  │
               └──────────────┬──────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │  bundle/        │
                    │  extract.go     │
                    │  parser.go      │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  Anthropic API  │
                    │  Claude Sonnet  │
                    └─────────────────┘
```

---

## Project Structure

```
autopsy/
├── main.go                     # Entry point, router setup
├── go.mod / go.sum
├── Dockerfile                  # Multi-stage: builder + minimal runtime
├── docker-compose.yml
├── .air.toml                   # Live reload config
├── Makefile
├── .env.example
├── internal/
│   ├── bundle/
│   │   ├── extract.go          # Streaming tar.gz extraction
│   │   ├── parser.go           # Parse pods, events, logs
│   │   └── types.go            # BundleData and related types
│   ├── analysis/
│   │   ├── analyzer.go         # Orchestrates 3-phase AI analysis
│   │   ├── phases.go           # Phase result types
│   │   ├── prompts.go          # All prompt templates as constants
│   │   ├── cache.go            # SHA256 content-hash cache
│   │   └── stubs.go            # Pre-canned responses for stub mode
│   ├── session/
│   │   └── store.go            # Thread-safe session map with TTL
│   └── server/
│       ├── handlers.go         # HTTP handlers
│       ├── sse.go              # SSE writer helper
│       └── middleware.go       # Logging, panic recovery
└── templates/
    ├── layout.html
    ├── upload.html
    ├── report.html
    └── partials/
        ├── risk_card.html
        ├── timeline.html
        ├── rca.html
        └── chat.html
```

---

## Generating a Demo Bundle

```bash
make demo-cluster    # creates a kind cluster with scripted failures
make demo-bundle     # generates demo-bundle-TIMESTAMP.tar.gz
```

The demo cluster includes: OOMKilled pod, CrashLoopBackOff, ImagePullBackOff,
pending pod (insufficient resources), and a missing ConfigMap reference.
