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
- **API Gateway:** Routes client requests to the appropriate services.
- **User Authentication Service:** Handles user registration, login, and token management.
- **Data Storage Service:** Manages data persistence using a lightweight database.
- **Caching Service:** Implements distributed caching for performance optimization.
- **File Management Service:** Handles file uploads, storage, and retrieval.
- **Task Scheduling Service:** Manages background jobs and recurring tasks.
- **Logging & Monitoring Service:** Centralizes log collection and system monitoring.

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
