# DevOps Integration & WASM/WASMCloud Possibilities

## Can you apply a DevOps course to this project?

**Short answer: Yes — this project is an excellent DevOps practice target.** It has
exactly the right properties:

- Multiple independently deployable services (each service = one potential pipeline)
- Clear service boundaries (port, database, config)
- A defined build tool (`task build`, `go build`)
- gRPC health probes are easy to add
- NATS is a single process to manage

What follows is a map of typical DevOps course topics and how each one applies here.

---

## DevOps course topics → this project

### 1. Containerisation (Docker)

Each service gets its own minimal `Dockerfile`. Since all binaries are statically
linked Go executables, `FROM scratch` or `FROM gcr.io/distroless/static` images
are tiny (< 30 MB):

```dockerfile
# Example: services/ride-service/Dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.work go.work.sum ./
COPY proto ./proto
COPY shared ./shared
COPY services/ride-service ./services/ride-service
RUN cd services/ride-service && go build -o /bin/ride-service ./cmd/ride-service

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /bin/ride-service /ride-service
ENTRYPOINT ["/ride-service"]
```

Each service only copies the modules it needs (workspace-aware build).

---

### 2. Container Orchestration (Docker Compose → Kubernetes)

**Docker Compose** is the natural first step — one compose file replaces `task up`:

```yaml
# docker-compose.yml (sketch)
services:
  nats:
    image: nats:2-alpine
    ports: ["4222:4222", "8222:8222"]

  user-service:
    build: { context: ., dockerfile: services/user-service/Dockerfile }
    environment:
      GRPC_ADDR: ":50051"
      JWT_SECRET: "${JWT_SECRET}"
    depends_on: [nats]

  api-gateway:
    build: { context: ., dockerfile: services/api-gateway/Dockerfile }
    ports: ["8080:8080"]
    depends_on:
      - user-service
      - driver-service
      # ... all services
    environment:
      JWT_SECRET: "${JWT_SECRET}"
```

