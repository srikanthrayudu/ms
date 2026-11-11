# Architecture

## Overview

RideShare is a **microservices-based** ride-sharing platform built entirely in Go.
Each service is an independent deployable unit with its own database, communicating
with peers via two channels:

- **gRPC** — synchronous request/response between services and the gateway.
- **NATS** — asynchronous publish/subscribe for event-driven workflows (ride lifecycle,
  payment charging, notification fan-out).

The browser talks only to the **API Gateway** over HTTP + SSE. Datastar on the client
side binds reactive signals to SSE event streams, so the UI updates live without
any client-side framework beyond Datastar itself.

---

## System diagram

```
  Browser (Datastar v1.0.5 + Leaflet)
      │  HTTP + Server-Sent Events (SSE)
      ▼
  ┌──────────────────────────────────────────────────────────────────┐
  │                        api-gateway :8080                         │
  │  • JWT cookie auth        • Datastar templates (embedded)        │
  │  • SSE aggregation        • Route → gRPC proxy                  │
  └────┬──────┬──────┬──────┬──────┬──────────────┬─────────────────┘
       │gRPC  │gRPC  │gRPC  │gRPC  │gRPC          │gRPC
       ▼      ▼      ▼      ▼      ▼              ▼
  user     driver  location  ride  payment    notification
  :50051  :50052  :50053  :50054  :50055      :50056
       │      │      │      │      │              │
       └──────┴──────┴──────┴──────┴──────────────┘
                              │
                           NATS :4222
                     rides.*.{matched,started,...}
                     drivers.*.location
```

---

## Services

### user-service (`:50051`)

**Responsibility:** User accounts and JWT issuance.

| Method | Description |
|--------|-------------|
| `Register` | Create account (passenger or driver). Passwords bcrypt-hashed. |
| `Login` | Validate credentials, issue HS256 JWT (24 h TTL). |
| `GetProfile` | Fetch user's public profile. |
| `UpdateProfile` | Update name and phone. |

**Store:** `data/user-service.db` — `users` table with unique email, hashed password,
role.

**Events emitted:** none.

---

### driver-service (`:50052`)

**Responsibility:** Driver onboarding, vehicle info, online/offline availability.

| Method | Description |
|--------|-------------|
| `OnboardDriver` | Register driver details for an existing user (upsert). |
| `GetDriver` | Return driver profile including rating and vehicle. |
| `SetAvailability` | Toggle online/offline status. |
| `GetAvailability` | Return current availability. |

**Store:** `data/driver-service.db` — `drivers` table, bool stored as `INTEGER 0/1`.

**Events emitted:** none.

---

### location-service (`:50053`)

**Responsibility:** Ingest GPS fixes, answer nearby-driver queries for matching.

| Method | Description |
|--------|-------------|
| `UpdateLocation` | Record driver lat/lng; upsert DB row; publish NATS event. |
| `GetLocation` | Return latest known position from memory cache. |
| `GetNearbyDrivers` | Return drivers within `radius_meters` using Haversine formula. |

**Store:** `data/location-service.db` — `driver_locations` table (upsert on conflict).
In-memory `sync.RWMutex` cache for sub-millisecond nearby queries.

**Events emitted:** `drivers.<driverID>.location` on every GPS update.

---

### ride-service (`:50054`)

**Responsibility:** Ride lifecycle and driver matching.

| Method | Description |
|--------|-------------|
| `RequestRide` | Create ride; call `location-service.GetNearbyDrivers`; check each candidate with `driver-service.GetAvailability`; assign first available driver. |
| `GetRide` | Fetch current ride state. |
| `UpdateRideStatus` | Advance lifecycle; compute fare on `COMPLETED`. |
| `ListMyRides` | List rides for a user (by role). |

**Fare formula:** `$5.00 + $1.50 × distance_km`

**Lifecycle:**
```
REQUESTED → MATCHED → ON_THE_WAY → ONGOING → COMPLETED
                                            → CANCELLED
```

**Store:** `data/ride-service.db` — `rides` table.

**Events emitted on each status change:**
- `rides.<rideID>.matched`
- `rides.<rideID>.started`
- `rides.<rideID>.ongoing`
- `rides.<rideID>.completed`
- `rides.<rideID>.cancelled`

---

### payment-service (`:50055`)

**Responsibility:** Fare payment records and earnings ledger.

| Method | Description |
|--------|-------------|
| `ChargeRide` | Idempotently create a payment for a ride (second call returns existing). |
| `GetPayment` | Fetch payment for a ride. |
| `ListPayments` | Payments for a user (passenger or driver), with earnings total. |

**NATS subscriber:** `rides.*.completed` → automatically creates payment record.
Both the NATS subscriber and an explicit `ChargeRide` call are idempotent via
`INSERT OR IGNORE` (unique constraint on `ride_id`).

