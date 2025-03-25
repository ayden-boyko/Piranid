# Piranid: Cloud-Connected Kubernetes Cluster

## Overview

Piranid is a cloud-connected Kubernetes cluster project running on a fleet of Raspberry Pi devices to create a distributed computing environment. It integrates with major cloud providers for hybrid deployments and demonstrates expertise in container orchestration, microservices management, and cloud-native development. It features a lightweight control plane on a Raspberry Pi 4B (Norn) and multiple worker nodes (Gaunts) using Raspberry Pi Zero 2Ws. The system integrates with cloud services for monitoring, management, and scalability.

## Features (proposed)

- **Kubernetes-based cluster** with Raspberry Pi nodes
- **Microservices deployment** using Docker and Kubernetes
- **Automated CI/CD pipelines** for seamless updates
- **Monitoring** using a custom logging service
- **K3s-based Kubernetes deployment** for lightweight cluster management
- **Dynamic scalability** to add or remove worker nodes

## Repository Structure

``` plaintext
piranid/
├── docs/                          # All documents relating tot the project(system design, etc)
├── controllers/                   # Code for the control plane (Pi 4B, called Norn)
│   ├── main.go                    # Entry point for controller logic
│   ├── scheduler/                 # Scheduler logic
│   ├── api/                       # API server for cluster management
│   └── Dockerfile                 # Dockerfile for controller container image
├── nodes/                         # Code for worker nodes (Pi Zero 2Ws, called Gaunts)
│   ├── main.go                    # Entry point for node logic
│   ├── tasks/                     # Task execution logic
│   └── Dockerfile                 # Dockerfile for node container image
├── shared/                        # Shared libraries and utilities
│   ├── config/                    # Cluster-wide configuration files
│   ├── utils.go                   # Shared helper functions
│   └── protocols.go               # Communication protocols (e.g., gRPC)
├── manifests/                     # Kubernetes manifests for deployment
│   ├── controller-deployment.yaml # Deployment for the controller
│   ├── node-deployment.yaml       # Deployment for the worker nodes
│   └── namespace.yaml             # Namespace definition
├── scripts/                       # Deployment and management scripts
│   ├── deploy.sh                  # Script to deploy all components to K8s
│   └── join-cluster.sh            # Script to join worker nodes to the cluster
├── tests/                         # Testing suite for the system
│   ├── unit/                      # Unit tests for components
│   ├── integration/               # Integration tests between services
│   ├── e2e/                       # End-to-end tests for full system validation
│   ├── load/                      # Load and performance testing
├── README.md                      # Project documentation
└── .gitignore                     # Ignored files and directories
```

## Hardware Setup

- **1 x Raspberry Pi 4B** (Master Node)
- **4 x Raspberry Pi Zero 2W** (Worker Nodes)
- **1 x Cluster Hat v2.5**

## Technologies Used (proposed)

- **Go** for backend microservices
- **Docker**: For containerizing your microservices
- **K3s**: The lightweight Kubernetes distribution for orchestrating your containers/ Loadbalancer / Reverse Proxy
- **containerd**: The container runtime used by K3s
- **gRPC**: An efficient protocol for communication between microservices
- **A message queue like RabbitMQ**: For asynchronous communication between services
- **SQLite**: a sql database for data storage
- **Redis**: a caching service
- **TSDB**: a time series DB for logs (Prometheus or InfluxDB)

## Usage

Piranid serves as a scalable, cloud-ready platform for distributed computing experiments, hybrid cloud integration, and Kubernetes learning. The project is designed to be extensible and can be expanded with additional cloud services or IoT integrations.
