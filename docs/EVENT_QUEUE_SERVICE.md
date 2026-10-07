# Piranid Event Queue Service

**Status:** implemented, tested. 36 tests, race-clean.
**Module:** `github.com/ayden-boyko/Piranid/nodes/Event_Queue`
**Transport:** HTTP REST, port `8082`

> This service was previously non-functional: its registry map was nil, its
> removal path self-deadlocked, queue creation always failed, and its manifest
> could not start the pod. All four were confirmed by running the code and have
> been fixed. [§9](#9-known-problems) records what was found and what was done
> about it, because the corrections explain most of the current design.

Related: [AUTH_SERVICE.md](AUTH_SERVICE.md) · [NOTIFICATIONS_SERVICE.md](NOTIFICATIONS_SERVICE.md) · [LOGGING_SERVICE.md](LOGGING_SERVICE.md) · [PLATFORM.md](PLATFORM.md)

---

## Table of contents

1. [What the service does](#1-what-the-service-does)
2. [Architecture](#2-architecture)
3. [Request lifecycle](#3-request-lifecycle)
4. [Routes](#4-routes)
5. [Files and descriptors](#5-files-and-descriptors)
6. [Data model](#6-data-model)
7. [Configuration](#7-configuration)
8. [Security posture](#8-security-posture)
9. [Verification performed](#9-verification-performed)
10. [Remaining work](#10-remaining-work)

---

## 1. What the service does

An HTTP API for administering **RabbitMQ queues on behalf of services**.

It is worth being precise about what this is, because the name oversells it:

> **This service neither publishes nor consumes messages.**

A repo-wide search for `Exchange`, `QueueBind`, `Publish`, and `Consume` across
`nodes/Event_Queue` returns zero hits. It only declares, inspects, and deletes
queues. Message production and consumption live elsewhere — Notifications
consumes (`notif_mq.go`), and nothing in this repository currently produces.

`ServiceConfig.json` describes a topic exchange (`event.exchange`) with routing
keys `auth.*`, `data.*`, `logging.*`. **No code reads that file, and no code
creates that topology.** It is a design sketch, not a description of the running
system.

| Capability                  | Status                                         |
| --------------------------- | ---------------------------------------------- |
| Declare queues per service  | Implemented                                    |
| List services and queues    | Implemented                                    |
| Drain-then-delete a queue   | Implemented, cannot complete (§9.5)            |
| Drain-then-delete a service | Implemented, cannot complete (§9.5)            |
| Publish messages            | Not implemented                                |
| Consume messages            | Not implemented                                |
| Exchanges and bindings      | Not implemented                                |
| Broker reconnection         | Implemented, with reconnect and registry reset |
| Tests                       | 36, in `models/` and `handlers/`               |

---

## 2. Architecture

```
   HTTP client (any Piranid service)
        │  Authorization: Bearer <JWT>
        ▼
┌────────────────────────────────────────┐
│  Event_Queue Node  (:8082)             │
│                                        │
│  ServeMux (method-scoped patterns)     │
│    └─ authn.Middleware ──┐             │
│                          │             │
│  handlers/routes.go      │             │
│    └─ drain loops ───────┤             │
│                          │             │
│  models.Services         │             │
│    map + RWMutex         │             │
│    └─ per-service *amqp.Channel       │
└──────────┬───────────────┼─────────────┘
           │               │
           │               └──── verifies JWT against Auth's JWKS
           ▼
   RabbitMQ (:5672)
   one *amqp.Connection, process lifetime
   no NotifyClose, no reconnect
```

**One AMQP connection** is dialled in `main.go:65` and threaded through
`RegisterRoutes` into every handler. **One channel per registered service** is
opened in `AddServiceHandler` (`routes.go:39`) and stored on the `Service`.

Both connection handles have no lifecycle management: no `NotifyClose`, no
heartbeat configuration, no reconnect loop.

---

## 3. Request lifecycle

### 3.1 Adding a service

```
Client                Authn middleware        Handler              models.Services        RabbitMQ
  │                        │                    │                        │                 │
  │ POST /services/svc-1   │                    │                        │                 │
  │ Authorization: Bearer  │                    │                        │                 │
  ├───────────────────────▶│                    │                        │                 │
  │                        │ verify sig/iss/aud/exp                     │                 │
  │                        │ 401 on failure    │                        │                 │
  │                        ├───────────────────▶│                        │                 │
  │                        │                    │ conn.Channel()        │                 │
  │                        │                    ├───────────────────────▶│                 │
  │                        │                    │ GetService → ErrNoRows │                 │
  │                        │                    │ AddService            │                 │
  │                        │                    ├───────────────────────▶│                 │
  │                        │                    │  stores *amqp.Channel │                 │
  │                        │                    │  (never closed)       │                 │
  │ 200, empty body        │                    │                        │                 │
  │◀───────────────────────┼────────────────────┤                        │                 │
  │                        │                    │ ⚠ returns are discarded, and the
  │                        │                    │   map is nil — panic.  §9.1, §9.20     │
```

Note the response: `AddServiceHandler` writes **no body and no status**, so a
client sees an implicit `200` with zero bytes.

### 3.2 Adding a queue — always fails

```
Client                Handler              models.Services
  │ POST /services/svc-1/queue/q1              │
  ├───────────────────────────────────────────▶│
  │                        GetService("svc-1") → ok
  │                        GetServiceQueue("svc-1","q1") → (nil, nil)   ← §9.6
  │                        handler reads err == nil as "already exists"
  │ 500 "Queue already exists"                 │
  │◀──────────────────────────────────────────┤
```

### 3.3 The drain-then-delete protocol

```
1. Mark the target draining        SetQueueDraining / SetServiceDraining
2. Poll until Messages == 0        PendingMessages, cancellable, deadline-bounded
3. Delete the queue                QueueDelete
4. Clear the draining marker       ClearQueueDraining / ClearServiceDraining
5. Remove from in-memory model     RemoveServiceQueue / RemoveService
```

Steps 4 and 5 are what make this work. The draining marker blocks removal _while
a drain is in flight_, which is its purpose — but the handler is the party that
set it, so it must clear it once the broker delete has succeeded. Without that
step the sequence deadlocks against itself; see [§9.5](#95-fixed--the-drain-protocol-could-never-complete).

Step 2 respects `r.Context()` and `DefaultDrainTimeout`, returning `504` with
`drain_timeout` rather than holding a goroutine and a channel open forever.

---

## 4. Routes

Registered in `eventcore/event_core.go:35-123`. `api_ver` defaults to `v1`.

| Method | Pattern                                           | Handler                 | Auth |
| ------ | ------------------------------------------------- | ----------------------- | ---- |
| any    | `/api/{v}/event_test`                             | `EventTestHandler`      | no   |
| GET    | `/api/{v}/services`                               | `GetAllServicesHandler` | yes  |
| POST   | `/api/{v}/services/{service_id}`                  | `AddServiceHandler`     | yes  |
| GET    | `/api/{v}/services/{service_id}`                  | `GetServiceHandler`     | yes  |
| DELETE | `/api/{v}/services/{service_id}`                  | `RemoveServiceHandler`  | yes  |
| POST   | `/api/{v}/services/{service_id}/queue/{queue_id}` | `AddQueueHandler`       | yes  |
| GET    | `/api/{v}/services/{service_id}/queue/{queue_id}` | `GetQueueHandler`       | yes  |
| DELETE | `/api/{v}/services/{service_id}/queue/{queue_id}` | `RemoveQueueHandler`    | yes  |
| GET    | `/healthz`                                        | inline status           | no   |

`{service_id}` and `{queue_id}` are read via `r.PathValue`. A body is optional
and the path always takes precedence.

`GET /api/{v}/services` is registered; it was previously defined but routed to
nothing.

`EventTestHandler` and `/healthz` are unauthenticated. Neither exposes data.

---

## 5. Files and descriptors

| File                        | Lines | Role                                                                                    |
| --------------------------- | ----: | --------------------------------------------------------------------------------------- |
| `main.go`                   |   122 | Entrypoint. Telemetry, logger, broker config, HTTP serve, signal drain. No `log.Panic`. |
| `eventcore/event_core.go`   |   396 | `EventNode`, route registration, `Broker` with reconnect, auth middleware, `/healthz`.  |
| `handlers/routes.go`        |   517 | All handler logic plus the drain-and-delete workflows.                                  |
| `models/services.go`        |   195 | The `Services` registry with a typed error contract and correct locking.                |
| `models/service.go`         |   176 | `Service`: AMQP channel, queue map, draining state, per-service locking.                |
| `models/service_queue.go`   |   110 | `ServiceQueue`, `QueueDeclaration`, `declareQueue`.                                     |
| `models/amqp_table.go`      |    79 | `map[string]string` → correctly typed `amqp.Table`.                                     |
| `transactions/*.go`         |  5–18 | DTOs plus `error_resp.go` with stable error codes.                                      |
| `models/models_test.go`     |     — | 21 registry tests, including the deadlock and nil-map regressions.                      |
| `handlers/handlers_test.go` |     — | 15 HTTP tests over the real handlers and mux.                                           |
| `EVENT.Dockerfile`          |    38 | Two-stage build → `event_server` on `alpine:3.20`.                                      |

### DTOs

```go
type AddServiceRequest struct {          // add_service_req.go
    ServiceId string   `json:"service_id"`
    Name      string   `json:"name"`      // accepted, then dropped
    Loggable  bool     `json:"loggable"`
    Tags      []string `json:"tags"`
}

type AddQueueRequest struct {            // add_queue_req.go
    ServiceId string   `json:"service_id"`
    QueueName string   `json:"queue_name"`
    Loggable  bool     `json:"loggable"`
    Tags      []string `json:"tags"`
    // pointers signal "optional" — absent means nil, so the caller can tell
    // "not supplied" from "explicitly false" and apply the AMQP default
    Durable    *bool             `json:"durable,omitempty"`
    AutoDelete *bool             `json:"auto_delete,omitempty"`
    Exclusive  *bool             `json:"exclusive,omitempty"`
    NoWait     *bool             `json:"no_wait,omitempty"`
    Args       map[string]string `json:"args,omitempty"`
}
```

`GetServiceResponse.Queues` has **no JSON tag**, so it serializes as
`"Queues"` rather than `"queues"` — inconsistent with every sibling field.

No DTO carries an HTTP status. Handlers signal failure by returning `error`, and
the route closures collapse every error to a flat `500` with the text
`"Internal Server Error"`. The specific reason is logged but never returned, so
there is no 404 and no 409 anywhere.

### `utils/map_to_table.go`

```go
func MapToTable(m map[string]string) amqp.Table {
	table := make(amqp.Table)
	for k, v := range m {
		table[k] = v
	}
	return table
}
```

Two problems. AMQP field tables are **typed**, so every value stays a Go
`string`; a numeric argument such as `x-message-ttl` arrives as a string and the
broker rejects it with a channel-level `PRECONDITION_FAILED`. And a `nil` map
returns a non-nil empty table, so `QueueDeclare` never receives `nil` args.

---

## 6. Data model

### `Service` — `models/service.go:9-14`

```go
type Service struct {
    ServiceId  string
    IsDraining bool
    Channel    *amqp.Channel
    Queues     map[string]*ServiceQueue
}
```

`SetServiceDraining` flips a bool. `SetQueueDraining` appends a `"draining"` tag
to the queue. `RemoveQueue` refuses to remove anything so tagged.

### `ServiceQueue` — `models/service_queue.go:7-13`

```go
type ServiceQueue struct {
    ServiceId string
    QueueName string
    Loggable  bool
    Tags      []string
    Queue     *amqp.Queue
}
```

`createServiceQueue` copies the tags slice defensively (lines 18-19).
`SearchTags` is a linear scan. `Loggable` is stored and returned but never
influences behaviour. `Queue` is retained and never read.

### `Services` — `models/services.go:13-16`

```go
type Services struct {
    services map[string]*Service
    mu       sync.RWMutex
}
```

The `RWMutex` guards **the top-level map only**. Per-service `Queues` maps are
mutated outside the lock, because `GetService` returns the live `*Service`
pointer and callers then read `service.Queues` directly (§9.12).

`NewServices()` exists and returns `*Services` with an initialised map — and has
**zero callers**. This is the direct cause of §9.1.

---

## 7. Configuration

| Variable               | Read at             | Default                | Manifest sets it   |
| ---------------------- | ------------------- | ---------------------- | ------------------ |
| `EVENT_QUEUE_PORT`     | `main.go:74`        | none                   | yes (`8082`)       |
| `RABBIT_MQ_PORT`       | `main.go:59`        | **none — `log.Panic`** | **NO** (§9.2)      |
| `API_VERSION`          | `event_core.go:27`  | `v1`                   | yes                |
| `OTEL_COLLECTOR_ADDR`  | `main.go:39`        | `localhost:4317`       | yes                |
| `AUTH_JWKS_URL`        | `event_core.go:139` | **required**           | yes                |
| `AUTH_ISSUER`          | `event_core.go:145` | **required**           | yes                |
| `AUTH_AUDIENCE`        | `event_core.go:150` | **required**           | yes                |
| `AUTH_LEEWAY`          | `event_core.go:186` | `30s`                  | yes                |
| `AUTH_ALLOW_ANONYMOUS` | `event_core.go:135` | unset                  | no                 |
| `REDIS_PORT`           | via `node.NewNode`  | —                      | no (client unused) |

RabbitMQ credentials and hostname are **hardcoded** (`main.go:65`):

```go
conn, err := amqp.Dial(fmt.Sprintf("amqp://guest:guest@rabbitmq:%s/", MQ_PORT))
```

`rabbitmq` matches the Service name in `manifests/rabbitmq-deployment.yaml`, so
this resolves inside the cluster. `guest:guest` is the broker's default
credential and is not configurable.

---

## 8. Security posture

This service **is** authenticated — a deliberate improvement over its prior
state, where it had no auth at all. The six service and queue endpoints are
wrapped in `authn.Middleware`, which verifies the RS256 signature against the
Auth node's JWKS and checks issuer, audience, expiry, and token type. It fails
closed: `buildAuthMiddleware` calls `logger.Fatal` if the JWKS URL, issuer, or
audience is unset, rather than starting open.

Unauthenticated routes: `event_test`, the empty `/api/{v}/services` no-op, and
`/healthz`. None exposes data.

Remaining gaps:

- `AUTH_ALLOW_ANONYMOUS=true` disables verification entirely. It logs a warning
  at startup and is unset in the manifest.
- No authorization model. Any valid token is accepted for every operation, so a
  client authorized to read a queue can also delete it. The `loggable` field on
  services and queues is stored but never enforced.
- `protect` rebuilds the middleware handler on every request rather than once at
  registration (`event_core.go:54-58`) — a per-request allocation on the hot
  path.
- `clockSkew()` silently swallows a malformed `AUTH_LEEWAY` and falls back to
  30s rather than failing.
- No TLS; plain HTTP.

---

## 9. Verification

**36 tests, race-clean**, in two suites.

| Suite                       | Tests | Covers                                                                                                                                                 |
| --------------------------- | ----: | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `models/models_test.go`     |    21 | Registry initialisation, the three sentinel errors, the deadlock and nil-map regressions, the drain sequence, snapshot independence, concurrent access |
| `handlers/handlers_test.go` |    15 | Routing, path-value extraction, status-code mapping, method enforcement, 404 vs 500, JSON field rejection                                              |

Every defect in §9 that had a runtime reproduction before the fix has a
regression test that would fail if it were reintroduced. `make check` and
`make test-race` are green.

The broker itself is not exercised: there is no RabbitMQ in CI, so the AMQP
paths (declaration, drain, delete) are covered at the registry layer and by the
structure of the handler, not against a live broker. That remains the largest
untested surface.

---

](#9-known-problems) records what was found and what was done

> about it, because the corrections explain most of the current design.

Related: [AUTH_SERVICE.md](AUTH_SERVICE.md) · [NOTIFICATIONS_SERVICE.md](NOTIFICATIONS_SERVICE.md) · [LOGGING_SERVICE.md](LOGGING_SERVICE.md) · [PLATFORM.md](PLATFORM.md)

---

## 10. Remaining work

Sequenced so each step is independently verifiable. Everything in §9 is fixed;
this is what is left.

| #   | Action                                                | Why                                                                                                                                                                                                         |
| --- | ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Exercise the AMQP paths against a real broker         | The largest untested surface: declaration, drain polling and delete are covered at the registry layer but not against RabbitMQ. A `testcontainers`-style or compose-backed integration test would close it. |
| 2   | Add a scope model and per-route requirements          | Any valid token can delete any queue. This is the same authorization gap the auth service has.                                                                                                              |
| 3   | Delete `ServiceConfig.json` and `database/Schema.sql` | Both unreferenced. `ServiceConfig.json` describes a topology no code creates, which actively misleads.                                                                                                      |
| 4   | Rename the `internal` package to `transactions`       | Every DTO file declares `package internal` and is aliased on import, so the directory name never appears.                                                                                                   |
| 5   | Enforce `loggable`, or drop the field                 | Stored, returned, and never read.                                                                                                                                                                           |
| 6   | Respect `AUTH_ALLOW_ANONYMOUS` only outside clusters  | Currently a plain env var. It logs a warning, which is the right shape; a deployment-time guard would be stronger.                                                                                          |
| 7   | Bound the drain deadline by configuration             | `DefaultDrainTimeout` is a constant. A busy cluster may need longer.                                                                                                                                        |

Items 3 and 4 are cleanup. Items 1 and 2 are the ones that matter.
