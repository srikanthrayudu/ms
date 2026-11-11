# Project Roadmap

This document tracks the four learning phases for the RideShare platform. Phase 1 is complete.

---

## Phase 1 — Harden the Foundation ✅ COMPLETE

**Goal:** Make the codebase trustworthy before adding anything new.

### Completed items

#### ✅ Tests — 19 test packages, 0 failures

| Module | Packages tested | Coverage |
|--------|----------------|---------|
| `shared/auth` | JWT issue + validate | 4 cases |
| `shared/geo` | Haversine distance | 4 cases |
| `user-service/store` | CRUD, unique email, not-found | 5 cases |
| `user-service/service` | Register, Login, GetProfile, UpdateProfile | 7 cases |
| `driver-service/store` | Upsert, GetByID, SetAvailability | 5 cases |
| `driver-service/service` | OnboardDriver, SetAvailability, GetAvailability | 5 cases |
| `location-service/store` | Upsert, Get, LoadAll, Count | 5 cases |
| `ride-service/store` | Create, Get, Update, ListByUser | 6 cases |
| `ride-service/service` | RequestRide (matched/no-match/busy/nil-clients), UpdateRideStatus lifecycle, ListMyRides | 9 cases |
| `payment-service/store` | Create idempotent, GetByRideID, ListByUser | 4 cases |
| `payment-service/service` | ChargeRide, idempotency, ListPayments | 5 cases |
| `notification-service/store` | Create, List, CountUnread, MarkRead | 5 cases |
| `notification-service/service` | SendNotification, GetNotifications, MarkRead, hub live delivery | 5 cases |

**Test pattern used:**
- Store tests: real in-memory libsql via `shared/testutil.NewMemDB`
- Service tests: real in-memory store (no DB mocking needed)
- Ride-service: minimal `fakeLocationClient` and `fakeDriverClient` implementing the gRPC interfaces — no mock generation tool required

Run: `task test`

---

#### ✅ gRPC health checks

All six backend services now register `google.golang.org/grpc/health` via:

```go
grpc_health_v1.RegisterHealthServer(g, health.NewServer())
```

Health can be queried with:

```bash
# install once
go install google.golang.org/grpc/cmd/grpc-health-probe@latest

# check any service
grpc-health-probe -addr localhost:50051    # user-service
grpc-health-probe -addr localhost:50054    # ride-service
```

Kubernetes `readinessProbe` and `livenessProbe` can reference this endpoint.

---

#### ✅ JWT secret validation

Both `user-service` and `api-gateway` now call `cfg.Validate()` at startup:

- If `LOG_LEVEL` is not `debug` **and** `JWT_SECRET` equals the placeholder
  `super-secret-change-me`, the service **refuses to start** with a clear error.
- If `JWT_SECRET` is shorter than 16 characters, the service also refuses.

**During development:** set `LOG_LEVEL=debug` to allow the default secret.
**In production:** always set a strong `JWT_SECRET` environment variable.

---

#### ✅ Taskfile fixed

Replaced invalid `background: true` command property with a proper shell block
using `&` for backgrounding. Added 16 tasks including `task test`, `task vet`,
`task restart -- <service>`, `task logs:service -- <name>`, `task build:service -- <name>`.

---

## Phase 2 — DevOps (apply your course here)

**Goal:** Make the platform deployable and observable.

**Prerequisites:** Finish Phase 1. ✅

### 2.1 Containerise every service

Add a `Dockerfile` to each service. Use a multi-stage build:

```dockerfile
# Stage 1: build
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.work go.work.sum ./
COPY proto ./proto
COPY shared ./shared
COPY services/ride-service ./services/ride-service
RUN cd services/ride-service && \
    go build -ldflags="-s -w" -o /bin/ride-service ./cmd/ride-service

# Stage 2: minimal runtime (< 25 MB image)
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /bin/ride-service /ride-service
ENTRYPOINT ["/ride-service"]
```

Each service only copies its own source tree + shared dependencies, keeping images small.

**Recommended order:** api-gateway first (most visible), then one backend service,
then template the rest.

---

### 2.2 Docker Compose

Replace `task up` with a `docker-compose.yml`. Key points:

```yaml
version: "3.9"
services:
  nats:
    image: nats:2-alpine
    ports: ["4222:4222", "8222:8222"]

  user-service:
    build: { context: ., dockerfile: services/user-service/Dockerfile }
    environment:
      GRPC_ADDR: ":50051"
      JWT_SECRET: "${JWT_SECRET}"   # from .env file
      LOG_LEVEL: "info"
    depends_on: [nats]

  api-gateway:
    build: { context: ., dockerfile: services/api-gateway/Dockerfile }
    ports: ["8080:8080"]
    environment:
      JWT_SECRET: "${JWT_SECRET}"
      LOG_LEVEL: "info"
    depends_on:
      - user-service
      # ... all other services
```

