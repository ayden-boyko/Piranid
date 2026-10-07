# Piranid: Cloud-Connected Kubernetes Cluster on Raspberry Pi

## Overview

Piranid is a Kubernetes cluster built from Raspberry Pis, running a set of Go
microservices. A Raspberry Pi 4B hosts the control plane and several worker
nodes run the services. The project exists to work through service design,
inter-service communication and observability under a real hardware budget —
512MB–1GB of RAM per node, and an SD card for storage.

## Hardware

| Role | Device |
|---|---|
| Control plane | 1 × Raspberry Pi 4B |
| Workers | 4 × Raspberry Pi Zero 2W |
| Network | Cluster Hat v2.5 |

## Repository layout

```plaintext
piranid/
├── docs/                     service documentation, see below
│   ├── AUTH_SERVICE.md           authorization server, in depth
│   ├── EVENT_QUEUE_SERVICE.md    queue administration API
│   ├── NOTIFICATIONS_SERVICE.md  gRPC notification delivery
│   ├── LOGGING_SERVICE.md        observability stack
│   ├── PLATFORM.md               shared packages, build, cross-service gaps
│   └── Overview.md               this architecture summary
│
├── nodes/                    worker node code
│   ├── Auth/                 OAuth 2.0 authorization server  (:8081)
│   ├── Event_Queue/          RabbitMQ queue administration   (:8082)
│   ├── Notifications/        gRPC notification delivery      (:8084)
│   ├── Logging/              observability config (compose)
│   └── Iot/                  stub
│
├── pkg/                      shared library
│   ├── authn/                token format: RS256, JWKS, verifier, middleware
│   ├── DataManager/          generic parameterized persistence
│   ├── dbutil/               SQLite with WAL and BEGIN IMMEDIATE
│   ├── node/                 HTTP node harness, separate module Piranid/node
│   ├── telemetry/            OpenTelemetry SDK, zap logger, HTTP/gRPC interceptors
│   └── proto/                gRPC contracts (notifications only has codegen)
│
├── manifests/                Kubernetes manifests
│   ├── namespace.yaml
│   ├── auth-deployment.yaml
│   ├── event-deployment.yaml
│   ├── notif-deployment.yaml
│   ├── logging-deployment.yaml      six components, one per Deployment
│   ├── logging-config.yaml           ConfigMaps for the above
│   ├── logging-storage.yaml          PVCs for the above
│   └── rabbitmq-deployment.yaml
│
├── scripts/                  deploy.sh, join-cluster.sh (both empty),
│                             fix-modules.sh
├── tests/                    test scaffolding
├── go.work                   Go workspace, five modules
├── Makefile                  build, vet, test, fmt, keys
└── docker-compose.yml        not usable, see docs/PLATFORM.md §5
```

## Services

| Service | Transport | Port | Status |
|---|---|---|---|
| **Auth** | HTTP | 8081 | Implemented, tested. OAuth 2.0 authorization code + PKCE, RS256 tokens, JWKS endpoint. |
| **Event_Queue** | HTTP | 8082 | Implemented, tested. RabbitMQ queue administration for other services. |
| **Notifications** | gRPC | 8084 | Implemented, tested. Email/SMS delivery via Courier, with a RabbitMQ consumer. |
| **Logging** | config | — | Six-component observability stack: collector, Tempo, Loki, Prometheus, Promtail, Grafana. |

Each service has a document under `docs/` describing its architecture, request
lifecycle, files, configuration and known problems.

## Authentication

The auth node is the only component holding a credential secret. It:

- authenticates users through the OAuth 2.0 authorization code grant with
  mandatory PKCE (RFC 7636, S256);
- issues short-lived RS256 JWTs (15 minutes by default);
- publishes its public key at `/.well-known/jwks.json` (RFC 7517).

Services verify those tokens against the published public key and check the
signature, issuer, audience and expiry. No service other than auth holds a
signing key, so a compromised service can verify tokens but cannot mint them.

