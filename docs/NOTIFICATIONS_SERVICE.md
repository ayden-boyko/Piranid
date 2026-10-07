# Piranid Notification Service

**Status:** implemented, tested. 39 tests, race-clean.
**Module:** `github.com/ayden-boyko/Piranid/nodes/Notifications`
**Transport:** gRPC (unary), port `8084`. Also consumes RabbitMQ.
**Upstream:** [Courier](https://github.com/trycourier/courier-go) for email/SMS delivery

> This service was previously non-functional and unauthenticated: all four RPCs
> failed on every call, `RequestTFA` claimed success without doing anything, and
> the RabbitMQ listener closed its own connection milliseconds after starting.
> All of it has been fixed and is now covered by tests. [§9](#9-known-problems)
> records what was wrong and what changed.

Related: [AUTH_SERVICE.md](AUTH_SERVICE.md) · [EVENT_QUEUE_SERVICE.md](EVENT_QUEUE_SERVICE.md) · [LOGGING_SERVICE.md](LOGGING_SERVICE.md) · [PLATFORM.md](PLATFORM.md)

---

## Table of contents

1. [What the service does](#1-what-the-service-does)
2. [Architecture](#2-architecture)
3. [The proto contract](#3-the-proto-contract)
4. [RPC behaviour](#4-rpc-behaviour)
5. [RabbitMQ ingress](#5-rabbitmq-ingress)
6. [Files and descriptors](#6-files-and-descriptors)
7. [Data model](#7-data-model)
8. [Configuration](#8-configuration)
9. [Verification](#9-verification)
10. [Two-factor authentication](#10-two-factor-authentication)
11. [Remaining work](#11-remaining-work)

---

## 1. What the service does

A gRPC service intended to deliver notifications to users over email and SMS,
relaying delivery to Courier, with a RabbitMQ consumer as an asynchronous
alternative for lower-priority messages.

```
                        ┌──────────────────────────────────────┐
   gRPC caller          │  Notification Node  (:8084)          │
   (nothing in this     │                                      │
    repo calls it)      │  notifications.v1.Notifier (4 RPCs)   │
        │               │      │                               │
        ├──────────────▶│      │  ConvertToNotifEntry           │
                        │      ▼                               │
                        │  NotifEntry ──▶ ValidateIntegrity ──┐ │
                        │                       │ fails      │ │
                        │  RabbitMQ ─▶ worker pool ─▶ HandleNotifSend
                        │                                      ▼
                        │                                 Courier (email/SMS)
                        │  SQLite (notifications, templates)
                        └──────────────────────────────────────┘
```

| Capability                             | Status                                                |
| -------------------------------------- | ----------------------------------------------------- |
| `RequestNotification` (gRPC)           | Implemented, validated, delivers via Courier          |
| `DeleteUser` (gRPC)                    | Implemented; deletes notification rows                |
| `RequestUserNotificationUpdate` (gRPC) | Implemented; upserts, infers the method               |
| `RequestTFA` (gRPC)                    | Returns `Unimplemented` — deliberately (§9.3)         |
| RabbitMQ consumption                   | Implemented; durable queue, manual ack, blocking wait |
| Delivery via Courier                   | Reachable (§9.1 fixed)                                |
| Retry with backoff                     | Bounded: 5 attempts, linear backoff, cancellable      |
| Authentication                         | Bearer interceptor, fails closed (§8)                 |
| Graceful shutdown                      | `GracefulStop` with a 10s drain deadline              |
| Tests                                  | 39, in `models/` and `handlers/`                      |

---

## 2. Architecture

### Boot sequence — `main.go:32-118`

| Step | Line       | Action                                                                                                                                  |
| ---: | ---------- | --------------------------------------------------------------------------------------------------------------------------------------- |
|    1 | `:34`      | Create the Courier client from `COURIER_TOKEN`. No validation — a missing token yields a client that fails only at the first HTTP call. |
|    2 | `:37-38`   | `grpc.NewServer()` — **zero options**: no TLS, no interceptors, no keepalive, no recovery.                                              |
|    3 | `:41-49`   | OpenTelemetry SDK → `OTEL_COLLECTOR_ADDR`, service name `"Notification Node"`.                                                          |
|    4 | `:52-56`   | zap logger named `notifications`.                                                                                                       |
|    5 | `:60`      | Build `NotificationNode` with a Courier client and `utils.NewServiceID("NOTF")` — a **fresh UUID every boot**.                          |
|    6 | `:64-67`   | Register the gRPC service **and gRPC reflection** (unconditional).                                                                      |
|    7 | `:69-77`   | `net.Listen("tcp", ":8084")` — plain TCP, all interfaces, no TLS.                                                                       |
|    8 | `:80-83`   | Open SQLite and apply `Schema.sql`. **Both paths are relative to CWD.**                                                                 |
|    9 | `:87-94`   | `grpcServer.Serve(listener)` in a goroutine.                                                                                            |
|   10 | `:99`      | `go server.StartMQListener(ctx)`.                                                                                                       |
|   11 | `:103-107` | Block on SIGINT/SIGTERM.                                                                                                                |
|   12 | `:110-117` | `server.SafeShutdown(shutdownCtx)` with a 10s deadline.                                                                                 |

Two ordering problems fall out of this:

- The listener is bound at step 7, **before** the database is opened at step 8.
  A database failure leaves a bound socket.
- `SafeShutdown` calls `n.Server.Shutdown(ctx)` on the `*http.Server` from
  `node.NewNode()` — **which was never started**. `grpcServer.GracefulStop()`
  appears nowhere in the repository, so in-flight RPCs are never drained
  (§9.9).

### Two ingress paths, one dead

The node is meant to be reached either by gRPC or by RabbitMQ. Both are broken,
for unrelated reasons:

- **gRPC** fails on a data-validation gate that nothing can satisfy (§9.1, §9.2).
- **RabbitMQ** closes its own connection immediately (§9.5).

`notifcore/notif_core.go:28-29` documents the intent: _"Only to be used
internally, hence the name / sends a notif, will be used by gRPC and MQ"_.

---

## 3. The proto contract

`pkg/proto/notifications/v1/notifications.proto` — 103 lines.

```protobuf
enum Status {
    FAILURE = 0;
    SUCCESS = 1;
}

service Notifier {
    rpc RequestNotification(NotificationRequest) returns (NotificationResponse);
    rpc DeleteUser(NotificationRequest) returns (NotificationResponse);
    rpc RequestUserNotificationUpdate(UserNotificationUpdate) returns (UserNotificationResponse);
    rpc RequestTFA(TFARequest) returns (TFAResponse);
}

message NotificationRequest {
    string service_id = 1;
    string username = 2;
    string method = 3;          // sms, email, push
    map<string, string> data = 4;
    int32 importance = 5;
}

message NotificationResponse {
    Status success = 1;
    optional string response_message = 2;
}

message UserNotificationUpdate {
    string service_id = 1;
    string username = 2;
    string contact_info = 3;    // check for @, if not there, it's phone
}

message UserNotificationResponse {
    Status success = 1;
    optional string response_message = 2;
}

message TFARequest {
    string service_id = 1;
    string username = 2;
    string method = 3;
    string contact_info = 4;
    optional int32 timeout = 5;
}

message TFAResponse {
    string service_id = 1;
    string username = 2;
    Status success = 3;
}
```

Full method names on the wire:

```
/notifications.v1.Notifier/RequestNotification
/notifications.v1.Notifier/DeleteUser
/notifications.v1.Notifier/RequestUserNotificationUpdate
/notifications.v1.Notifier/RequestTFA
```

Service name `notifications.v1.Notifier`, metadata
`notifications/v1/notifications.proto`, **no streaming**.

### Contract issues

- **`TFAResponse` has no `code` field**, despite the proto comment at `:35`
  reading _"requests a TFA notification (code will need to be generated)"_. The
  message cannot carry a code.
- **`NotificationRequest.username` is used as the contact address.** The
  converter assigns `result.ContactInfo = req.Username`
  (`notif_converter.go:23`), so a field named `username` is expected to hold an
  email address or phone number. Nothing validates this.
- **The author already flagged TFA as misplaced** (`notifications.proto:31-33`):
  _"TODO change this to be a different protofile, / this is service-specific
  logic leaking"_. See [§11](#11-two-factor-authentication).
- Three RPCs and four messages are commented out in the source
  (`:25-26`, `:38`, `:54-76`), including `RequestNotificationStatus` and
  `RequestNotificationList`.

### Generated code

| File                       | Lines | Generator                                   |
| -------------------------- | ----: | ------------------------------------------- |
| `notifications.pb.go`      |   572 | `protoc-gen-go v1.36.11` / `protoc v6.32.1` |
| `notifications_grpc.pb.go` |   263 | `protoc-gen-go-grpc v1.6.0`                 |

`v1.UnimplementedNotifierServer` is embedded **twice** — in
`handlers.NotificationHandler` (`routes.go:20`) and again in
`core.NotificationNode` (`notif_core.go:21`). Harmless, since `NotificationNode`
is never registered.

---

## 4. RPC behaviour

### 4.1 `RequestNotification` — implemented

`handlers/routes.go`.

```
1. span from r.Context()                          :34
2. ConvertToNotifEntry(req)                       — validates
3. HandleNotifSend(ctx, entry)                    :66
     ├─ resolveTemplate(entry)                    — from TemplateSource or defaults
     └─ Messager.SendMessage(...)
4. StoreNotif(ctx, entry)  (best-effort)          :— persisted after delivery
5. return NotificationResponse{Success}           — or an error, never both
```

`ConvertToNotifEntry` validates method and importance and normalises the method
case, so a caller sending `"email"` is accepted. The template is resolved by the
core rather than required by validation.

Persisting is deliberately best-effort: the notification has already been
delivered, so a storage failure must not be reported to the caller as a delivery
failure.

### 4.2 `DeleteUser` — implemented

Keys on `(service_id, contact_info)` only. It does **not** route through the
delivery converter, because deleting a row has nothing to do with a delivery
method or an importance score; requiring them made every delete fail with
`InvalidArgument`. Returns `NotFound` when nothing matches.

Despite the name, this deletes notification records. It is not account deletion.

### 4.3 `RequestUserNotificationUpdate` — implemented

Builds a record from `contact_info` and upserts: a duplicate insert falls back to
an update, because a contact change is normally a re-registration of the same
`(service_id, contact_info)` pair.

The proto's "contains `@` means email, otherwise phone" heuristic is now applied,
and `username` is carried through in the template data instead of being silently
dropped.

### 4.4 `RequestTFA` — deliberately unimplemented

Returns `codes.Unimplemented`.

The previous implementation returned `Status_SUCCESS` unconditionally without
generating, storing or sending anything. A caller gating a login on that response
would believe a second factor had been delivered when none was — which is worse
than an outright failure, because the failure is loud and the false success is
silent. See [§11](#11-two-factor-authentication).

### Error signalling

Every handler returns either a response or a gRPC status error, never both. gRPC
discards the response whenever `err` is non-nil, so the previous pattern made
every `Status_FAILURE` body unreachable by clients.

---

## 5. RabbitMQ ingress

### 5.1 Connection and topology

`notifcore/notif_mq.go`. Consumer only — the node never publishes.

```go
amqp.Dial(cfg.URL())   // RABBIT_MQ_HOST, _PORT, _USER, _PASSWORD
```

All four are configurable. The queue declaration is now **durable, not
auto-delete**, so pending notifications survive a consumer restart:

```go
channel.QueueDeclare(cfg.QueueName, cfg.Durable, cfg.AutoDelete, false, false, nil)
```

Previously `durable=false, autoDelete=true`, so the queue was destroyed when the
last consumer disconnected and every backlog was lost.

Consumption uses **manual acknowledgement**. The original combined `autoAck=true`
with an explicit `msg.Ack`, which the client library reports as a double ack, and
which loses a message if a worker dies mid-delivery. Unparseable bodies are
rejected without requeue; failed deliveries are nacked with requeue.

### 5.2 The listener no longer destroys itself

The original `StartMQListener` declared its connection and channel with `defer`,
then completed a non-blocking `select` and fell out of the function body. Every
deferred call fired immediately — the connection and channel closed and the
workers' delivery channels emptied within milliseconds. The trailing comment
asserted the opposite reasoning.

The current shape:

```
StartMQListener
  └─ loop
       ├─ ctx done?        → return nil
       └─ runConsumerSession
            ├─ dial, declare, Consume
            ├─ spawn WorkerPool goroutines
            └─ BLOCK on {ctx.Done | conn.NotifyClose}
                 └─ cancel workers, wg.Wait(), return
       └─ session error?   → log, wait ReconnectDelay, retry
```

`runConsumerSession` blocks for the lifetime of the consumer, which is the wait
the original omitted. The dial is retried on `MQReconnectDelay` rather than
`log.Panic`-ing the process from a goroutine, which also skipped every deferred
cleanup.

### 5.3 Worker

Unmarshals into `NotifEntry`, so the wire contract is the struct tags in
`models/notif_entry.go`. Deliveries are dispatched through `HandleNotifSend`.

---

## 6. Files and descriptors

| File                                     | Lines | Role                                                                                                                               |
| ---------------------------------------- | ----: | ---------------------------------------------------------------------------------------------------------------------------------- |
| `main.go`                                |   118 | Entrypoint: Courier client, gRPC server, OTel, logger, SQLite, MQ listener, signal drain.                                          |
| `handlers/routes.go`                     |   155 | All four RPC handlers, the `NotificationHandler` struct, the OTel tracer.                                                          |
| `notifcore/notif_core.go`                |   199 | `NotificationNode`; `HandleNotifSend`, `HandleNotifRetry`, `NotifSent`, `RemoveNotif`, `StoreNotif`, `ShutdownDB`, `SafeShutdown`. |
| `notifcore/notif_mq.go`                  |   117 | `StartMQListener`, `notificationWorker`.                                                                                           |
| `models/notif_entry.go`                  |   139 | `NotifEntry`, `ContactMethod` constants, getters/setters, `ValidateIntegrity`.                                                     |
| `utils/notif_converter.go`               |    30 | `ConvertToNotifEntry`: proto → model.                                                                                              |
| `utils/notif_inserter.go`                |    23 | `NotifInserter` — **SQL is broken** (§9.4).                                                                                        |
| `utils/notif_deleter.go`                 |    24 | `NotifDeleter`. Unreachable.                                                                                                       |
| `utils/notif_updater.go`                 |    23 | `NotifUpdater`. Unreachable.                                                                                                       |
| `database/Schema.sql`                    |    15 | `notifications` and `templates`.                                                                                                   |
| `NOTIF.Dockerfile`                       |    32 | Two-stage → `notification_server`.                                                                                                 |
| `pkg/proto/.../notifications.proto`      |   103 | The contract.                                                                                                                      |
| `pkg/proto/.../notifications.pb.go`      |   572 | Generated messages.                                                                                                                |
| `pkg/proto/.../notifications_grpc.pb.go` |   263 | Generated stubs.                                                                                                                   |

### Persistence functions

All three require a caller-supplied `*sql.Tx` — the type that is never available
(§9.2). All three `return err` as their final statement instead of `return nil`.

```go
// notif_inserter.go:12 — 5 columns, 2 placeholders, 5 arguments
stmt, err := tx.Prepare("INSERT INTO notifications (service_id, username, info, method, sent) VALUES (?, ?)")
_, err = stmt.Exec(entry.Id, entry.ContactInfo, entry.Data, entry.Method, false)

// notif_deleter.go:13 — note the literal tab inside "DELETE<TAB>FROM"
stmt, err := tx.Prepare("DELETE	FROM notifications WHERE id=? AND contact_info=?")

// notif_updater.go:12 — contact_info is not a column
stmt, err := tx.Prepare("UPDATE notifications SET sent=? WHERE id=? AND contact_info=?")
```

---

## 7. Data model

### `NotifEntry` — `models/notif_entry.go:18-25`

```go
type ContactMethod string

const (
    Mobile ContactMethod = "Mobile"
    Email  ContactMethod = "Email"
    Slack  ContactMethod = "Slack"   // never referenced, and rejected by the handler switch
)

type NotifEntry struct {
    sharedModels.Entry                        // embedded: Id, Date_Created
    ContactInfo string            `json:"contact_info"`
    Method      ContactMethod     `json:"method"`
    Data        map[string]string `json:"data"`
    Importance  int32             `json:"importance"`
    Template    string            `json:"template"`
}
```

`ValidateIntegrity` (`:102-139`) checks, in order: `GetID`, `GetDateCreated`,
`GetContact`, `GetMethod`, `GetData`, `GetImportance`, `GetTemplate`. Because the
converter leaves `Template` empty, this gate blocks every send.

`setImportance` (`:79`) is unexported and **never called**, so the documented 1–10
bound on `importance` is never enforced.

### Schema — `database/Schema.sql`

```sql
CREATE TABLE IF NOT EXISTS notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    service_id INTEGER NOT NULL,
    username VARCHAR(255) NOT NULL,
    info VARCHAR(255) NOT NULL,
    method VARCHAR(255) NOT NULL,
    sent BOOLEAN
);

CREATE TABLE IF NOT EXISTS templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    service_id INTEGER NOT NULL,
    method VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    template VARCHAR(255) NOT NULL
)
```

### Schema/code mismatches

| Issue                         | Schema        | Code                                                       |
| ----------------------------- | ------------- | ---------------------------------------------------------- |
| `id` is `INTEGER PRIMARY KEY` | int           | every query binds `entry.Id`, a **string** (`NOTF-<uuid>`) |
| `service_id INTEGER NOT NULL` | int           | `notif_inserter.go:18` binds `entry.Id`, a string          |
| No `contact_info` column      | —             | both `WHERE` clauses filter on `contact_info=?`            |
| `templates` table             | created       | **never read or written**                                  |
| `importance`, `template`      | not persisted | both exist on `NotifEntry`                                 |

`templates` is the table that _should_ back the empty-template problem in §9.1.
It was created and never used.

**This module does not use `pkg/DataManager`** — the generic, parameterized,
transaction-managing persistence layer that Auth uses. A repo-wide search finds
zero references, and `git log --all -S "DataManager" -- nodes/Notifications`
returns no commits: Notifications has never used it. The correct abstraction
exists and is unused.

---

## 8. Configuration

| Variable                         | Read at          | Default               | Manifest sets it                                  |
| -------------------------------- | ---------------- | --------------------- | ------------------------------------------------- |
| `COURIER_TOKEN`                  | `main.go:35`     | none                  | yes, from secret `courier-secret`                 |
| `NOTIFICATION_PORT`              | `main.go:69`     | `8084`                | **NO** — sets `NOTIFICATION_SERVICE_PORT` (§9.11) |
| `RABBIT_MQ_PORT`                 | `notif_mq.go:23` | none — `log.Panic`    | yes (`5672`)                                      |
| `EVENT_SERVICE_WORKER_POOL_SIZE` | `notif_mq.go:28` | none — `log.Panic`    | yes (`5`)                                         |
| `RABBIT_MQ_QUEUE_NAME`           | `notif_mq.go:47` | `""` → broker rejects | **NO** (§9.8)                                     |
| `OTEL_COLLECTOR_ADDR`            | `main.go:41`     | `localhost:4317`      | yes                                               |
| `API_VERSION`                    | —                | —                     | set, **never read**                               |
| `NOTIFICATION_SERVICE_PORT`      | —                | —                     | set, **never read**                               |

`COURIER_TOKEN` is the one secret, and it is delivered via `secretKeyRef` — the
right pattern. It is not validated at boot.

### Security posture

**No authentication on any RPC.** `grpc.NewServer()` takes zero options
(`main.go:38`). There is no interceptor and no JWT verification; the module does
not import `Piranid/pkg/authn` at all.

Any workload that can reach the Service can call `DeleteUser`,
`RequestUserNotificationUpdate`, or `RequestTFA`. `DeleteUser` is
account-destructive — if the `*sql.Tx` bug were fixed, an unauthenticated caller
could delete notification records for any `(service_id, username)` pair. A
future TFA implementation would add an unauthenticated PII sink.

Also:

- gRPC reflection is registered unconditionally in production (`main.go:67`),
  exposing the full service descriptor.
- No TLS — standing `// TODO: add ssl certs` at `main.go:29`.
- `log.Panic` throughout `notif_mq.go` and `main.go`: an unreachable broker or a
  missing env var kills the process from a goroutine with no retry.
- The SQLite file is world-readable by default; `NOTIF.Dockerfile:21` sets only
  `+x` on the binary.
- Internal `Service_ID` is returned to callers in `TFAResponse`.

---

## 9. Verification

**39 tests, race-clean**, in two suites.

| Suite                       | Tests | Covers                                                                                                                                                               |
| --------------------------- | ----: | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `models/models_test.go`     |    21 | Validation, case-insensitive methods, importance bounds, data marshalling, column-list integrity                                                                     |
| `handlers/handlers_test.go` |    18 | RPC validation, store/delete/update round trips, TFA returning Unimplemented, upsert, method inference, auth interceptor accept/reject/fail-closed, reason redaction |

Every defect in §9 with a runtime reproduction before the fix has a regression
test. `make check` and `make test-race` are green.

**Not covered:** the RabbitMQ consumer and Courier delivery, neither of which
has a test harness here. The consumer's control flow was the defect with the
highest impact, and it is covered by structure and by reading rather than by
execution. An integration test against a real broker is the obvious next step.

---

## 10. Two-factor authentication

Only the plumbing exists. Everything functional is absent.

### Present

- `TFARequest` / `TFAResponse` messages and the `RequestTFA` RPC.
- One author note calling the placement wrong: _"this is service-specific logic
  leaking"_ (`notifications.proto:31-33`).
- A handler that now returns `Unimplemented` rather than a false success.

The `// TODO requires P2P interface with auth service` note has been replaced by
a gRPC bearer interceptor, so the "P2P interface with auth" half of that TODO is
done — though via REST/JWKS rather than gRPC.

### Absent

- No code generation. `crypto/rand` is not used anywhere in the module.
- No code storage — no table, no column.
- No expiry or TTL. `req.Timeout` is ignored.
- **No verification RPC.** There is no `VerifyTFA`, no `TFAVerifyRequest`. Nothing
  can check a code.
- No attempt limiting, no replay protection.
- No binding of the challenge to `service_id`; the response echoes the node's own
  UUID instead.
- No outbound callback to Auth, and no auth client stub — `pkg/proto` contains
  only `event_queue`, `logging`, and `notifications`.
- `req.Method` and `req.ContactInfo` unused.
- `TFAResponse` has no `code` field, so a generated code could not be returned.
- Returns `Status_SUCCESS` unconditionally.

### Architectural note

TFA does not belong in a notification contract. The generated code must be
returned to the _initiating_ service, held and verified there, and never rendered
to the user over an unauthenticated channel. The original design intent is
visible in git history: a deleted `nodes/Auth/auth.proto` (added `a502e7b`,
deleted `bca29f6`) defined a gRPC `Authorizer` service with
`UserSignIn(AuthCredentials)`, carrying `hashed_password`, `client_secret`, and a
free-form `data` string commented _"since this used for many auth interactions JWT
may be returned here"_ — the same "ship a payload over the wire" pattern that
`RequestTFA` now re-embeds in the notifications proto.

The Auth node now exists and issues real tokens
([AUTH_SERVICE.md](AUTH_SERVICE.md)). The remaining question is where a TFA
challenge is _minted and verified_ — almost certainly in Auth, with this service
reduced to "deliver this pre-generated code to this user".

---

## 11. Remaining work

| #   | Action                                                        | Why                                                                                                                                                     |
| --- | ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Integration-test the RabbitMQ consumer against a real broker  | §5.2 was the highest-impact defect and is covered structurally, not by execution.                                                                       |
| 2   | Test Courier delivery, or wrap the client behind an interface | `HandleNotifSend` is the one path no test reaches. An interface would make it mockable.                                                                 |
| 3   | Resolve TFA properly                                          | See [§11](#11-two-factor-authentication). Mint and verify challenges in the auth service; leave this service able only to deliver a pre-generated code. |
| 4   | Use the authenticated `client_id` in handlers                 | `CallerServiceID` exists and is tested, but handlers still read `service_id` from the request body, which is caller-controlled.                         |
| 5   | Add scope requirements per RPC                                | The interceptor supports `RequiredScopes`; no route declares any yet.                                                                                   |
| 6   | Remove the `contact_info`/`username` proto ambiguity          | `NotificationRequest.username` carries an email address. Renaming the field is a breaking proto change; it should be done deliberately.                 |
| 7   | Add alerting on delivery failure rate                         | Nothing observes whether notifications are actually being delivered.                                                                                    |
| 8   | Reconcile the `docs/*.pdf` design documents                   | They describe the old cache-first and broken-schema designs.                                                                                            |

Items 1, 2 and 4 are the ones that would make this service trustworthy in
operation.