**Kubernetes** is the natural progression: each service becomes a `Deployment` + `Service`.
NATS can be deployed via its [official Helm chart](https://github.com/nats-io/k8s).
Services discover each other by K8s DNS names instead of `localhost:5005x`.

---

### 3. CI/CD (GitHub Actions / GitLab CI)

A multi-stage pipeline maps naturally:

```yaml
# .github/workflows/ci.yml  (sketch)
jobs:
  build-and-test:
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - run: task build
      - run: task test
      - run: task vet

  build-images:
    needs: build-and-test
    strategy:
      matrix:
        service: [user-service, driver-service, ride-service, payment-service,
                  notification-service, location-service, api-gateway]
    steps:
      - run: docker build -f services/${{ matrix.service }}/Dockerfile -t rideshare/${{ matrix.service }}:${{ github.sha }} .
      - run: docker push rideshare/${{ matrix.service }}:${{ github.sha }}
```

Each service can be built and published in parallel (matrix strategy).

---

### 4. Infrastructure as Code (Terraform / Pulumi)

The simplest DevOps-course IaC exercise:
- Use Terraform to provision a VM (or ECS / GKE cluster)
- Deploy NATS and the services as containers
- Output the gateway URL

Pulumi Go SDK is a great fit here because the whole stack is Go — the infrastructure
code and application code are the same language.

---

### 5. Observability (OpenTelemetry)

The Go OpenTelemetry SDK integrates easily. Add to each service:

```go
// In main.go
tp := initTracer("ride-service")
defer tp.Shutdown(ctx)

// Wrap the gRPC server
g := grpc.NewServer(
    grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
)
```

Traces can flow across service boundaries because gRPC supports W3C TraceContext
propagation. Ship traces to **Jaeger** (self-hosted) or **Tempo** (Grafana Cloud).

Metrics: expose a Prometheus `/metrics` endpoint in each service using
`go.opentelemetry.io/otel/exporters/prometheus`.

---

### 6. Secret Management (Vault / Kubernetes Secrets)

The key secret is `JWT_SECRET`. It currently defaults to a placeholder string.
In a DevOps setup:
- Store it in HashiCorp Vault or a K8s `Secret`
- Inject via environment variable at runtime
- Rotate without changing code (services read it from env on startup)

---

### 7. Health Probes

Add a gRPC health check to each service:

```go
import "google.golang.org/grpc/health/grpc_health_v1"
// ...
grpc_health_v1.RegisterHealthServer(g, health.NewServer())
```

Kubernetes `livenessProbe` and `readinessProbe` can then use `grpc-health-probe`.

---

### 8. Service Mesh (Istio / Linkerd)

Because services communicate via gRPC over plain TCP, a sidecar service mesh
(Istio or Linkerd) can be added with zero code changes:
- mTLS between services automatically
- Traffic policies (retries, circuit-breaking)
- Automatic Jaeger trace injection

---

## WASM / WASMCloud integration

**Is it possible? Yes, in meaningful ways — but with specific trade-offs.**

### Where WASM fits naturally

#### 1. Business logic components (wasmCloud / WASI P2)

The best candidates for WASM components in this architecture are **pure-function
business logic** pieces that have no I/O side effects:

| Logic unit | WASM component candidate? |
|------------|--------------------------|
| Fare calculation | ✅ Perfect — pure function: `fn calc_fare(meters: f64) -> f64` |
| Haversine / geo distance | ✅ Perfect — no I/O, math only |
| JWT validation | ✅ Reasonable — pure crypto |
| Notification template rendering | ✅ HTML/text generation, no I/O |
| Ride status machine | ✅ Pure state transitions |
| gRPC server | ⚠️ Possible via WASI HTTP but complex |
| Database queries (libSQL) | ❌ Needs I/O capabilities |
| NATS publish/subscribe | ❌ Needs networking capabilities |

#### 2. wasmCloud actors (cosmonic.com)

[wasmCloud](https://wasmcloud.com) lets you deploy WASM components as **actors**
that communicate with capability providers (databases, messaging) via interfaces.
The fare calculator, geo logic, and notification renderer could each be a wasmCloud
actor. The gRPC services would call them via the wasmCloud lattice instead of
calling Go functions directly.

This maps to **microservices within microservices** — a natural extension for a
DevOps course unit on WASM runtimes.

#### 3. Browser-side WASM (Leaflet extensions)

The Datastar frontend could load a WASM module for:
- Client-side route polyline rendering (compute the driving path in WASM)
- Fare estimation preview before the ride is requested
- Offline-capable service worker with WASM for cached map tiles

---

### How to add a WASM fare calculator (concrete example)

**Step 1:** Write the component in Go using TinyGo:

```go
// wasm/fare/main.go
//go:build tinygo

package main

import "math"

//export calcFare
func calcFare(distanceMeters float64) float64 {
    return 5.0 + 1.5*(distanceMeters/1000.0)
}

func main() {}
```

Compile: `tinygo build -o wasm/fare/fare.wasm -target wasi ./wasm/fare`

**Step 2:** Call it from ride-service using `github.com/bytecodealliance/wasmtime-go`:

```go
engine := wasmtime.NewEngine()
module, _ := wasmtime.NewModuleFromFile(engine, "wasm/fare/fare.wasm")
store := wasmtime.NewStore(engine)
instance, _ := wasmtime.NewInstance(store, module, nil)
calcFare := instance.GetFunc(store, "calcFare")
result, _ := calcFare.Call(store, distanceMeters)
fare := result.(float64)
```

**Step 3:** In a wasmCloud setup, deploy the component as an actor, wire it to
ride-service via the lattice, and update it independently of the Go service binary.

---

### Summary recommendation

| Phase | Action |
|-------|--------|
| Now (learning) | Add Dockerfiles + Compose file; wire a CI pipeline with GitHub Actions |
| Next | Add gRPC health probes + OpenTelemetry traces to Jaeger |
| Advanced | Extract fare calculator as a WASM component using TinyGo + wasmtime-go |
| Production path | wasmCloud actors for stateless logic, K8s for stateful services, Istio for mTLS |

The architecture is deliberately WASM-friendly: stateless logic lives in service.go
files, is already separated from I/O, and can be lifted into WASM components without
changing the gRPC interface.
