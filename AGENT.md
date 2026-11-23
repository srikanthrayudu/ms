# AGENT.md — Developer & AI Agent Context for RideShare Microservices

This document provides complete architectural, operational, and development context for AI coding agents (OpenAI Codex, Claude, Cursor, Copilot) working in this repository.

---

## 1. Project Overview

**RideShare** is a production-grade distributed microservices ride-sharing platform written in **Go 1.24+**.
It demonstrates modern distributed systems, cloud-native DevOps, and event-driven microservices architecture:
- **Core Domain:** Passenger requests a ride (Solo or Carpool/Pool), the system matches nearby drivers using spatial Haversine distance, streams live GPS telemetry over Server-Sent Events (SSE) to a Datastar frontend, drives trip lifecycles, computes dynamic fares, charges payments, and manages distributed compensation transactions (Saga pattern) upon cancellation.
- **Microservices Count:** 7 independent services + NATS message broker + Observability stack.

---

## 2. System Architecture & Component Map

```
                             ┌──────────────────────── HTTP / SSE ─────────────────────────┐
                             │              Datastar Reactive UI (Browser)                 │
                             └──────────────────────────────┬──────────────────────────────┘
                                                            │
                                                    ┌───────▼────────┐
                                                    │  api-gateway   │  :8080 (metrics: :8080/metrics)
                                                    └──┬──┬──┬──┬───┘
                            gRPC                       │  │  │  │
                  ┌─────────────┬──────────────────────┘  │  └──────────────────────┐
                  ▼             ▼                         ▼                         ▼
           user-service   driver-service         location-service              ride-service
             :50051         :50052                 :50053                        :50054
         (metrics: 9090)(metrics: 9091)        (metrics: 9092)               (metrics: 9093)
                                                               ╲             ╱
                                                                NATS :4222
                                                               ╱             ╲
                                                  payment-service    notification-service
                                                     :50055               :50056
                                                 (metrics: 9094)      (metrics: 9095)
```

### Microservice Directory & Port Matrix

| Service | Path | gRPC Port | Metrics Port | Storage Engine | Responsibility |
|---|---|---|---|---|---|
| **api-gateway** | `services/api-gateway` | — | `:8080/metrics` | In-memory / stateless | HTTP + SSE server, JWT auth proxy, Datastar templates, Leaflet maps |
| **user-service** | `services/user-service` | `:50051` | `:9090` | libSQL SQLite (`user-service.db`) | User registration, password hashing (bcrypt), JWT issuance |
| **driver-service** | `services/driver-service` | `:50052` | `:9091` | libSQL SQLite (`driver-service.db`) | Driver vehicle onboarding, profiles, availability toggle |
| **location-service**| `services/location-service`| `:50053` | `:9092` | Memory + SQLite (`location-service.db`) | Driver GPS fixes, spatial nearby-driver queries (Haversine) |
| **ride-service** | `services/ride-service` | `:50054` | `:9093` | libSQL SQLite (`ride-service.db`) | Ride lifecycle, matching, carpool/pool pooling, fare calculation |
| **payment-service**| `services/payment-service` | `:50055` | `:9094` | libSQL SQLite (`payment-service.db`) | Idempotent payments, NATS auto-charge on completion, Saga refunds |
| **notification-service**| `services/notification-service`| `:50056`| `:9095`| libSQL SQLite (`notification-service.db`)| SSE push delivery, NATS event notifications fanout |

---

## 3. Shared Modules & Libraries