**Store:** `data/payment-service.db` — `payments` table.

---

### notification-service (`:50056`)

**Responsibility:** Persistent in-app notifications with live push to the gateway.

| Method | Description |
|--------|-------------|
| `SendNotification` | Persist notification; fan out to subscribed SSE streams. |
| `GetNotifications` | Fetch recent notifications (supports `unread_only`). |
| `MarkRead` | Mark specific or all notifications read. |

**In-memory hub:** `internal/hub.Hub` — thread-safe map of `userID → []chan`.
The gateway polls every 5 s via gRPC; a streaming RPC would be a natural upgrade.

**NATS subscriber:** `rides.>` → fans out type-specific notifications to matched
driver (passenger notifications are pushed via the ride SSE stream).

**Store:** `data/notification-service.db` — `notifications` table.

---

### api-gateway (`:8080`)

**Responsibility:** Single entry point for all browser traffic.

- Validates JWT cookie on every request (via shared `auth` middleware).
- Serves Datastar HTML templates embedded into the binary (`embed.FS`).
- Proxies REST endpoints to the appropriate gRPC services.
- Opens NATS subscriptions per-ride and per-driver for live SSE streams.
- Polls notification-service every 5 s to push unread notification badges/toasts.

**Routes summary:**

| Method | Path | Handler |
|--------|------|---------|
| `GET` | `/` | Landing page |
| `GET` | `/login`, `/register` | Auth pages |
| `GET` | `/dashboard` | Passenger ride-request page |
| `GET` | `/rides/{id}` | Live ride tracking page |
| `GET` | `/driver` | Driver onboarding / dashboard |
| `GET` | `/payments` | Payment history |
| `POST` | `/api/login`, `/api/register`, `/api/logout` | Auth |
| `POST` | `/api/rides` | Request a ride |
| `GET` | `/api/rides/{id}/stream` | Live ride SSE |
| `POST` | `/api/rides/{id}/start,pickup,complete` | Advance lifecycle |
| `POST` | `/api/driver/onboard`, `availability`, `location` | Driver management |
| `GET` | `/api/driver/stream` | Live driver SSE |
| `GET` | `/api/payments` | Payment history (JSON/Datastar) |
| `GET` | `/api/notifications/stream` | Notification SSE |
| `POST` | `/api/notifications/read` | Mark read |

---

## Shared module (`ms/shared`)

| Package | Contents |
|---------|----------|
| `auth` | JWT issue (`HS256`) and validate |
| `config` | `Env(key, fallback)` helper |
| `events` | NATS subject constants, `RideEvent` and `DriverLocationEvent` structs |
| `geo` | Haversine great-circle distance |
| `grpcclient` | Insecure `grpc.NewClient` dial helper |
| `log` | `slog.Logger` factory |

---

## Data flow: request a ride

```
1. Passenger clicks "Request ride" in browser.
2. Browser POSTs signals {pickupLat, pickupLng, dropoffLat, dropoffLng}
   to gateway POST /api/rides  (Datastar @post).
3. Gateway calls ride-service.RequestRide via gRPC.
4. ride-service:
     a. Calls location-service.GetNearbyDrivers(pickup, 3000 m, limit 5).
     b. For each candidate: calls driver-service.GetAvailability.
     c. Assigns first available driver; updates ride row to MATCHED.
     d. Publishes rides.<rideID>.matched on NATS.
5. Gateway receives the matched Ride and redirects browser to /rides/<id>
   via Datastar sse.Redirect.
6. Browser opens /rides/<id> which renders a Leaflet map and starts SSE
   stream GET /api/rides/<id>/stream.
7. Gateway:
     a. Fetches current ride state → patches #ride-status DOM element.
     b. Subscribes to rides.<rideID>.* and drivers.<driverID>.location on NATS.
     c. On each NATS message: re-fetches ride, patches DOM, pushes map commands.
8. payment-service NATS subscriber receives rides.<rideID>.completed and
   creates a payment record automatically.
9. notification-service NATS subscriber receives rides.> events and creates
   per-driver notifications fanned out via its in-memory hub.
```

---

## Technology choices (ADR summary)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| IPC | gRPC + protobuf | Typed contracts, generated clients, efficient binary framing |
| Broker | NATS | Lightweight Go-native broker; no external infra needed; JetStream-ready |
| Database | libSQL / SQLite | Zero-setup, file-per-service, Turso-compatible DSN for cloud migration |
| Frontend | Datastar v1.0.5 | SSE-native, hypermedia-first, no React/VDOM overhead |
| Geolocation | Haversine in Go | No Redis or PostGIS dependency; correct at < 10 km accuracy |
| Auth | JWT HS256 | Simple shared-secret approach appropriate for a learning project |
| Task runner | Taskfile v3 | Cleaner YAML than Make; cross-platform; no background: property used |
