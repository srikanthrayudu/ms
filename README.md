# RideShare — A Microservices-Based Smart Ride-Sharing Platform

A learning project that demonstrates a microservices architecture for a ride-sharing
platform: a passenger requests a ride, the platform matches a nearby driver, and both
sides track the trip in real time as the driver drives to the pickup, picks up the
passenger and completes the trip.

Built with **Go** (gRPC microservices), **NATS** (eventing), **libSQL** (per-service
SQLite databases) and a **Datastar** frontend served by an API gateway over HTTP + SSE.

---

## Architecture

```
                       ┌──────────────────────── HTTP / SSE ─────────────────────────┐
                       │                  Datastar UI (browser)                      │
                       └──────────────────────────┬──────────────────────────────────┘
                                                  │
                                          ┌───────▼────────┐
                                          │  api-gateway   │  :8080
                                          └──┬──┬──┬──┬───┘
                  gRPC                       │  │  │  │
        ┌───────────────┬────────────────────┘  │  └──────────────────────┐
        ▼               ▼                       ▼                         ▼
 user-service    driver-service       location-service              ride-service
   :50051          :50052               :50053                        :50054
                                                     ╲             ╱
                                                      NATS :4222
                                                     ╱             ╲
                                        payment-service    notification-service
                                           :50055               :50056
```

| Service              | Port  | Responsibility                                          | Store                    |
|----------------------|-------|---------------------------------------------------------|--------------------------|
| api-gateway          | 8080  | HTTP + SSE UI, JWT auth, proxies to gRPC services       | —                        |
| user-service         | 50051 | User accounts + JWT issuance                            | libSQL `user-service.db` |
| driver-service       | 50052 | Driver onboarding, profiles, availability               | libSQL `driver-service.db` |
| location-service     | 50053 | Live driver GPS + nearby-driver queries (Haversine)     | in-memory + libSQL       |
| ride-service         | 50054 | Ride lifecycle + matching + fare calculation            | libSQL `ride-service.db` |
| payment-service      | 50055 | Fare payment records, NATS-driven auto-charge           | libSQL `payment-service.db` |
| notification-service | 50056 | In-app notifications, NATS subscriber, SSE fan-out      | libSQL `notification-service.db` |

**Communication:** gRPC between services (protobuf under `proto/`), NATS for async
events (`rides.<rideID>.<event>` and `drivers.<driverID>.location`), HTTP + SSE between
the gateway and the browser.

**Databases:** each service owns its own libSQL (SQLite) file in `data/`. The `libsql`
driver is used throughout; local files use a pure-Go SQLite engine, and a service can be
pointed at a remote Turso database (`libsql://...`) by changing only its `DB_PATH`.

---

## Tech stack

