# API Reference

All endpoints are served by the **api-gateway** on `http://localhost:8080`.

Authentication is via an `HttpOnly` JWT cookie named `token` (set on login/register,
cleared on logout). Protected endpoints return `401` if the cookie is absent or expired.

Datastar endpoints return `text/event-stream` responses carrying
`datastar-patch-elements` and `datastar-patch-signals` events.
Plain endpoints (e.g. `POST /api/driver/location`) return `204 No Content`.

---

## Auth

### `POST /api/register`

Register a new account. Signals (JSON body):

```json
{ "email": "user@example.com", "password": "secret", "name": "Alice",
  "phone": "555-0100", "role": "passenger" }
```

`role` must be `"passenger"` or `"driver"`.

**Response:** SSE stream. On success sets `token` cookie and emits a redirect to
`/dashboard` (passenger) or `/driver` (driver). On failure patches `#form-error`.

---

### `POST /api/login`

Signals:
```json
{ "email": "user@example.com", "password": "secret" }
```

**Response:** SSE. On success sets cookie and redirects. On failure patches `#form-error`.

---

### `POST /api/logout`

Clears the `token` cookie and redirects to `/`.

---

## Rides

### `POST /api/rides`

Request a new ride. Signals:
```json
{ "pickupLat": 40.7128, "pickupLng": -74.006,
  "dropoffLat": 40.72,  "dropoffLng": -74.0 }
```

**Response:** SSE redirect to `/rides/<rideID>` on success, or patches `#form-error`.

---

### `GET /api/rides/{id}/stream`

Opens a persistent SSE stream for ride `{id}`. The gateway:
- Immediately patches `#ride-status` with the current state.
- Subscribes to `rides.<id>.*` and `drivers.<driverID>.location` on NATS.
- On each event: re-fetches ride, patches DOM, signals, and map coordinates.

**Events emitted (Datastar):**
- `PatchElements` → `#ride-status` div with status badge, driver info, fare.
- `ExecuteScript` → `window.placePickup(lat,lng)`, `window.placeDropoff(lat,lng)`,
  `window.moveDriverMarker(lat,lng)`.
- `MarshalAndPatchSignals` → `{status, driverId, fare}`.

---

### `POST /api/rides/{id}/start`

Advance ride to `ON_THE_WAY` (driver heading to pickup). Driver only.

### `POST /api/rides/{id}/pickup`

Advance ride to `ONGOING` (passenger picked up). Driver only.

### `POST /api/rides/{id}/complete`

Advance ride to `COMPLETED`. Triggers fare calculation and automatic payment charge.
Driver only.

---

## Driver

### `POST /api/driver/onboard`

Onboard vehicle details. Signals:
```json
{ "license": "DL123", "plate": "NY-100", "model": "Sedan" }
```

**Response:** SSE redirect to `/driver` on success.

---

### `POST /api/driver/availability`

Toggle online status. Signals:
```json
{ "online": true }
```

**Response:** SSE patches `#driver-status` and executes `window.startDrivingLoop()`
or `window.stopDrivingLoop()`.

---

### `POST /api/driver/location`

Report GPS fix (called by the simulated driving loop every 2 s).
Signals:
```json
{ "lat": 40.7128, "lng": -74.006 }
```

**Response:** `204 No Content`.

---

### `GET /api/driver/stream`

Persistent SSE stream for the driver dashboard. Pushes:
- `#current-ride` card with lifecycle controls.
- `window.onRideMatched(id, plat, plng, dlat, dlng)` when matched.
- `window.onRideStatus(status)` on each transition.

---

## Payments

### `GET /api/payments`

Query params: `?role=passenger|driver` (defaults to the authenticated user's role).

**Response:** SSE patches `#payment-list` with an HTML table of payments and
the total amount.

---

### `GET /api/payments/{rideId}`

**Response:** SSE patches `#payment-detail` with a single payment card.

---

## Notifications

### `GET /api/notifications/stream`

Persistent SSE stream. Pushes:
- `#notif-badge` span with unread count.
- `.toast` elements (self-removing after 5 s via CSS animation) for each
  unread notification.

Polling interval: 5 seconds. Future: upgrade to gRPC server-streaming.

---

### `POST /api/notifications/read`

Marks all notifications for the authenticated user as read.
Patches `#notif-badge` to hidden.

---

## gRPC Services (internal)

Internal gRPC endpoints are not exposed externally but are described in full in the
protobuf definitions under `proto/`:

| Proto file | Service | Port |
|------------|---------|------|
| `user/v1/user.proto` | `UserService` | `:50051` |
| `driver/v1/driver.proto` | `DriverService` | `:50052` |
| `location/v1/location.proto` | `LocationService` | `:50053` |
| `ride/v1/ride.proto` | `RideService` | `:50054` |
| `payment/v1/payment.proto` | `PaymentService` | `:50055` |
| `notification/v1/notification.proto` | `NotificationService` | `:50056` |

Regenerate Go bindings: `task proto:generate`

---

## NATS Subjects

| Subject pattern | Publisher | Subscribers |
|-----------------|-----------|-------------|
| `rides.<id>.matched` | ride-service | api-gateway (ride stream), notification-service |
| `rides.<id>.started` | ride-service | api-gateway, notification-service |
| `rides.<id>.ongoing` | ride-service | api-gateway, notification-service |
| `rides.<id>.completed` | ride-service | api-gateway, payment-service, notification-service |
| `rides.<id>.cancelled` | ride-service | api-gateway, notification-service |
| `drivers.<id>.location` | location-service | api-gateway (ride stream) |
