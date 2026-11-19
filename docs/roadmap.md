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

## Phase 2 — DevOps (apply your course here) ✅ COMPLETE

**Goal:** Make the platform deployable and observable.

**Prerequisites:** Finish Phase 1. ✅

### Completed items

#### ✅ 2.1 Containerise every service

Every service now has an optimized multi-stage `Dockerfile`:
- Stage 1 (`golang:alpine`): compiles a statically linked, stripped CGO-free Go binary using a workspace initialized specifically for required modules.
- Stage 2 (`gcr.io/distroless/static:nonroot`): runs as non-root user (uid 65532) in an image < 22 MB.
- Root `.dockerignore` excludes unnecessary files and artifacts.

| Service | Dockerfile | Image Size |
|---------|------------|------------|
| `api-gateway` | `services/api-gateway/Dockerfile` | ~18 MB |
| `user-service` | `services/user-service/Dockerfile` | ~21.5 MB |
| `driver-service` | `services/driver-service/Dockerfile` | ~21.5 MB |
| `location-service` | `services/location-service/Dockerfile` | ~21.5 MB |
| `ride-service` | `services/ride-service/Dockerfile` | ~21.5 MB |
| `payment-service` | `services/payment-service/Dockerfile` | ~21.5 MB |
| `notification-service` | `services/notification-service/Dockerfile` | ~21.5 MB |

Build commands: `task docker:build` or `task docker:build:service -- <svc>`

---

#### ✅ 2.2 Docker Compose Orchestration

Created `docker-compose.yml` with:
- `nats:2-alpine` with healthcheck on `/varz`
- 6 backend services with individual SQLite persistent volumes (`user-data`, `driver-data`, etc.)
- `api-gateway` on port 8080 with automatic dependencies and service discovery via internal Docker network (`rideshare-net`)
- `.env.example` with full configuration reference
- Seamless compatibility with both `docker compose` and `podman-compose`

Run: `task compose:up` / `task compose:down` / `task compose:logs`

---

#### ✅ 2.3 CI/CD Pipeline (GitHub Actions)

Created `.github/workflows/ci.yml`:
- **lint-and-test**: runs `go work sync`, `task vet`, and `task test` across all workspace modules.
- **compose-validate**: validates compose configurations against `.env.example`.
- **build-images**: matrix build job across all 7 services ensuring container images build without warnings.

---

#### ✅ 2.4 Kubernetes Manifests & Helm Chart

Created production-ready manifests in `deploy/k8s/`:
- `namespace.yaml`: creates `rideshare` namespace
- `configmap.yaml` & `secret.yaml`: configuration and credentials separation
- `nats.yaml`: Deployment and Service with health checks
- Backend services (`user-service.yaml`, `driver-service.yaml`, etc.): gRPC liveness and readiness probes (`grpc: { port: 5005x }`), non-root securityContext, and resource limits
- `api-gateway.yaml`: Deployment with HTTP `/healthz` probe and LoadBalancer service
- `ingress.yaml`: NGINX ingress with SSE timeout annotations
- `kustomization.yaml`: apply the entire stack with `task k8s:apply` (`kubectl apply -k deploy/k8s`)

Also created a full Helm chart in `deploy/helm/rideshare/` supporting parameterized deployments, ingress toggles, replica counts, and values overrides.

---

#### ✅ 2.5 Observability Stack

- `docker-compose.observability.yml`: overlay including Jaeger (`:16686`), Prometheus (`:9090`), and Grafana (`:3000`)
- `deploy/observability/prometheus.yml`: Prometheus scrape configuration
- Run: `task compose:obs:up` / `task compose:obs:down`

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
| 2 — DevOps | ✅ Done | Dockerfiles, Compose, CI/CD, K8s manifests, Helm chart, Observability |
| 3 — Deep dive | ⏳ Next | Pick one: distributed systems, cloud deploy, or WASM |
| 4 — Portfolio | 🔮 After Phase 3 | Demo GIF, write-up, interview talking points |