- **`proto/`**: Protobuf definitions (`user`, `driver`, `location`, `ride`, `payment`, `notification`) managed by [Buf](https://buf.build). Generated code in `proto/gen/`.
- **`shared/auth`**: JWT token creation and validation (`golang-jwt/jwt/v5`).
- **`shared/config`**: Environment variable helper (`Env(key, default)`).
- **`shared/events`**: NATS subject definitions and typed event schemas (`RideEvent`, `DriverLocationEvent`).
- **`shared/geo`**: Haversine distance calculations, `SoloFare`, and `PoolFare` with detour surcharges.
- **`shared/grpcclient`**: Standard gRPC dialer with auto-wired OpenTelemetry stats handler and keepalives.
- **`shared/log`**: Structured JSON / text logger using Go standard `log/slog`.
- **`shared/telemetry`**: OpenTelemetry W3C TraceContext propagator, gRPC stats handlers (`ServerStatsHandler`, `ClientStatsHandler`), and Prometheus `/metrics` background server.
- **`shared/testutil`**: In-memory libSQL test harness (`NewMemDB(t, migrateFunc)`).

---

## 4. Key Architectural Patterns & Invariants

### A. Multi-Module Go Workspace
The root contains `go.work` linking all service modules, `proto`, and `shared`.
- **Rule:** Never run `go test ./...` from root because root is a multi-module workspace without code. Always run `task test` or pass explicit module paths: `go test ./shared/... ./services/...`.

### B. Database Isolation & libSQL
- Each microservice strictly owns its own SQLite database in `data/<service>.db` (or `/app/data/<service>.db` in containers).
- Uses `github.com/tursodatabase/libsql-client-go/libsql` and `modernc.org/sqlite` (pure Go, CGO-free).
- A service can point to a distributed Turso database simply by setting `DB_PATH=libsql://...`.

### C. Testing Strategy (No Mock Generators)
- **Store Tests:** Use real in-memory SQLite instances via `shared/testutil.NewMemDB(t, migrateFunc)`.
- **Service Tests:** Use real in-memory stores and interface-based fakes (e.g., `fakeLocationClient`, `fakeDriverClient`). Do not introduce heavyweight mock generators like gomock or mockery.

### D. Distributed Saga Pattern (Ride Cancellation)
When a ride is cancelled (`POST /api/rides/{id}/cancel`):
1. `ride-service` marks status `CANCELLED` and publishes `rides.<id>.cancelled` on NATS with `DriverID` and `PassengerID`.
2. `payment-service` listens on `rides.*.cancelled` and executes compensation by setting charge status to `REFUNDED`.
3. `notification-service` alerts both driver and passenger.

### E. Dynamic Ride Pooling (Carpooling)
- `ride.proto` defines `RideType` (`SOLO` vs `POOL`), `RiderStatus`, and `Rider`.
- `ride-service` dynamically matches incoming pool requests with nearby active pool rides within radius (`tryJoinPool`).
- Fares are dynamically split with detour compensation using `shared/geo.PoolFare`.

### F. Observability Standard
- Every gRPC server must register `telemetry.ServerStatsHandler()`.
- Every gRPC client dials via `shared/grpcclient.Dial()` which injects `telemetry.ClientStatsHandler()`.
- Every service starts Prometheus metrics on `METRICS_ADDR` via `telemetry.StartMetricsServer(cfg.MetricsAddr)`.

### G. Security & Container Constraints
- Microservice containers use multi-stage builds on `gcr.io/distroless/static:nonroot` (< 22 MB each).
- Run as non-root user `uid 65532`.
- `CGO_ENABLED=0` static linking only.
- In production (`LOG_LEVEL != "debug"`), services refuse to start if `JWT_SECRET` is the default placeholder or < 16 chars.

---

## 5. Standard Development Commands (`Taskfile`)

The repository uses [go-task](https://taskfile.dev):

```bash
# Testing & Verification
task test                   # Run all unit and integration tests across workspace
task vet                    # Run go vet across all 8 modules

# Code Generation
task proto:generate         # Regenerate protobuf and gRPC Go code using buf

# Local Running (Native Go Binaries)
task build                  # Compile all 7 binaries to ./bin
task up                     # Start NATS + all 7 services in background
task down                   # Stop all local background services
task logs                   # Tail all logs

# Docker & Container Compose
task docker:build           # Build all 7 multi-stage distroless images
task compose:up             # Launch microservices via Compose
task compose:down           # Stop Compose stack
task compose:obs:up         # Launch with Prometheus (:9090), Grafana (:3000), Jaeger (:16686)

# Podman & Podman Desktop
task podman:build           # Build images using podman
task podman:play            # Play full Kubernetes stack via 'podman play kube'
task podman:down            # Stop played pods

# Kubernetes (Kind / Minikube)
task k8s:kind:load          # Load local images into Kind cluster
task k8s:apply              # Apply deploy/k8s/ manifests via Kustomize
task k8s:status             # View pods, services, and ingress status
task k8s:delete             # Tear down Kubernetes resources
```

---

## 6. Directory Structure

```
ms/
├── go.work                           # Go workspace linking all modules
├── Taskfile.yml                      # Task runner recipes
├── README.md                         # Project overview and quickstart
├── AGENT.md                          # AI agent & developer guidelines (this file)
├── docker-compose.yml                # Microservices orchestration
├── docker-compose.observability.yml  # Prometheus, Jaeger, Grafana overlay
├── proto/                            # Protobuf source files & generated Go code
│   ├── buf.yaml / buf.gen.yaml
│   └── */v1/*.proto
├── shared/                           # Shared library module (ms/shared)
│   ├── auth, config, events, geo, grpcclient, log, telemetry, testutil
├── services/
│   ├── api-gateway/                  # HTTP + SSE gateway, Datastar templates
│   │   ├── cmd/api-gateway/main.go
│   │   ├── internal/{auth,clients,config,server}
│   │   └── web/{static,templates}
│   └── {user,driver,location,ride,payment,notification}-service/
│       ├── cmd/<svc>/main.go
│       └── internal/{config,service,store}
├── deploy/
│   ├── k8s/                          # Kubernetes manifests & Kustomize
│   │   ├── rideshare-stack.yaml      # All-in-one manifest for Podman Desktop
│   │   ├── observability.yaml        # Prometheus & Jaeger k8s specs
│   │   └── kustomization.yaml
│   ├── helm/rideshare/               # Production Helm chart
│   └── observability/prometheus.yml  # Prometheus scrape configuration
└── docs/
    ├── architecture.md               # Detailed system design & ADRs
    ├── api-reference.md              # Full HTTP, gRPC, and NATS endpoint specs
    ├── podman-desktop-k8s.md         # Podman Desktop & Kubernetes walkthrough
    ├── runbook.md                    # Operational runbook
    └── roadmap.md                    # Project development phase tracker
```

---

## 7. Instructions for Modifying Code

1. **Before making changes:** Check `git status` and run `task test` to verify baseline health.
2. **When modifying Protobuf (`proto/**/*.proto`):**
   - Run `task proto:generate` to refresh `proto/gen/`.
   - Update corresponding gRPC server implementations and client calls.
3. **When modifying backend service `main.go`:**
   - Preserve `telemetry.Init("<service-name>")`.
   - Preserve `grpc.NewServer(telemetry.ServerStatsHandler())`.
   - Preserve `telemetry.StartMetricsServer(cfg.MetricsAddr)`.
   - Preserve `grpc_health_v1.RegisterHealthServer(g, health.NewServer())`.
4. **When modifying database schema:**
   - Add migrations in `internal/store/store.go` inside the `Migrate()` method using `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS`.
   - Write corresponding tests in `internal/store/store_test.go`.
5. **After making changes:**
   - Always run `task vet` to ensure clean static analysis.
   - Always run `task test` to confirm all unit and integration tests pass.
