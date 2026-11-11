# Runbook

Operational reference for the RideShare platform.

---

## Prerequisites

Go 1.24+. Install Go binaries once:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"

go install github.com/go-task/task/v3/cmd/task@latest
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/nats-io/nats-server/v2@latest
```

Add `$(go env GOPATH)/bin` to your shell's `PATH` permanently (e.g. in `.bashrc`).

No Docker required.

---

## Common tasks

| Goal | Command |
|------|---------|
| Build all binaries | `task build` |
| Start entire stack | `task up` |
| Stop entire stack | `task down` |
| Tail all logs | `task logs` |
| Tail one service | `task logs:service -- ride` |
| Restart one service | `task restart -- ride-service` |
| Run tests | `task test` |
| Lint all modules | `task vet` |
| Regen proto code | `task proto:generate` |
| Tidy all modules | `task tidy` |
| Clean build + DBs | `task clean` |
| Remove log files | `task clean:logs` |

---

## Service ports

| Service | Port | Protocol |
|---------|------|----------|
| api-gateway | 8080 | HTTP + SSE |
| user-service | 50051 | gRPC |
| driver-service | 50052 | gRPC |
| location-service | 50053 | gRPC |
| ride-service | 50054 | gRPC |
| payment-service | 50055 | gRPC |
| notification-service | 50056 | gRPC |
| NATS (clients) | 4222 | NATS |
| NATS (monitoring) | 8222 | HTTP |

---

## Environment variables

Each service reads environment variables at startup. All have sensible defaults.

### Common to all services

| Variable | Default | Description |
|----------|---------|-------------|
| `GRPC_ADDR` | `:5005x` | gRPC listen address |
| `DB_PATH` | `data/<svc>.db` | libSQL file path |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

### Services with NATS

| Variable | Default | Description |
|----------|---------|-------------|
| `NATS_URL` | `nats://127.0.0.1:4222` | NATS server URL |

### ride-service

| Variable | Default | Description |
|----------|---------|-------------|
| `LOCATION_SERVICE_ADDR` | `localhost:50053` | gRPC address of location-service |
| `DRIVER_SERVICE_ADDR` | `localhost:50052` | gRPC address of driver-service |

### api-gateway

| Variable | Default | Description |
|----------|---------|-------------|
| `HTTP_ADDR` | `:8080` | HTTP listen address |
| `JWT_SECRET` | `super-secret-change-me` | **Must match user-service JWT_SECRET** |
| `USER_SERVICE_ADDR` | `localhost:50051` | |
| `DRIVER_SERVICE_ADDR` | `localhost:50052` | |
| `LOCATION_SERVICE_ADDR` | `localhost:50053` | |
| `RIDE_SERVICE_ADDR` | `localhost:50054` | |
| `PAYMENT_SERVICE_ADDR` | `localhost:50055` | |
| `NOTIFICATION_SERVICE_ADDR` | `localhost:50056` | |

> **Security:** Always set a strong `JWT_SECRET` in production. The same secret must be
> set on both `user-service` (issuer) and `api-gateway` (validator).

---

## Log files

When started via `task up`, logs are written to `/tmp/ms/`:

```
/tmp/ms/nats.log
/tmp/ms/user.log
/tmp/ms/driver.log
/tmp/ms/location.log
/tmp/ms/ride.log
/tmp/ms/payment.log
/tmp/ms/notification.log
/tmp/ms/gateway.log
```

---

## Database files

Each service creates its SQLite database in `./data/` relative to the working
directory. Remove them to reset state:

```bash
task clean        # removes ./bin and ./data
task clean:logs   # removes /tmp/ms/*.log
```

---

## Regenerating protobuf code

Modify a `.proto` file under `proto/`, then:

```bash
task proto:generate
```

This runs `buf generate` from the `proto/` directory and writes generated files to
`proto/gen/`. After that, run `task tidy` to update `go.sum` files.

---

## Adding a new service

1. Create `proto/<service>/v1/<service>.proto`, run `task proto:generate`.
2. Create `services/<service-name>/` with the same layout as an existing service:
   ```
   cmd/<service-name>/main.go
   internal/config/config.go
   internal/store/store.go
   internal/service/service.go
   go.mod
   ```
3. Add the module path to `go.work` under `use (...)`.
4. Add build / tidy entries in `Taskfile.yml`.
5. If the gateway needs to call it, add a client in `services/api-gateway/internal/clients/clients.go` and add its address to the gateway config.

---

## Troubleshooting

### Services won't start

- Check `$(go env GOPATH)/bin` is on `PATH` before running `task`.
- Run `task build` first to ensure binaries exist in `bin/`.

### "no servers available" NATS warning

Normal if NATS hasn't started yet. Services retry 5 times with 500 ms wait.
They function without NATS (ride streams and auto-payment won't work).
Start NATS first: `task nats` in a separate terminal, or use `task up`.

### Port already in use

A previous run's process is still alive. Run `task down` to kill all managed processes.

### Database "unable to open file"

The `data/` directory doesn't exist. Each service creates it with `os.MkdirAll` on startup.
If running a binary directly, run it from the workspace root where `data/` can be created.

### JWT mismatch (401 on all requests)

`JWT_SECRET` differs between `user-service` and `api-gateway`. Both default to
`super-secret-change-me`; set the same value explicitly in production.
