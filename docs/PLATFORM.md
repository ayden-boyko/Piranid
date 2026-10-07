# Piranid Platform

**Scope:** the parts of Piranid that are not a single service — the shared
libraries, the build system, the deployment surface, and the cross-service gaps
that no individual service document can describe.

Service documents: [AUTH_SERVICE.md](AUTH_SERVICE.md) ·
[EVENT_QUEUE_SERVICE.md](EVENT_QUEUE_SERVICE.md) ·
[NOTIFICATIONS_SERVICE.md](NOTIFICATIONS_SERVICE.md) ·
[LOGGING_SERVICE.md](LOGGING_SERVICE.md)

---

## Table of contents

1. [Repository layout](#1-repository-layout)
2. [Go workspace](#2-go-workspace)
3. [Shared packages](#3-shared-packages)
4. [Build and verification](#4-build-and-verification)
5. [Deployment surface](#5-deployment-surface)
6. [Cross-service status](#6-cross-service-status)
7. [Documentation drift](#7-documentation-drift)
8. [Remaining work](#8-remaining-work)

---

## 1. Repository layout

```
Piranid/
├── go.work                 Go workspace, 5 modules
├── go.work.sum             checksums
├── Makefile                build / vet / test / fmt / keys / tidy / clean
├── README.md               project overview — partly stale, see §7
├── docker-compose.yml      broken, see §5
│
├── nodes/
│   ├── Auth/               OAuth 2.0 authorization server   (:8081)
│   ├── Event_Queue/        RabbitMQ queue administration     (:8082)
│   ├── Notifications/      gRPC notification delivery        (:8084)
│   ├── Logging/            observability stack config only (compose)
│   └── Iot/                4-line stub
│
├── pkg/                    shared library, module Piranid/pkg
│   ├── authn/              token format: RS256, JWKS, verifier, middleware
│   ├── DataManager/        generic parameterized persistence
│   ├── dbutil/             SQLite open with WAL + BEGIN IMMEDIATE
│   ├── models/             Entry base type
│   ├── node/               shared HTTP node harness, module Piranid/node
│   ├── telemetry/          OpenTelemetry SDK + zap logger
│   └── proto/              gRPC contracts (event_queue, logging, notifications)
│
├── manifests/              Kubernetes: namespace, auth, event, notif, logging, rabbitmq
├── scripts/                deploy.sh (empty), join-cluster.sh (empty), fix-modules.sh
├── docs/                   this documentation
└── tests/                  scaffolding only — placeholder point.txt files
```

### Node status

| Node | Transport | Auth | Tests | Image |
|---|---|---|---:|---|
| **Auth** | HTTP REST | issues tokens | 20 | 64MB |
| **Event_Queue** | HTTP REST | verifies tokens | 36 | 41MB |
| **Notifications** | gRPC + MQ | verifies tokens | 39 | 44MB |
| **Logging** | config | n/a | validated in CI | — |
| **Iot** | — | — | 0 | stub |

All three services build, vet clean, and pass their tests under the race
detector. `nodes/Iot/main.go` is four lines and prints a message.

Event_Queue and Notifications were both non-functional and unauthenticated before
this work; see their documents for what was wrong and what changed.

---

## 2. Go workspace

```go
// go.work
go 1.26.1

use (
    ./nodes/Auth
    ./nodes/Event_Queue
    ./nodes/Notifications
    ./pkg
    ./pkg/node
)
```

Five modules. `pkg` is imported by all three services via
`replace Piranid/pkg => ../../pkg`, and `pkg/node` by `pkg` itself.

The workspace is **load-bearing, not optional**. `GOWORK=off` does not work:
`nodes/Auth/main.go` imports `Piranid/node`, which `nodes/Auth/go.mod` does not
require, and `pkg/utils.go` imports `Piranid/node` without requiring it either.
Both resolve only through the workspace.

### Version skew

| Declaration | Version |
|---|---|
| `go.work` | `go 1.26.1` |
| `pkg/go.mod` | `go 1.25.0` |
| `pkg/node/go.mod` | `go 1.24.0` |
| `nodes/*/go.mod` | `go 1.25.0` |
| Dockerfiles | `golang:1.26-alpine` |

Four different Go versions across seven files. Not currently breaking, but it
means the effective language version for a module is the workspace's, and a
newer feature in a module's own `go.mod` would be silently unavailable.

### `go.work.sum` was committed with conflict markers

Six merge-conflict blocks (lines 44–47, 112–115, 182–185, 198–201, 236–240,
246–249) from commit `13a0c73`. Every `go` command failed:

```
malformed go.sum: go.work.sum:46: wrong number of fields 1
```

All six had an empty "stashed" side, so the resolution was mechanical. Resolved
as part of the Auth work. **This is worth a CI check** — a conflict marker in a
sum file is invisible in review and fatal to the build.

---

## 3. Shared packages

### `pkg/authn` — token format

The security boundary for the whole platform. Documented in full in
[AUTH_SERVICE.md §6](AUTH_SERVICE.md#6-files-and-descriptors).

| File | Lines | Role |
|---|---:|---|
| `keys.go` | 209 | Key loading, generation, PEM encoding, RFC 7638 `kid` |
| `jwks.go` | 137 | `JWK`/`JWKS` types, emit, parse, lookup |
| `claims.go` | 183 | `Claims`, claim verification, typed errors |
| `signer.go` | 123 | RS256 issuance |
| `verifier.go` | 255 | Verification, static and rotating key sources |
| `middleware.go` | 270 | Bearer middleware, JWKS fetcher, `AllowAll` |
| `config.go` | 121 | Env config with validation |
| `random.go` | 43 | `RandomToken`, `NewJTI`, `ClockSkew` |
| `authn_test.go` | 501 | 16 tests |
| `middleware_test.go` | 320 | 12 tests |

**Consumers:** Auth (signs, publishes JWKS) and Event_Queue (verifies).
Notifications does not import it.

**Dependency note.** Built on `golang-jwt/jwt/v5` plus stdlib `crypto/rsa` and
`encoding/base64` — JWK encoding is hand-rolled rather than using a JOSE library.
That is ~40 lines and is tested, but a dedicated JOSE library is the safer
long-term choice.

### `pkg/DataManager` — generic persistence

| Function | Purpose |
|---|---|
| `GetEntry(columns, key, id, scanner)` | Parameterized `SELECT` with an explicit column list |
| `ConsumeEntry(columns, key, id, scanner, deleter)` | Atomic read-and-delete in one transaction |
| `PushData` / `UpdateData` / `DeleteData` | Transaction-scoped writes |

Identifiers are interpolated into SQL text (they cannot be bound), so they are
validated against `^[A-Za-z_][A-Za-z0-9_]*$` before use; values are always bound.

**Consumers:** Auth only. Notifications has hand-written SQL helpers with the
bugs documented in [NOTIFICATIONS_SERVICE.md §9.5](NOTIFICATIONS_SERVICE.md#9-known-problems),
and Event_Queue holds its state in memory with no database at all.

### `pkg/dbutil` — SQLite setup

```go
func OpenSQLite(path string) (*sql.DB, error)
func ApplySchema(db *sql.DB, script string) error
```

Applies `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`,
`synchronous=NORMAL`, and `_txlock=immediate`, with a pool capped at 8
connections.

`_txlock=immediate` is the important one: SQLite does not invoke its busy handler
for a deferred transaction's read-lock→write-lock upgrade, so concurrent writes
fail with `SQLITE_BUSY` regardless of `busy_timeout`. `BEGIN IMMEDIATE` acquires
the write lock up front, where the handler applies. Found by a concurrency test,
documented in [AUTH_SERVICE.md](AUTH_SERVICE.md#sqlite-configuration).

**Consumers:** Auth. Notifications still opens SQLite the old way.

### `pkg/node` — HTTP node harness

| Element | Purpose |
|---|---|
| `Node` struct | `*http.Server`, `*http.ServeMux`, `DB interface{}`, unexported Redis `*redis.Client` |
| `NewNode()` | Builds an empty server, mux, and a Redis client pointed at `localhost:$REDIS_PORT` |
| `Run(port, registerRoutes)` | Binds, invokes the route callback, serves |
| `SafeShutdown(ctx)` | `Server.Shutdown` then `ShutdownDB()` |
| `ShutdownDB()` | Returns `nil` — a no-op |
| `GetDB` / `SetDB` / `GetCache` / `SetCache` | Accessors |

Three problems worth knowing about:

1. **`DB` is `interface{}`.** Services type-assert it to `*sql.DB` (Auth,
   Event_Queue) or `*sql.Tx` (Notifications — which is why that service's writes
   all fail). A typed field would have caught it at compile time.
2. **`ShutdownDB()` is a no-op.** It returns `nil` without closing anything. Auth
   and Event_Queue override it; Notifications inherits the no-op, so its
   `SafeShutdown` never closes the database.
3. **`NewNode()` always creates a Redis client** that no service uses and no code
   closes. `GetCache`/`SetCache` have no callers outside `pkg/node` itself.

The `HTTP` framing also does not fit Notifications, which is gRPC — so it embeds
a node it never uses and calls `Server.Shutdown` on a server it never started.

### `pkg/telemetry` — observability

`SetupOTelSDK(ctx, serviceName, collectorAddr)` and
`NewLogger(serviceName)` / `WithTraceID(ctx, logger)`.

Provides OTLP/gRPC traces, metrics and logs over an insecure connection, W3C
propagation, and a zap logger with a static `service` field.

**Every service calls it, and every service gets observability wrong in a
different way.** See [LOGGING_SERVICE.md §6](LOGGING_SERVICE.md#6-instrumentation-in-the-services):

- Event_Queue reports `service.name = "Auth Node"` and `service = notifications`.
- Notifications logs each error twice and calls `WithTraceID` on only some paths.
- Auth does not call `WithTraceID` at all, so its log lines have no trace ID.
- No service has server spans — no `otelhttp` wrapper, no gRPC interceptor.

### `pkg/proto` — gRPC contracts

| Directory | Service | Status |
|---|---|---|
| `notifications/v1` | `notifications.v1.Notifier` | 4 RPCs, all non-functional |
| `event_queue/v1` | — | `event_queue.proto` only, **no generated Go** |
| `logging/v1` | — | `logging.proto` only, **no generated Go** |

Only `notifications` has generated code (`*.pb.go`, `*_grpc.pb.go`). The other
two protos have no Go bindings, so there is no gRPC client or server for them
anywhere.

There is **no `pkg/proto/auth*`** — ever. The auth service's original
`nodes/Auth/auth.proto` (added `a502e7b`, deleted `bca29f6`) was a gRPC
`Authorizer`; it was removed in favour of the HTTP OAuth implementation that now
exists in `nodes/Auth`.

`pkg/Makefile`'s defaults are stale: `PROTO_FILE ?= notifications.proto` with
`-I=proto` expects `pkg/proto/notifications.proto`, which does not exist. The
working invocation survives only in a trailing comment.

---

## 4. Build and verification

```bash
make help          # list targets
make build         # build all 5 modules
make vet           # go vet all modules
make fmt           # format
make fmt-check     # fail if anything is unformatted
make test          # run all tests
make test-race     # run all tests under -race
make check         # fmt-check + vet + test
make keys          # generate an RSA signing pair into jwt-keys/
make tidy          # tidy every go.mod
make clean         # remove artifacts and test cache
```

`MODULES := nodes/Auth nodes/Event_Queue nodes/Notifications pkg`

Current state: **`make check-all` green. `make test-race` green. 135 tests.**

| Suite | Tests | Coverage |
|---|---:|---|
| `pkg/authn` | 28 | Signing, verification, every rejection path, JWKS, rotation, middleware |
| `pkg/DataManager` | 8 | Parameterization, injection rejection, atomic consume under race |
| `pkg/dbutil` | 4 | Pragmas applied, `BEGIN IMMEDIATE`, concurrent writers, schema |
| `nodes/Auth/tests` | 20 | Full OAuth flow end to end, including concurrency |
| `nodes/Event_Queue/models` | 21 | Registry, sentinel errors, deadlock and nil-map regressions, snapshots |
| `nodes/Event_Queue/handlers` | 15 | Routing, path values, status mapping, 404 vs 500 |
| `nodes/Notifications/models` | 21 | Validation, method casing, importance bounds, marshalling |
| `nodes/Notifications/handlers` | 18 | RPC validation, persistence round trips, auth interceptor |

### CI

`.github/workflows/ci.yml`, five jobs:

| Job | Guards against |
|---|---|
| `check` | Unformatted code, build breaks, vet findings, test failures |
| `race` | Data races |
| `manifests` | YAML that does not parse, undefined ConfigMap/PVC references, volume mounts with no volume, duplicate env keys |
| `no-conflict-markers` | The `go.work.sum` failure that made every `go` command fail with no visible cause |
| `docker` | A Dockerfile that stops building |

The `no-conflict-markers` job exists because `go.work.sum` once shipped six
committed merge-conflict blocks. Nothing in the diff explained why, and nothing
was running tests.

### Additional Make targets

| Target | Purpose |
|---|---|
| `make check-all` | `check` plus manifest validation |
| `make validate-manifests` | Parse `manifests/*.yaml`, check ConfigMap/PVC references, volume mounts, duplicate env keys |
| `make images` | Build all three service images |

### Test harness conventions

- Auth's `tests/oauth_flow_test.go` runs against a **real** SQLite file, a **real**
  2048-bit RSA key and a **real** `httptest` server. Nothing is mocked, because
  the bugs it guards were in the wiring between storage, PKCE, signing and
  redirects.
- HTTP clients in that suite use
  `CheckRedirect: func(...) error { return http.ErrUseLastResponse }`, because
  the assertions are on the 302 itself.
- Time-dependent code routes through an overridable `nowUnix` var rather than
  mocking a clock library.

### Line endings

24 files across the repo had CRLF endings, which made `gofmt -l` permanently
non-empty and the format gate meaningless. All were normalized to LF — a no-op
semantically. If a file reappears as modified with no content diff, check this
first.

---

## 5. Deployment surface

### Kubernetes manifests

| File | Target | Notes |
|---|---|---|
| `namespace.yaml` | namespace `piranid` | |
| `auth-deployment.yaml` | Auth | Key Secret mount, token config, health probes |
| `event-deployment.yaml` | Event_Queue | Auth config, broker config, health probe |
| `notif-deployment.yaml` | Notifications | Auth config, broker config, gRPC probe, data volume |
| `logging-config.yaml` | 6 ConfigMaps | Collector, Loki, Tempo, Prometheus, Promtail, Grafana |
| `logging-storage.yaml` | 4 PVCs | Loki, Tempo, Prometheus, Grafana |
| `logging-deployment.yaml` | 6 Deployments + 6 Services | Replaces a single unschedulable container |
| `rabbitmq-deployment.yaml` | RabbitMQ | `rabbitmq:management`, no PVC, default credentials |

All three application manifests can now start their pods. `RABBIT_MQ_PORT` and
`RABBIT_MQ_QUEUE_NAME` were both missing; either one crash-looped the pod before
it reached a handler. `scripts/validate-manifests.py` runs in CI to catch
undefined references and duplicated env keys.

Every service uses `imagePullPolicy: Never` with `docker.io/library/piranid-*`
tags, so images must be built and loaded onto each node manually.

### Docker

| File | Status | Image |
|---|---|---|
| `nodes/Auth/AUTH.Dockerfile` | Works. `alpine:3.20`, static, keys not baked in. | 64MB |
| `nodes/Event_Queue/EVENT.Dockerfile` | Was `golang:1.26-alpine`; now `alpine:3.20`, static. | 41MB |
| `nodes/Notifications/NOTIF.Dockerfile` | Same. | 44MB |
| `nodes/Logging/docker-compose.yaml` | Internally consistent, with `mem_limit` on every service. | — |
| `nodes/Event_Queue/docker-compose.yml` | References `dockerfile: Dockerfile`; the file is `EVENT.Dockerfile`. | — |
| root `docker-compose.yml` | **Broken** — see below. Candidate for deletion. |

The runtime images previously carried a full Go toolchain, which is 269MB on its
own and did not fit the 128Mi memory limits. All three now build with
`CGO_ENABLED=0 -trimpath` onto `alpine:3.20`; `make images` builds them and CI
verifies they build at all.

All three service Dockerfiles require the **repo root** as build context
(`COPY . .` then `cd nodes/<name>`), because the modules depend on `pkg` through
the workspace.

A `.dockerignore` now excludes `.git`, `*.pem`, `*.key`, `jwt-keys/`, `*.db`,
docs and editor directories. This matters more than a usual context-size saving:
without it, `COPY . .` pulls signing keys into the build context and potentially
into an image layer, where they would be readable by anyone who can pull the
image and could not be rotated without a rebuild.

Auth's Dockerfile is the reference: minimal runtime image, static build, signing
keys mounted at runtime rather than baked in — an image layer is readable by
anyone who can pull it, and a key in an image cannot be rotated without a rebuild.

### Root `docker-compose.yml` does not work

```yaml
build:
  context: ../Auth-Service          # ← does not exist
  dockerfile: ../nodes/Auth/AUTH.Dockerfile
ports:
  - "8000:8000"                     # ← the app listens on 8081
environment:
  - TODO                            # ← the literal string "TODO"
```

All five build contexts are missing directories: `Auth-Service`,
`Event-Queue-Service`, `Notification-Service`, `Log-Service`, `Database-Service`.
Ports are off by 80–84. `environment: - TODO` sets nothing. No RabbitMQ service is
defined, though two services `depends_on: Event-Queue-Service`.

Compose also rejects contexts outside the compose file's own directory, so this
cannot work as written regardless.

### Scripts

| File | Lines | Status |
|---|---:|---|
| `scripts/deploy.sh` | 0 | Empty |
| `scripts/join-cluster.sh` | 0 | Empty |
| `scripts/fix-modules.sh` | 81 | Rewrites `go.mod` replace directives |

`fix-modules.sh` is the only substantive script. `deploy.sh` and
`join-cluster.sh` are zero-byte placeholders — deployment is manual.

---

## 6. Cross-service status

Which services can actually talk to each other today.

```
Auth  ──────────────► Event_Queue        ✅ works
Auth  ──────────────► Notifications      ✅ works (bearer interceptor)
                      Event_Queue ─────► Notifications   ⚠️  MQ path untested
Notifications ───────► anyone            ⚠️  no in-repo client exists
```

| Integration | Mechanism | Status |
|---|---|---|
| Auth → Event_Queue | JWT over HTTP, JWKS verification | **Working.** Event_Queue fails closed if unconfigured. |
| Auth → Notifications | JWT in gRPC metadata, JWKS verification | **Working.** Fails closed if unconfigured. |
| Event_Queue → Notifications | RabbitMQ | Both ends now correct; **no integration test**, and no producer in-repo. |
| Anyone → Auth | OAuth flows | **Working**, 20 tests. |

### The authentication gap — closed

Auth was built so services could verify its tokens. Event_Queue was wired first;
Notifications now follows, via `grpc.UnaryInterceptor` reading the bearer token
from request metadata. This required a gRPC interceptor rather than the HTTP
middleware used elsewhere, because the service has no HTTP surface at all.

`DeleteUser` is no longer an unauthenticated destructive operation, and the
interceptor rejects rather than admits when verification is unconfigured.

### Storage — now consistent

| Service | Storage | Opens via |
|---|---|---|
| Auth | SQLite | `dbutil.OpenSQLite` |
| Event_Queue | none — in-memory registry | n/a |
| Notifications | SQLite | `dbutil.OpenSQLite` + `DataManager` |

Both SQL-backed services now open through `dbutil.OpenSQLite` (WAL,
`busy_timeout`, `BEGIN IMMEDIATE`) and use `pkg/DataManager` with explicit column
lists. The hand-written SQL in Notifications that filtered on a
non-existent column, embedded a tab in `DELETE`, and bound a map to a parameter
is gone.

### Observability — now consistent

| Service | OTel `service.name` | Log `service` field | Server spans | `WithTraceID` |
|---|---|---|---|---|
| Auth | `Auth Node` | `auth` | yes | yes |
| Event_Queue | `event_queue` | `event_queue` | yes | via context |
| Notifications | `notifications` | `notifications` | yes | yes |

All three report a correct identity and chain traces across service boundaries.
Detail in [LOGGING_SERVICE.md §6](LOGGING_SERVICE.md#6-instrumentation-in-the-services).

---

## 7. Documentation drift

`README.md` described a repository that did not exist: `controllers/`,
`shared/` and `test/` were all absent, `deploy.sh` and `join-cluster.sh` were
zero bytes, and the technology list named InfluxDB and ECharts, neither of which
appears anywhere in the tree.

**Fixed.** `README.md` now describes the actual layout, links every service
document, carries a worked quick start, and states what CI runs. The stale
`controllers/`, `shared/` and `test/` entries are gone, and InfluxDB and ECharts
are replaced with what is actually deployed.

Remaining drift:

- `docs/*.pdf` — five design documents whose contents predate the current code.
  The Auth PDF describes a cache-first credential lookup that was never
  implemented; the Event Queue PDF describes a topology still not created.
- `docs/observability-architecture.md` — its memory budget and component list
  predate the current manifests.
- `docker-compose.yml` at the repository root is still broken; see
  [§5](#5-deployment-surface). Compose is superseded by the K3s manifests and by
  `nodes/Logging/docker-compose.yaml`, which does work. The obvious action is to
  delete it rather than maintain a third deployment story.

---

## 8. Remaining work

| # | Item | Services | Why |
|---|---|---|---|
| 1 | Integration-test Event_Queue against a real broker | Event_Queue | The AMQP paths are covered at the registry layer but not against RabbitMQ. Largest untested surface. |
| 2 | Integration-test the Notifications consumer | Notifications | §5.2 was the highest-impact defect and is covered structurally, not by execution. |
| 3 | Wrap the Courier client behind an interface | Notifications | `HandleNotifSend` is the one path no test reaches. |
| 4 | Introduce a producer for the notification queue | Event_Queue → Notifications | Event_Queue still declares no exchanges or bindings; nothing publishes. |
| 5 | Authorize handlers on the authenticated `client_id` | Notifications | `CallerServiceID` exists and is tested, but handlers still read `service_id` from the request body, which is caller-controlled. |
| 6 | Add a scope model and per-route requirements | All consumers | Any valid token can perform any operation. The middleware supports scopes; nothing declares them. |
| 7 | Provision Grafana dashboards and alerts | Logging | Datasources exist, so the cluster gets storage without a UI or an alarm. |
| 8 | Move observability storage off SD cards | Logging | Metrics storage on an SD card is a wear and failure risk. |
| 9 | TLS for the OTLP path and for service traffic | All | `insecure: true` is fine on a trusted cluster network, not a routed one. |
| 10 | Implement TFA in the auth service | Auth, Notifications | `RequestTFA` now correctly returns `Unimplemented`. See [NOTIFICATIONS_SERVICE.md §11](NOTIFICATIONS_SERVICE.md#11-two-factor-authentication). |
| 11 | Delete `Event_Queue/ServiceConfig.json` and `Schema.sql` | Event_Queue | Both unreferenced; the first describes a topology no code creates. |
| 12 | Reconcile the `docs/*.pdf` design set | Docs | They describe the old cache-first and broken-schema designs. |
| 13 | Add the IoT node | Iot | Four-line stub. |
| 14 | Revisit `pkg/node` | Platform | `DB` is `interface{}`, which is how Notifications' `*sql.Tx` assertion went unnoticed, and `ShutdownDB` is a no-op that Notifications inherited. |

Items 1–3 are about confidence; items 5–6 are about authorization; the rest are
capability or hygiene.