- **Go 1.24+** with a [Go workspace](go.work) (one module per service + `proto` + `shared`)
- **gRPC / protobuf** generated with [buf](https://buf.build)
- **NATS** as the message broker
- **libSQL** (`github.com/tursodatabase/libsql-client-go`) over a pure-Go SQLite engine
- **Datastar** ([`github.com/starfederation/datastar-go`](https://github.com/starfederation/datastar-go)) for the reactive frontend over Server-Sent Events
- **Leaflet** for the live map
- **JWT** (HS256) auth issued by `user-service`, validated by the gateway
- **Taskfile** for orchestration

---

## Prerequisites

Go 1.24+ is required. Everything else is installed as Go binaries:

```bash
go install github.com/go-task/task/v3/cmd/task@latest
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/nats-io/nats-server/v2@latest
```

Ensure `$(go env GOPATH)/bin` is on your `PATH`.

> No Docker is required — NATS runs as a native Go binary and libSQL is a pure-Go library.

---

## Documentation

Detailed documentation lives in `docs/`:

| File | Contents |
|------|----------|
| [`docs/architecture.md`](docs/architecture.md) | Full system design, service responsibilities, data flow, ADRs |
| [`docs/api-reference.md`](docs/api-reference.md) | Every HTTP + gRPC + NATS endpoint |
| [`docs/runbook.md`](docs/runbook.md) | Setup, common tasks, env vars, troubleshooting |
| [`docs/devops-and-wasm.md`](docs/devops-and-wasm.md) | DevOps course integration guide + WASM/wasmCloud possibilities |
| [`docs/roadmap.md`](docs/roadmap.md) | Phase 1–4 learning roadmap with concrete implementation steps |

---

## Quick start

```bash
task up        # generate proto, build all services, start NATS + everything
               # Gateway: http://localhost:8080  (Ctrl-C to stop)
```

In another terminal:

```bash
task logs     # tail all service logs
task down     # stop everything
```

### Other tasks

```bash
task proto:generate   # regenerate gRPC code from proto/ definitions
task tidy             # go work sync + go mod tidy for every module
task build            # build all binaries into ./bin
task nats             # run just the NATS server
task test             # run all tests
task clean            # remove ./bin and ./data
```

---

## Using the app

Open http://localhost:8080 in two browser windows/tabs.

**Driver** (tab 1):
1. Register with role **Driver** → you land on the driver dashboard.
2. Onboard a vehicle (license / plate / model).
3. Toggle **Go online**. Your marker appears near the city centre and the page
   starts simulating your driving, reporting your GPS to `location-service`.

**Passenger** (tab 2):
1. Register with role **Passenger** → you land on the dashboard.
2. Click **📍 Set pickup** then click the map (near the centre to match the driver).
3. Click **🏁 Set dropoff** then click the map again.
4. Click **Request ride**. You are redirected to the live tracking page, the ride is
   matched to the driver, and you watch the driver marker move toward you.

**Driver** (back to tab 1): the assigned ride appears automatically. Use
**Start trip → Pick up passenger → Complete trip** to drive the lifecycle. The passenger
sees each status change live, and the fare is shown when the trip completes.

---

## Configuration

Each service reads from environment variables with defaults (see the `internal/config`
files). Important ones:

| Variable              | Default                          | Used by              |
|-----------------------|----------------------------------|----------------------|
| `HTTP_ADDR`           | `:8080`                          | api-gateway          |
| `GRPC_ADDR`           | `:5005x`                         | each backend service |
| `DB_PATH`             | `data/<service>.db`              | each backend service |
| `NATS_URL`            | `nats://127.0.0.1:4222`          | ride, location, gateway |
| `JWT_SECRET`          | `super-secret-change-me`         | user-service, gateway |
| `LOCATION_SERVICE_ADDR` | `localhost:50053`              | ride-service          |
| `DRIVER_SERVICE_ADDR` | `localhost:50052`                | ride-service          |

> `JWT_SECRET` must match between `user-service` (issuer) and `api-gateway` (validator).

---

## Project structure

```
ms/
├── go.work                 # Go workspace (all modules)
├── Taskfile.yml            # build / run / stop orchestration
├── proto/                  # protobuf definitions + generated Go code
│   ├── buf.yaml / buf.gen.yaml
│   ├── user|driver|location|ride/v1/*.proto
│   └── gen/                # generated *.pb.go (by `task proto:generate`)
├── shared/                 # shared Go module: log, config, auth, events, geo, grpcclient
└── services/
    ├── api-gateway/        # HTTP + SSE gateway, Datastar templates, embedded assets
    │   ├── cmd/api-gateway/main.go
    │   ├── internal/{config,clients,auth,server}
    │   └── web/{templates,static}   # embedded into the binary
    └── {user,driver,location,ride}-service/
        ├── cmd/<svc>/main.go
        └── internal/{config,store,service}
```

Each backend service follows the same idiom: `cmd/` entrypoint, `internal/config`
(env loading), `internal/store` (libSQL repository), `internal/service` (gRPC handler).

---

## Ride lifecycle

`REQUESTED → MATCHED → ON_THE_WAY → ONGOING → COMPLETED` (or `CANCELLED`).

`ride-service` matches by querying `location-service.GetNearbyDrivers` and confirming
each candidate is available via `driver-service.GetAvailability`; it publishes a NATS
event on every transition. `location-service` broadcasts every GPS fix. The gateway
subscribes to these subjects and streams Datastar patches (DOM + signals) and
imperative map updates to the browser over SSE.
