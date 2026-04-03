# Observability Architecture — ClusterHAT + Pi 4B

## Cluster Layout

```
ClusterHAT (attached to Pi 4B)
├── Pi Zero 2W #1 → Go service A
├── Pi Zero 2W #2 → Go service B
├── Pi Zero 2W #3 → Go service C
└── Pi Zero 2W #4 → Go service D
          ↕ business events via RabbitMQ
Pi 4B (k3s control plane)
├── RabbitMQ          — message bus
├── RabbitMQ controller (Go) — topology manager
├── Tempo             — trace storage
├── Loki              — log storage
├── Prometheus        — metrics storage
└── Grafana           — visualises all three
```

---

## What Runs Where and Why

### Pi Zero 2W (each node)

- **Your Go service** — the domain service for that node
- **Promtail** — ships logs to Loki on the 4B (~30MB, acceptable)
- **OTel SDK** (embedded in the Go binary) — emits traces and metrics directly to the 4B

### Pi 4B

Everything that serves the cluster as a whole lives here:

- **RabbitMQ** — central message bus, all Zeros depend on it
- **RabbitMQ controller** — Go service that manages exchanges, queues, and bindings dynamically (think k3s controller but for RabbitMQ topology). Kept here because it is tightly coupled to RabbitMQ — separating them adds unnecessary network hops and a fragile dependency on a weak node
- **k3s control plane**
- **Tempo, Loki, Prometheus, Grafana**

---

## Observability Pipeline

### Traces

```
Go app (OTel SDK) → OTLP/gRPC → Tempo (4B) → Grafana
```

- The OTel SDK is embedded directly in the Go binary
- Exports spans directly to Tempo on the 4B — no OTel Collector needed on the Zero
- Use `TraceIDRatioBased(0.1)` sampler in production to reduce CPU/network overhead
- Always sample errors regardless of ratio

### Logs

```
Go app (stdout) → Promtail (Zero) → Loki (4B) → Grafana
```

- Your app just writes structured logs to stdout
- Promtail tails stdout and ships to Loki — you write zero extra code
- Each Zero runs its own Promtail instance with an `instance` label so you can tell nodes apart in Grafana
- Loki stores and indexes logs, serves them to Grafana. There is no separate "logging service" to write or maintain

### Metrics

```
Go app (OTel SDK) → Prometheus scrape or OTLP push → Prometheus (4B) → Grafana
```

---

## Distributed Tracing Across Services

Since services communicate via RabbitMQ (not HTTP), trace context does not travel automatically. You propagate it manually via message headers.

### Producer (publishing a message)

```go
headers := amqp.Table{}
otel.GetTextMapPropagator().Inject(ctx, amqp091HeaderCarrier(headers))
msg := amqp.Publishing{Headers: headers, Body: payload}
ch.Publish(exchange, key, false, false, msg)
```

### Consumer (receiving a message)

```go
ctx = otel.GetTextMapPropagator().Extract(ctx, amqp091HeaderCarrier(msg.Headers))
ctx, span := tracer.Start(ctx, "order.process")
defer span.End()
```

You need a small `amqp091HeaderCarrier` adapter (~15 lines) implementing `TextMapCarrier`. This is what links a trace across multiple Zeros into one continuous flame graph in Grafana — without it, each service's spans are isolated islands.

---

## Why RabbitMQ Is Not Used for Logs

RabbitMQ is designed for business events — work that needs to be queued, retried, and acknowledged. Logs are not work items. Routing logs through RabbitMQ means:

- RabbitMQ becomes part of your observability critical path — if it goes down, you lose visibility exactly when you need it most
- You need to write and maintain a log consumer service
- Unprocessed logs back up in the queue under load

Promtail → Loki is the correct path. If Loki is temporarily unreachable, Promtail buffers locally and retries. Unimportant logs that drop during an outage are an acceptable loss — that is the correct behavior.

---

## Why the 4B Is the Right Hub

The Zeros are already fully dependent on the 4B:

- k3s control plane is on the 4B — if it goes down, workload scheduling stops
- RabbitMQ is on the 4B — if it goes down, inter-service communication stops

Adding direct OTLP connections from each Zero to the 4B does not meaningfully increase coupling. The 4B is already the single point of failure by design. Keeping all observability backends there is consistent with that architecture, not a new risk.

---

## Memory Budget (Per Zero)

| Component           | Estimated RSS  |
| ------------------- | -------------- |
| OS baseline         | ~80MB          |
| Go runtime          | ~10MB          |
| Your app logic      | ~10–30MB       |
| OTel SDK + exporter | ~8–15MB        |
| Promtail            | ~30MB          |
| **Total**           | **~140–165MB** |

Leaves ~350MB headroom on a 512MB Zero. The risk is not memory — it is CPU spikes during span flushes. A `BatchSpanProcessor` with a 5s timeout and ratio-based sampling keeps this flat.

---

## Key Environment Variables (Go services)

| Variable                      | Default           | Description                           |
| ----------------------------- | ----------------- | ------------------------------------- |
| `OTEL_SERVICE_NAME`           | `unknown_service` | Service name in Tempo/Grafana         |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `<4B IP>:4317`    | Tempo endpoint on the 4B              |
| `SERVICE_VERSION`             | `dev`             | Attached as a span resource attribute |

---

## Quick Reference — What Each Tool Does

| Tool                | Role                                            | Where                  |
| ------------------- | ----------------------------------------------- | ---------------------- |
| OTel SDK            | Generates traces/spans inside your Go binary    | Each Zero (in-process) |
| Promtail            | Ships stdout logs to Loki                       | Each Zero (sidecar)    |
| Tempo               | Stores and queries traces                       | Pi 4B                  |
| Loki                | Stores and queries logs                         | Pi 4B                  |
| Prometheus          | Stores and queries metrics                      | Pi 4B                  |
| Grafana             | Visualises all three with trace↔log correlation | Pi 4B                  |
| RabbitMQ            | Business event bus between services             | Pi 4B                  |
| RabbitMQ controller | Dynamically manages queue/exchange topology     | Pi 4B                  |
