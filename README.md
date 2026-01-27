# Piranid: Cloud-Connected Kubernetes Cluster

## Overview

Piranid is a personal project I built to learn more about Kubernetes, distributed systems, and running real workloads on limited hardware. It’s a small Kubernetes cluster made from Raspberry Pis, with a lightweight control plane running on a Raspberry Pi 4B (called a *Norn*) and several Raspberry Pi Zero 2Ws acting as worker nodes (*Gaunts*).

The goal wasn’t just to get Kubernetes running, but to understand how scheduling, service communication, and monitoring work under real constraints. I also wanted the cluster to be cloud-aware, so it integrates with external services for metrics, logging, and management. Piranid acts as a hands-on testbed for experimenting with microservices, hybrid cloud ideas, and cluster automation.

## Repository Structure

```plaintext
piranid/
├── docs/                          # Design notes and documentation
├── controllers/                   # Control plane code (Pi 4B / Norns)
│   ├── main.go                    # Controller entry point
│   ├── scheduler/                 # Custom scheduling logic
│   ├── api/                       # Cluster management API
│   └── Dockerfile                 # Controller container image
├── nodes/                         # Worker node code (Pi Zero 2W / Gaunts)
│   ├── tasks/                     # Node-specific execution logic
│   │   ├── main.go                # Node entry point
│   │   └── Dockerfile             # Node container image
├── shared/                        # Shared utilities and libraries
│   ├── config/                    # Cluster-wide configuration
│   ├── utils.go                   # Helper functions
│   └── protocols.go               # gRPC communication definitions
├── manifests/                     # Kubernetes manifests
│   ├── controller-deployment.yaml
│   ├── node-deployment.yaml
│   └── namespace.yaml
├── scripts/                       # Deployment and cluster management scripts
│   ├── deploy.sh
│   └── join-cluster.sh
├── test/                          # Testing suites
│   ├── unit/
│   ├── integration/
│   ├── cluster/
│   ├── e2e/
│   └── load/
├── README.md
└── .gitignore
```

## Hardware Setup

- **1 × Raspberry Pi 4B** — control plane node  
- **4 × Raspberry Pi Zero 2W** — worker nodes  
- **1 × Cluster Hat v2.5**

## Technologies Used

- **Go** for cluster services and node logic  
- **Docker** for packaging services  
- **K3s** for a lightweight Kubernetes distribution  
- **gRPC** for communication between controllers and nodes  
- **RabbitMQ** for async messaging and task coordination  
- **SQLite** for lightweight persistent storage  
- **Redis** for caching and fast state access  
- **InfluxDB** for metrics and time-series data  
- **Apache ECharts** for visualizing cluster metrics  

## Usage

Piranid is mainly a playground for experimenting with Kubernetes internals, microservices, and distributed workloads on constrained hardware. I use it to test scheduling behavior, service communication patterns, and monitoring setups without relying entirely on cloud infrastructure.

The project is intentionally flexible and continues to evolve as I add new services, improve automation, or experiment with cloud and IoT integrations.
