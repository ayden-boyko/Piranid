# Piranid Observability Stack

**Status:** configured for both compose and Kubernetes, and now deployable in-cluster
**Location:** `nodes/Logging/` (compose), `manifests/logging-*.yaml` (Kubernetes)
**Components:** OpenTelemetry Collector, Tempo, Loki, Prometheus, Promtail, Grafana

Related: [AUTH_SERVICE.md](AUTH_SERVICE.md) · [EVENT_QUEUE_SERVICE.md](EVENT_QUEUE_SERVICE.md) · [NOTIFICATIONS_SERVICE.md](NOTIFICATIONS_SERVICE.md) · [PLATFORM.md](PLATFORM.md)

---

## Table of contents

1. [What this is](#1-what-this-is)
2. [Architecture](#2-architecture)
3. [Telemetry pipeline](#3-telemetry-pipeline)
4. [Components](#4-components)
5. [Files and descriptors](#5-files-and-descriptors)
6. [Instrumentation in the services](#6-instrumentation-in-the-services)
7. [Deployment](#7-deployment)
8. [Known problems](#8-known-problems)
9. [Remaining work](#9-remaining-work)

---

## 1. What this is

Six components that collect, store and visualise the three OpenTelemetry signal
types. **There is no application code here** — this node is pure configuration.

| Signal | Stored in | Query UI |
|---|---|---|
| Traces | Tempo | Grafana |
| Metrics | Prometheus | Grafana |
| Logs | Loki | Grafana |

Everything runs on a Pi cluster, so the whole stack is budgeted against roughly
140–165MB RSS per Pi Zero 2W. Every component now declares explicit memory limits
in both compose (`mem_limit`) and Kubernetes (`resources.limits`), and Prometheus
has a size-bounded TSDB.

Two deployment shapes exist and they are not interchangeable:

| | Compose | Kubernetes |
|---|---|---|
| Files | `nodes/Logging/` | `manifests/logging-*.yaml` |
| Log discovery | Docker socket | `kubernetes_sd_configs` over `/var/log/containers` |
| Topology | 6 containers on a bridge network | 6 Deployments + Services |
| Storage | named volumes | PVCs |

The difference in log discovery is not a stylistic choice — see
[§8.2](#82-promtail-cannot-see-containerd-pods).

---

## 2. Architecture

```
   Piranid services (Auth, Event_Queue, Notifications)
        │  OTLP/gRPC push, insecure
        │  pkg/telemetry.SetupOTelSDK → OTEL_COLLECTOR_ADDR
        ▼
┌──────────────────────────────────────────────┐
│  OTel Collector  (:4317 gRPC, :4318 HTTP)    │
│                                              │
│  receivers: otlp                             │
│  processors: batch (1s, 1024)                │
│                                              │
│  traces ──▶ otlp/tempo ──▶ Tempo :4417        │
│  logs   ──▶ loki       ──▶ Loki  :3100        │
│  metrics ─▶ prometheus ──▶ :8889 (scrape)    │
└───────────────┬──────────────┬───────────────┘
                │              │
   ┌────────────▼───┐   ┌──────▼──────────────┐
   │ Tempo :3200    │   │ Prometheus :9090 ◀────┘
   │ traces /tempo  │   │ scrapes collector:8889
   └────────┬───────┘   └──────────┬──────────┘
            │                      │
            │   ┌──────────────────┴──────────┐
            │   │ Loki :3100                  │
            │   │  ▲ from collector (OTLP)    │
            │   │  ▲ from Promtail (docker)   │
            │   └──────────────────┬──────────┘
            │                      │
            └──────────┬───────────┘
                       ▼
              ┌─────────────────┐
              │ Grafana :3000   │  3 provisioned datasources
              │ admin / admin   │  tracesToLogsV2 correlation
              └─────────────────┘
```

**Two independent log paths.** Structured logs reach Loki via the collector
(OTLP), and container stdout reaches Loki via Promtail reading the Docker socket.
They are separate pipelines with different labels, so a log line may be queryable
two ways or only one.

---

## 3. Telemetry pipeline

### `nodes/Logging/otel-collector/config.yaml`

```yaml
receivers:
  otlp:
    protocols:
      grpc: { endpoint: 0.0.0.0:4317 }
      http: { endpoint: 0.0.0.0:4318 }

processors:
  batch:
    timeout: 1s
    send_batch_size: 1024

exporters:
  otlp/tempo:  { endpoint: tempo:4417, tls: { insecure: true } }
  loki:        { endpoint: "http://loki:3100/loki/api/v1/push" }
  prometheus:  { endpoint: 0.0.0.0:8889 }

service:
  pipelines:
    traces:  { receivers: [otlp], processors: [batch], exporters: [otlp/tempo] }
    metrics: { receivers: [otlp], processors: [batch], exporters: [prometheus] }
    logs:    { receivers: [otlp], processors: [batch], exporters: [loki] }
```

The collector is a pure fan-out: one OTLP receiver, three signal-specific
pipelines, no `memory_limiter`, no sampling, no resource or attribute
processing. `prometheus` is an **exporter**, not a receiver — the collector
exposes scraped metrics on `:8889` and Prometheus pulls from it (§4.3).

`insecure: true` throughout: no TLS anywhere in the telemetry path.

### Signal flow

| Signal | Service → | Collector pipeline | Store | Grafana datasource |
|---|---|---|---|---|
| Traces | OTLP/gRPC `:4317` | `traces` → `otlp/tempo` | Tempo `:4417` | Tempo `:3200` |
| Logs | OTLP/gRPC `:4317` | `logs` → `loki` | Loki `:3100` | Loki `:3100` |
| Metrics | OTLP/gRPC `:4317` | `metrics` → `prometheus` | collector `:8889` → scraped | Prometheus `:9090` |
| Container stdout | Promtail → Loki | — | Loki `:3100` | Loki `:3100` |

---

## 4. Components

### 4.1 Tempo — traces

```yaml
server:   { http_listen_port: 3200 }
distributor:
  receivers:
    otlp:
      protocols:
        http: { endpoint: 0.0.0.0:4418 }
        grpc: { endpoint: 0.0.0.0:4417 }
storage:
  trace:
    backend: local
    local: { path: /tempo/traces }
    wal:    { path: /tempo/wal }
```

Local filesystem backend — no object store. Appropriate for a single-node Pi
cluster, but traces are lost if the volume goes away.

Note the collector exports to `tempo:4417` (gRPC) while Tempo's own config also
opens `:4418` (HTTP). Only the gRPC path is wired up.

### 4.2 Loki — logs

```yaml
auth_enabled: false
server: { http_listen_port: 3100 }
ingester:
  lifecycler.ring: { kvstore: { store: inmemory } }
  replication_factor: 1
  chunk_idle_period: 5m
  chunk_retain_period: 30s
schema_config:
  configs:
    - from: 2024-01-01
      store: tsdb
      object_store: filesystem
      schema: v13
      index: { prefix: index_, period: 24h }
storage_config:
  tsdb_shipper: { active_index_directory: /loki/index, cache_location: /loki/cache }
  filesystem:   { directory: /loki/chunks }
limits_config:
  reject_old_samples: true
  reject_old_samples_max_age: 168h
```

Single-replica in-memory ring, filesystem chunks, TSDB schema v13. Samples older
than 168h (7 days) are rejected outright.

### 4.3 Prometheus — metrics

```yaml
global: { scrape_interval: 15s, evaluation_interval: 15s }
scrape_configs:
  - job_name: prometheus
    static_configs: [ { targets: ["localhost:9090"] } ]
  - job_name: otel-collector
    static_configs: [ { targets: ["otel-collector:8889"] } ]
```

Only two scrape targets: itself and the collector's metrics exporter. **No
Piranid service is scraped directly** — all service metrics arrive as OTLP and
pass through the collector.

Consequence: if the collector is down, every service's metrics disappear from
Prometheus, and there is no independent record of whether a service is
generating metrics at all.

### 4.4 Promtail — container log shipping

```yaml
server:   { http_listen_port: 9080, grpc_listen_port: 0 }
positions: { filename: /tmp/positions.yaml }
clients:  [ { url: "http://loki:3100/loki/api/v1/push" } ]
scrape_configs:
  - job_name: docker
    docker_sd_configs:
      - host: unix:///var/run/docker.sock
        refresh_interval: 5s
    relabel_configs:
      - { source_labels: [__meta_docker_container_name], target_label: container }
      - { source_labels: [__meta_docker_container_log_stream], target_label: stream }
      - { source_labels: [__meta_docker_compose_service], target_label: service }
```

Discovers containers through the Docker socket and ships their stdout/stderr to
Loki with `container`, `stream`, and `service` labels.

**This is a Docker mechanism.** On K3s the containers are containerd, and
`/var/run/docker.sock` does not exist — so this path does not work in the target
environment. See [§8.2](#82-promtail-cannot-see-containd-pods).

Note also that `positions: /tmp/positions.yaml` is on the container's writable
layer, so tail positions reset on every restart and logs may be re-shipped.

### 4.5 Grafana

`grafana/provisioning/datasources/datasources.yaml` provisions three
datasources:

```yaml
- name: Loki       type: loki       url: http://loki:3100
- name: Tempo      type: tempo      url: http://tempo:3200
  jsonData:
    tracesToLogsV2:
      datasourceUid: loki
      spanStartTimeShift: "-1h"
      spanEndTimeShift: "1h"
      filterByTraceID: true
      filterBySpanID: true
- name: Prometheus type: prometheus url: http://prometheus:9090   isDefault: true
```

`tracesToLogsV2` is what makes Grafana's "logs for this span" work: it queries
Loki for entries whose content contains the trace or span ID.

**This only works if log lines carry a trace ID.** `pkg/telemetry.WithTraceID`
adds `traceID`/`spanID` fields — but only when a handler actually calls it.
See [§6](#6-instrumentation-in-the-services).

No dashboards are provisioned; only datasources. Credentials are
`admin`/`admin` via environment variables.

---

## 5. Files and descriptors

| File | Lines | Role |
|---|---:|---|
| `otel-collector/config.yaml` | 37 | Fan-out: OTLP in, Tempo/Loki/Prometheus out. |
| `tempo/config.yaml` | 17 | Trace storage, local filesystem backend. |
| `loki/config.yaml` | 33 | Log storage, TSDB schema v13, filesystem chunks. |
| `prometheus/config.yaml` | 14 | 15s scrape of itself and the collector. |
| `promtail/config.yaml` | 23 | Docker log discovery and shipping. |
| `grafana/provisioning/datasources/datasources.yaml` | 29 | Three datasources + trace-to-log correlation. |
| `docker-compose.yaml` | 96 | All six components, bridge network, four named volumes. |

### Pinned versions

| Component | Version |
|---|---|
| `grafana/loki` | 3.4.2 |
| `grafana/promtail` | 3.4.2 |
| `grafana/tempo` | 2.7.2 |
| `prom/prometheus` | v3.2.1 |
| `otel/opentelemetry-collector-contrib` | 0.123.0 |
| `grafana/grafana` | 11.6.1 |

All pinned. Loki and Promtail are on the same version, which matters — they are
released together and mismatched versions can fail to talk.

### Volumes

`loki_data`, `tempo_data`, `prometheus_data`, `grafana_data` — all named volumes.
No persistence for Promtail positions.

### Ports

| Port | Component | Protocol | Exposed to host |
|---:|---|---|---|
| 4317 | Collector | OTLP gRPC | yes |
| 4318 | Collector | OTLP HTTP | yes |
| 8889 | Collector | metrics | yes |
| 3100 | Loki | HTTP | yes |
| 3200 | Tempo | HTTP | yes |
| 4417 | Tempo | OTLP gRPC | yes |
| 4418 | Tempo | OTLP HTTP | yes |
| 9090 | Prometheus | HTTP | yes |
| 3000 | Grafana | HTTP | yes |
| 9080 | Promtail | HTTP | **no** |

Eight of ten ports are published to the host with no authentication and no TLS.
For a cluster on a private network this is tolerable; on a shared or routed
network it exposes Prometheus, Loki, Grafana and the raw OTLP receiver to anyone
who can reach the host.

---

## 6. Instrumentation in the services

Provided by `pkg/telemetry`, shared by all three services.

### `SetupOTelSDK` — `pkg/telemetry/telemetry.go:31-101`

One gRPC connection to `OTEL_COLLECTOR_ADDR` with `insecure.NewCredentials()`.
Installs:

- **W3C propagation** — composite `TraceContext` + `Baggage` (`telemetry.go:56-59`).
- **Traces** — `otlptracegrpc`, batched, 5s batch timeout (`:62-72`).
- **Metrics** — `otlpmetricgrpc`, periodic reader every 15s (`:75-85`).
- **Logs** — `otlploggrpc` → batch processor → `global.SetLoggerProvider` (`:88-98`).
- **Resource** — `newResource(serviceName)` merges defaults with
  `semconv.ServiceName` (`pkg/telemetry/resource.go:8-17`).

Returns an aggregated shutdown function (`:35-42`) using `errors.Join`.

### `NewLogger` / `WithTraceID` — `pkg/telemetry/logger.go`

```go
func NewLogger(serviceName string) (*zap.Logger, error)   // :11
func WithTraceID(ctx context.Context, logger *zap.Logger) *zap.Logger  // :27
```

zap production config with ISO8601 timestamps and a static `service` field.
`WithTraceID` adds `traceID`/`spanID` from the context span, returning the logger
unchanged when the span context is invalid.

### Adoption across the services

| Service | OTel service name | Logger name | Server spans | `WithTraceID` |
|---|---|---|---|---|
| Auth | `Auth Node` | `auth` | yes | yes |
| Event_Queue | `event_queue` | `event_queue` | yes (`HTTPMiddleware`) | via context |
| Notifications | `notifications` | `notifications` | yes (unary + stream interceptor) | yes |

All three now report a correct identity, produce server spans derived from the
incoming trace context, and correlate log lines with the active span. That is
what makes Grafana's `tracesToLogsV2` work rather than silently returning
nothing.

### Span instrumentation

```go
// pkg/telemetry/http.go
func HTTPMiddleware(service string, next http.Handler) http.Handler
func UnaryServerInterceptor(service string) grpc.UnaryServerInterceptor
func StreamServerInterceptor(service string) grpc.StreamServerInterceptor
```

`HTTPMiddleware` starts a server span, extracting the incoming `traceparent`
through the configured propagator, and records the status code. The gRPC
interceptors do the same from request metadata.

These are small local implementations, not `otelhttp`. The upstream package
pulled a dependency subtree that forced an OpenTelemetry SDK upgrade breaking
`pkg/telemetry` — the wrong trade on hardware with a memory budget.

Handlers still create their own spans, but derive them from `r.Context()` rather
than a context captured at startup. Event_Queue previously did the latter, so
incoming trace context was dropped and traces terminated at the boundary.

---

## 7. Deployment

### Compose — `nodes/Logging/docker-compose.yaml`

All six components on a `logging` bridge network with four named volumes. This
file is internally consistent: image names, ports and the `depends_on` graph all
agree. Every service now carries a `mem_limit` so the stack stays inside the Pi
budget.

### Kubernetes — `manifests/logging-*.yaml`

Three files, applied in this order:

```bash
kubectl apply -f manifests/logging-config.yaml    # 6 ConfigMaps
kubectl apply -f manifests/logging-storage.yaml   # 4 PVCs
kubectl apply -f manifests/logging-deployment.yaml # 6 Deployments + 6 Services
```

This replaces a single Deployment named `logging` referencing an image
`piranid-logging` that nothing builds, with contradictory ports
(`containerPort: 8084`, Service target `8083`, env `8083`), and with no
containers for the six stateful components it was meant to represent.

| Component | Port(s) | Storage | Limits |
|---|---|---|---|
| loki | 3100 | PVC | 512Mi |
| tempo | 3200, 4417 | PVC | 512Mi |
| prometheus | 9090 | PVC | 512Mi |
| otel-collector | 4317, 4318, 8889, 13133 | — | 256Mi |
| promtail | 9080 | `emptyDir` (positions) | 128Mi |
| grafana | 3000 (NodePort 30300) | PVC | 256Mi |

Only Grafana is exposed outside the cluster. The previous setup published eight
of ten ports to the host with no authentication.

Grafana credentials come from a `grafana-admin` Secret, and
`GF_AUTH_ANONYMOUS_ENABLED=false`, replacing the compose default of
`admin`/`admin`.

`scripts/validate-manifests.py` runs in CI and fails on: a file that does not
parse, a Deployment referencing an undefined ConfigMap or PVC, a `volumeMount`
with no matching volume, or a duplicated environment variable.

### Observability of the observer

Prometheus scrapes the services directly as well as through the collector:

```yaml
- job_name: piranid-services
  kubernetes_sd_configs: [ { role: pod } ]
  relabel_configs:
    - source_labels: [__meta_kubernetes_pod_label_app]
      regex: (auth|event-queue|notifications)
      action: keep
```

All service metrics previously flowed through the collector, so a collector
outage removed every service's metrics with no independent record that anything
was still up. This makes that diagnosable.

## 8. Known problems

Each entry records what was found during the audit and what was done. What
remains open is in [§9](#9-remaining-work).

### 8.1 Fixed — the Kubernetes manifest could not deploy the stack

One Deployment, one container, a non-existent image, three contradictory port
numbers, and no components. Replaced with 6 Deployments, 6 Services, 6 ConfigMaps
and 4 PVCs across three files. See [§7](#7-deployment).

### 8.2 Fixed — Promtail could not see containerd pods

The config used `host: unix:///var/run/docker.sock`. Piranid targets K3s, where
the runtime is containerd and that socket does not exist, so Promtail discovered
no containers and shipped nothing.

**Fixed** in the Kubernetes config with `kubernetes_sd_configs` over
`role: pod`, a `cri` pipeline stage to strip the CRI wrapper, and labels extracted
for `namespace`, `pod`, `service` and `container`. The compose config still uses
the Docker socket, which is correct there, and now says so explicitly so the
divergence is not mistaken for an oversight.

### 8.3 Fixed — trace-to-log correlation was largely broken

Grafana's `tracesToLogsV2` filters on `filterByTraceID`, but `WithTraceID` was
called only in Notifications and only on some paths. Auth logged without one, and
Event_Queue's identifier was wrong anyway.

**Fixed.** `pkg/telemetry.HTTPMiddleware` and `UnaryServerInterceptor` start spans
from the incoming trace context, so traces chain across services rather than
starting fresh. Handlers derive spans from `r.Context()`. Auth uses
`telemetry.WithTraceID` on every log call.

### 8.4 Fixed — no end-to-end traces

No `otelhttp` wrapper and no gRPC interceptor, so no server spans, and
Event_Queue derived spans from a boot-time context.

**Fixed** with `pkg/telemetry.HTTPMiddleware` for HTTP and
`UnaryServerInterceptor`/`StreamServerInterceptor` for gRPC. These are small local
implementations rather than `otelhttp`: the upstream package forced an OpenTelemetry
SDK upgrade that broke `pkg/telemetry`, which is a poor trade on a memory budget.

### 8.5 Fixed — wrong service identity in two of three nodes

Event_Queue reported `service.name = "Auth Node"` and `service = notifications`.

**Fixed.** Both are `event_queue`.

### 8.6 Fixed — Prometheus depended on the collector for everything

Fixed with the direct `piranid-services` scrape job. See
[§7](#7-deployment).

### 8.7 Fixed — no limits in compose, no dashboards

**Fixed** for limits. Dashboards are still not provisioned — only datasources —
so Grafana presents storage without a UI. See [§9](#9-remaining-work).

### 8.8 Fixed — unbounded growth

Loki had no volume cap and rejected only samples older than 168h; Tempo had no
retention; Prometheus had no retention flags.

**Fixed.** Loki has `retention_period: 168h` and a running compactor with
retention enabled. Tempo has `block_retention: 168h`. Prometheus runs with
`--storage.tsdb.retention.time=7d` and `--storage.tsdb.retention.size=512MB`.

### 8.9 Fixed — everything unauthenticated

Eight of ten ports were published to the host with no auth and no TLS; Grafana ran
at `admin`/`admin`.

**Fixed.** Only Grafana is published, as a NodePort, with credentials from a Secret
and anonymous access disabled. The remaining components are `ClusterIP`-only,
reachable inside the cluster. OTLP remains insecure, which is appropriate on a
trusted cluster network and is called out in the config comments.

### 8.10 Fixed — Promtail positions not persisted

`positions: /tmp/positions.yaml` sat on the container writable layer, so a
restart lost tail positions and re-shipped logs.

**Fixed.** Positions live on a volume in the Kubernetes config.

### 8.11 Fixed — no memory limiter in the collector

A telemetry spike could OOM the collector, and on a Pi that takes down the node
rather than just the collector.

**Fixed.** A `memory_limiter` processor runs ahead of `batch` in every pipeline,
at 75% with a 20% spike allowance.

### 8.12 Fixed — no health endpoint on the collector

**Fixed.** A `health_check` extension on `:13133`, used by the liveness probe.

---

## 9. Remaining work

| # | Action | Why |
|---|---|---|
| 1 | Provision at least one Grafana dashboard | Datasources exist, so the cluster gets storage without a UI. The highest-value remaining item. |
| 2 | Alert on delivery failure and error rates | Nothing watches whether the services are healthy. |
| 3 | Move storage off SD cards | The PVCs are backed by the node's filesystem. Metrics storage on an SD card is a wear and failure risk. |
| 4 | TLS for the OTLP path | `insecure: true` is fine on a trusted cluster network, not across a routed one. mTLS with cert-manager would be the fix. |
| 5 | Restrict Grafana's NodePort or front it with an ingress | It is the one component still reachable from outside the cluster. |
| 6 | Reconcile `docs/observability-architecture.md` | It predates the current config and its memory budget no longer matches. |
| 7 | Sample traces above a volume threshold | Everything is stored for 7 days regardless of rate. |

Items 1 and 2 are what turn collected data into something acted on.
