# Cloud-Connected Kubernetes Cluster on Raspberry Pi

## Overview

This document describes the architecture of a cloud-connected Kubernetes cluster built using Raspberry Pi devices. The cluster consists of a Raspberry Pi 4B acting as the control plane and multiple Raspberry Pi Zero 2W nodes serving as worker nodes. The system orchestrates microservices using K3s, a lightweight Kubernetes distribution, which also handles load balancing.

---

## Cluster Architecture

### Control Plane on Raspberry Pi 4B

The Raspberry Pi 4B functions as the master node and manages the overall state of the cluster. It performs the following tasks:

- **Kubernetes API Server:** Handles all requests related to cluster management, including deploying new microservices and scaling existing ones.
- **Scheduler:** Distributes microservices across the Pi Zero 2W worker nodes based on resource availability and constraints.
- **Cluster State Management:** Stores and maintains state information using the `etcd` database to ensure consistency.
- **Controller Processes:** Regulates the cluster state, ensuring the desired number of microservice pods remain operational.

### Worker Nodes on Raspberry Pi Zero 2W

Each Pi Zero 2W functions as a worker node, running containerized microservices assigned by the control plane. These nodes:

- **Run a Container Runtime:** Likely `containerd`, to execute microservice containers.
- **Communicate with the Control Plane:** Receive and execute commands to start, stop, or manage containers.
- **Maintain Network Connectivity:** Use a network proxy to enable seamless communication between microservice pods and external systems.

---

## Microservice Deployment and Management

1. **Microservice Deployment:**
   - A YAML manifest is submitted to the API server on the Pi 4B.
   - The scheduler assigns the microservice to a Pi Zero 2W based on resource availability.

2. **Service Discovery & Communication:**
   - The Pi 4B manages service discovery, ensuring microservices running on different Pi Zero 2Ws can communicate seamlessly.

3. **Scaling and Load Balancing:**
   - When a microservice needs to scale, the Pi 4B distributes traffic across multiple instances on different Pi Zero 2Ws.

4. **Zero-Downtime Updates:**
   - The control plane orchestrates rolling updates, gradually updating instances across the worker nodes.

---

## Distributed Services on Raspberry Pi Nodes

Each microservice runs in its own Docker container, deployed across different Raspberry Pi nodes.

### Core Microservices:

on Pi 4B

- **Message Queue Service:** Handles asynchronus messages and events between services.
- **Logging & Monitoring Service:** Centralizes log collection and system monitoring.

on Pi 02w

- **User Authentication Service:** OAuth 2.0 authorization server (authorization code grant with mandatory PKCE). Issues RS256 JWT access tokens and publishes its public signing key at `/.well-known/jwks.json`, so consuming services verify tokens (signature, issuer, audience, expiry) without ever holding the signing key. See [AUTH_SERVICE.md](AUTH_SERVICE.md).
- **Notification Service:** gRPC service delivering email and SMS via Courier, with a RabbitMQ consumer as an asynchronous path. Every RPC requires a valid bearer token. See [NOTIFICATIONS_SERVICE.md](NOTIFICATIONS_SERVICE.md).
- **Message Queue Service:** RabbitMQ broker; the Event_Queue node administers its queues. See [EVENT_QUEUE_SERVICE.md](EVENT_QUEUE_SERVICE.md).
- **Logging & Monitoring Service:** Six-component observability stack (collector, Tempo, Loki, Prometheus, Promtail, Grafana). See [LOGGING_SERVICE.md](LOGGING_SERVICE.md).
- **MQTT Broker Service** Handles Events from IOT devices

### Service documentation

| Document | Covers |
|---|---|
| [AUTH_SERVICE.md](AUTH_SERVICE.md) | Authorization server: flows, token lifecycle, endpoints, files, security properties |
| [EVENT_QUEUE_SERVICE.md](EVENT_QUEUE_SERVICE.md) | Queue administration REST API: routes, drain protocol, files, known problems |
| [NOTIFICATIONS_SERVICE.md](NOTIFICATIONS_SERVICE.md) | gRPC notification delivery: proto contract, RPC behaviour, files, TFA |
| [LOGGING_SERVICE.md](LOGGING_SERVICE.md) | Observability stack: collector pipelines, Tempo/Loki/Prometheus/Grafana, instrumentation |
| [PLATFORM.md](PLATFORM.md) | Shared packages, build system, deployment surface, cross-service gaps |

> **Current state.** All three services are implemented, build, vet clean, and
> pass their tests under the race detector — 135 in total. Auth issues and
> publishes tokens; Event_Queue administers queues and verifies tokens;
> Notifications delivers via Courier over gRPC and RabbitMQ, and verifies tokens
> on every RPC. `make check-all` and `make test-race` are green, and CI runs
> both plus manifest validation and image builds. `PLATFORM.md §8` lists what
> remains, which is concentrated in integration testing, authorization and TLS.

---

## Key Technologies

### Raspberry Pi 4B as Controller:

- Functions as the **main control node** for the Kubernetes cluster.
- Acts as an **API gateway and load balancer**, routing requests to appropriate microservices.

### Raspberry Pi Zero 2Ws as Worker Nodes:

- Each Pi Zero 2W runs as a **worker node** hosting dockerized microservices.
- Dedicated nodes handle different functionalities for efficient load distribution.

### K3s for Kubernetes Orchestration:

- A **lightweight Kubernetes distribution** designed for IoT and edge computing.
- Manages deployment, scaling, and maintenance of microservices across the cluster.

### k3 as Reverse Proxy and Load Balancer:

- Used on the Pi 4B to handle **incoming requests and routing**.

---

## Conclusion

This cloud-connected Kubernetes cluster on Raspberry Pi provides an efficient microservices architecture, leveraging the power of containerization and orchestration. The Pi 4B serves as the central controller, while Pi Zero 2W nodes run distributed microservices, enabling scalability, resilience, and flexibility for edge computing applications.