See [docs/AUTH_SERVICE.md](docs/AUTH_SERVICE.md).

## Build and verify

```bash
make check          # gofmt check, go vet, and tests, across all modules
make test-race      # the same tests under the race detector
make keys           # generate an RSA signing key pair into jwt-keys/
```

Requires Go 1.26 or newer. The build depends on the Go workspace: the service
modules reference `pkg` and `pkg/node` through `replace` directives that only
resolve inside `go.work`, so `GOWORK=off` will not work.

## Quick start

```bash
# 1. Generate a signing key pair. The private key must never be committed;
#    .gitignore covers *.pem and jwt-keys/.
make keys

# 2. Run the auth node
cd nodes/Auth && go run . \
  AUTH_PORT=8081 \
  AUTH_ISSUER=https://auth.piranid.local \
  AUTH_AUDIENCE=event-queue \
  AUTH_PRIVATE_KEY_PATH=../../jwt-keys/jwt_private.pem \
  AUTH_DB_PATH=/tmp/auth.db

# 3. In another shell, run the event queue node
cd nodes/Event_Queue && go run . \
  EVENT_QUEUE_PORT=8082 \
  RABBIT_MQ_HOST=localhost RABBIT_MQ_PORT=5672 \
  AUTH_JWKS_URL=http://localhost:8081/.well-known/jwks.json \
  AUTH_ISSUER=https://auth.piranid.local \
  AUTH_AUDIENCE=event-queue
```

`AUTH_ISSUER`, `AUTH_AUDIENCE` and the key path are required: a service that
cannot verify tokens refuses to start rather than serving unauthenticated.

## Deployment

Images are built from the repository root, because the modules depend on `pkg`
through the workspace:

```bash
docker build -f nodes/Auth/AUTH.Dockerfile           -t piranid-auth:latest .
docker build -f nodes/Event_Queue/EVENT.Dockerfile   -t piranid-event:latest .
docker build -f nodes/Notifications/NOTIF.Dockerfile -t piranid-notifications:latest .
```

The manifests use `imagePullPolicy: Never`, so images must be loaded onto each
node (`docker save` / `docker load`, or `k3s ctr images import`).

```bash
kubectl apply -f manifests/namespace.yaml
kubectl apply -f manifests/logging-config.yaml -f manifests/logging-storage.yaml
kubectl apply -f manifests/logging-deployment.yaml
kubectl apply -f manifests/rabbitmq-deployment.yaml
kubectl apply -f manifests/auth-deployment.yaml
kubectl apply -f manifests/event-deployment.yaml
kubectl apply -f manifests/notif-deployment.yaml
```

Secrets that must exist first:

```bash
kubectl -n piranid create secret generic auth-signing-keys \
  --from-file=jwt_private.pem=jwt-keys/jwt_private.pem \
  --from-file=jwt_public.pem=jwt-keys/jwt_public.pem

kubectl -n piranid create secret generic courier-secret \
  --from-literal=COURIER_TOKEN=<token>

kubectl -n piranid create secret generic grafana-admin \
  --from-literal=user=admin --from-literal=password=<password>
```

## Technologies

| Concern | Choice |
|---|---|
| Language | Go (workspace, five modules) |
| Orchestration | K3s |
| Service communication | HTTP (auth, event queue), gRPC (notifications), RabbitMQ |
| Authentication | OAuth 2.0 + PKCE, RS256 JWT, JWKS |
| Storage | SQLite with WAL |
| Metrics, traces, logs | OpenTelemetry → Tempo, Prometheus, Loki, Grafana |

## Current state

Auth, Event_Queue and Notifications all build, vet clean and have passing tests
(135 total). The observability stack is configured for both compose and
Kubernetes. `docs/PLATFORM.md §8` tracks the remaining work, which is
concentrated in durability and operational hardening rather than missing
functionality.