After this, `docker compose up` replaces `task up`.

**Key learning:** services discover each other by DNS name (e.g. `user-service:50051`)
instead of `localhost:50051`. Update env vars in the Compose file.

---

### 2.3 CI/CD pipeline (GitHub Actions)

Create `.github/workflows/ci.yml`:

```yaml
name: CI
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - run: go install github.com/go-task/task/v3/cmd/task@latest
      - run: task build
      - run: task test
      - run: task vet

  build-images:
    needs: test
    runs-on: ubuntu-latest
    strategy:
      matrix:
        service:
          - user-service
          - driver-service
          - location-service
          - ride-service
          - payment-service
          - notification-service
          - api-gateway
    steps:
      - uses: actions/checkout@v4
      - run: |
          docker build \
            -f services/${{ matrix.service }}/Dockerfile \
            -t rideshare/${{ matrix.service }}:${{ github.sha }} \
            .
```

Each service image is built and tagged with the commit SHA.

---

### 2.4 Kubernetes manifests

For each service, write:

```
deploy/k8s/
  nats/
    deployment.yaml
    service.yaml
  user-service/
    deployment.yaml
    service.yaml
  ...
```

Key K8s patterns for this project:

- **liveness probe** using `grpc-health-probe`
- **readiness probe** using the same
- **Secret** for `JWT_SECRET` (not a ConfigMap — it's sensitive)
- **Service** type `ClusterIP` for backend services (internal only)
- **Service** type `LoadBalancer` or `NodePort` for api-gateway

NATS can be deployed via its [official Helm chart](https://github.com/nats-io/k8s).

---

### 2.5 Observability

Add OpenTelemetry tracing to every gRPC service:

```go
// In each service's main.go
import "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

g := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()),
)
```

Run Jaeger locally:
```bash
docker run -p 16686:16686 -p 4317:4317 jaegertracing/all-in-one:latest
```

Export metrics with Prometheus:
```go
import "go.opentelemetry.io/otel/exporters/prometheus"
```

---

## Phase 3 — Go Deeper (pick one track)

**Goal:** Deep expertise in one area.

**Prerequisites:** Phase 2 complete. Choose one track.

---

### Track A — Distributed Systems

**Estimated time:** 3–4 weeks

**Step 1 — NATS JetStream (durable event log)**

Upgrade from Core NATS to JetStream so events survive NATS restarts:

```go
js, _ := nc.JetStream()
// Create a stream covering all ride and payment subjects
js.AddStream(&nats.StreamConfig{
    Name:     "rideshare",
    Subjects: []string{"rides.>", "drivers.>", "payments.>"},
})
// Publish with guaranteed delivery
js.Publish("rides.r_1.completed", data)
```

JetStream gives you: at-least-once delivery, replay from any offset,
consumer groups (multiple service instances competing for the same events).

**Step 2 — Driver rating system**

After a ride completes:
1. Passenger submits a rating (1–5 stars) via the UI.
2. ride-service stores the rating.
3. driver-service computes a rolling average over the last 100 rides.
4. Rating is returned with every `GetDriver` call.

This is a good distributed transaction exercise: what happens if the rating
is stored but the average update fails?

**Step 3 — Ride cancellation with compensation**

If a driver cancels an already-matched ride:
1. ride-service sets status to `CANCELLED`.
2. ride-service emits `rides.<id>.cancelled`.
3. payment-service: if a charge already exists, mark it `REFUNDED`.
4. notification-service: notify both passenger and driver.
5. ride-service: re-attempt matching (request a new driver for the same ride).

This is the saga pattern — a sequence of local transactions coordinated
through events. No distributed locking required.

---

### Track B — Cloud / Platform Engineering

**Estimated time:** 2–3 weeks

**Step 1 — Deploy to Fly.io (easiest path for Go)**

```bash
# Install flyctl, then for each service:
fly launch --dockerfile services/ride-service/Dockerfile --name rideshare-ride
fly secrets set JWT_SECRET=your-strong-secret
fly deploy
```

Fly.io runs Docker containers globally with built-in TLS and
a free tier sufficient for a demo. Services discover each other
via Fly's internal DNS (`ride-service.internal`).

**Step 2 — Turso (remote libSQL)**

Change a single environment variable to migrate from local SQLite
to a globally distributed libSQL database:

```bash
# Before (local file)
DB_PATH=data/ride-service.db

# After (Turso cloud — same Go code, different DSN)
DB_PATH=libsql://your-ride-db.turso.io
```

Add `WithAuthToken(os.Getenv("TURSO_TOKEN"))` to the connector:

```go
db := sql.OpenDB(libsql.NewConnector(
    os.Getenv("DB_PATH"),
    libsql.WithAuthToken(os.Getenv("TURSO_TOKEN")),
))
```

This is the value proposition of libSQL — zero code change to go from
embedded SQLite to a production cloud database.

**Step 3 — Pulumi IaC (Go)**

Write your infrastructure in Go using the Pulumi SDK — the same language as the application:

```go
// infra/main.go
package main

import (
    "github.com/pulumi/pulumi-fly/sdk/go/fly"
    "github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
    pulumi.Run(func(ctx *pulumi.Context) error {
        app, _ := fly.NewApp(ctx, "rideshare-gateway", &fly.AppArgs{
            Name: pulumi.String("rideshare-gateway"),
        })
        // ... provision each service
        return nil
    })
}
```

---

### Track C — WASM / wasmCloud

**Estimated time:** 3–4 weeks (cutting-edge, sparse docs)

**Step 1 — Extract fare calculator to WASM (TinyGo)**

The fare calculation is currently a Go function:
```go
fare = 5.0 + 1.5*(distanceMeters/1000.0)
```

Move it to a WASM component:

```go
// wasm/fare/main.go  (compile with TinyGo)
//go:build tinygo

package main

//export calcFare
func calcFare(meters float64) float64 {
    return 5.0 + 1.5*(meters/1000.0)
}
func main() {}
```

Compile: `tinygo build -o wasm/fare/fare.wasm -target wasi ./wasm/fare`

Call from ride-service using `github.com/bytecodealliance/wasmtime-go`:

```go
engine := wasmtime.NewEngine()
module, _ := wasmtime.NewModuleFromFile(engine, "wasm/fare/fare.wasm")
instance, _ := wasmtime.NewInstance(wasmtime.NewStore(engine), module, nil)
calcFare := instance.GetFunc(store, "calcFare")
result, _ := calcFare.Call(store, distanceMeters)
fare := result.(float64)
```

The WASM component can be updated independently of the Go service binary.

**Step 2 — wasmCloud actor**

[wasmCloud](https://wasmcloud.com) lets you deploy WASM components as actors
that communicate with capability providers (databases, messaging) via WASI
interfaces. The fare, geo, and notification-template logic are ideal actors.

**Why this matters:** actors scale to zero, start in milliseconds, and can be
hot-swapped at runtime across a distributed cluster with zero downtime.

---

## Phase 4 — Portfolio-Ready

**Goal:** Turn the project into something you can show confidently.

**Prerequisites:** Phase 2 complete + at least one Phase 3 track.

**Checklist:**

- [ ] All `task test` passes, `task vet` clean
- [ ] `task up` starts the full stack from scratch in < 10 seconds
- [ ] A demo GIF/video: `task up` → register → request ride → complete trip → payment shown
  - Use [vhs](https://github.com/charmbracelet/vhs) or [asciinema](https://asciinema.org) to record it
- [ ] Architecture diagram as an image (export the ASCII diagram to SVG/PNG)
- [ ] Updated README with the demo GIF and a one-paragraph "What I built and why"
- [ ] One public write-up:
  - Blog post: "Building a microservices ride-sharing platform with Go, gRPC, NATS, and Datastar"
  - LinkedIn article with the demo GIF
  - Dev.to post with code snippets

**Things to highlight in interviews:**

| Concept | Evidence in this project |
|---------|-------------------------|
| Microservices decomposition | 7 independent services with separate DBs |
| Event-driven architecture | NATS subjects, ride lifecycle via pub/sub |
| gRPC + protobuf | Typed service contracts, generated clients |
| Real-time streaming | Datastar SSE, driver location updates |
| Idempotent operations | Payment `INSERT OR IGNORE` pattern |
| Testing strategy | In-memory DB tests, interface-based fakes, no mocks |
| Graceful degradation | NATS optional, services continue without it |
| Security basics | JWT auth, production secret validation |

---

## Summary

| Phase | Status | Key deliverable |
|-------|--------|----------------|
| 1 — Harden | ✅ Done | 19 test packages, health checks, JWT validation, fixed Taskfile |
| 2 — DevOps | ⏳ Next | Dockerfiles, Compose, CI/CD, K8s manifests |
| 3 — Deep dive | 🔮 After DevOps | Pick one: distributed systems, cloud deploy, or WASM |
| 4 — Portfolio | 🔮 After Phase 3 | Demo GIF, write-up, interview talking points |
