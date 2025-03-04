# Piranid: Cloud-Connected Kubernetes Cluster

## Overview

Piranid is a cloud-connected Kubernetes cluster project running on a fleet of Raspberry Pi devices to create a distributed computing environment. It integrates with major cloud providers for hybrid deployments and demonstrates competency in container orchestration, microservices management, and cloud-native development. It features a lightweight control plane on a Raspberry Pi 4B and multiple worker nodes using Raspberry Pi Zero 2Ws. The system integrates with cloud services for monitoring, management, and scalability.

## Features (proposed)

- **Kubernetes-based cluster** with Raspberry Pi nodes
- **Hybrid cloud integration** with AWS, Google Cloud, or Azure
- **Microservices deployment** using Docker and Kubernetes
- **Automated CI/CD pipelines** for seamless updates
- **Monitoring and scaling** with Prometheus and Grafana
- **K3s-based Kubernetes deployment** for lightweight cluster management
- **Azure Arc connectivity** for cloud-based cluster management
- **Dynamic scalability** to add or remove worker nodes
- **Azure Monitor integration** for cluster observability
- **Edge processing** for local workload execution before cloud aggregation

## Repository Structure

```
piranid/
├── controllers/                   # Code for the control plane (Pi 4B)
│   ├── main.go                    # Entry point for controller logic
│   ├── scheduler/                 # Scheduler logic
│   ├── api/                       # API server for cluster management
│   └── Dockerfile                 # Dockerfile for controller container image
├── nodes/                         # Code for worker nodes (Pi Zero 2Ws)
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
├── nginx/                         # Reverse proxy configuration
│   ├── Dockerfile                 # Dockerfile for Nginx container image
│   ├── nginx.conf                 # Nginx configuration file
├── README.md                      # Project documentation
└── .gitignore                     # Ignored files and directories
```

## Hardware Setup

- **1 x Raspberry Pi 4B** (Master Node)
- **4 x Raspberry Pi Zero 2W** (Worker Nodes)
- **1 x Cluster Hat v2.5**

## Technologies Used

- **Kubernetes** (K3s for lightweight deployment)
- **Docker** for containerization
- **Go** for backend microservices
- ~~**Prometheus & Grafana** for monitoring~~ (not yet)
- **Nginx** for load balancing

## Usage

Piranid serves as a scalable, cloud-ready platform for distributed computing experiments, hybrid cloud integration, and Kubernetes learning. The project is designed to be extensible and can be expanded with additional cloud services or IoT integrations.

## License

**MIT License**

This project provides a robust, cost-effective Kubernetes cluster using Raspberry Pi devices, ideal for learning and real-world distributed computing.

