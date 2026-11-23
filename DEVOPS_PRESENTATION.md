---
marp: true
theme: gaia
_class: lead
paginate: true
backgroundColor: #0f172a
color: #f8fafc
headingDivider: 2
style: |
  section {
    font-family: 'Inter', system-ui, -apple-system, sans-serif;
    padding: 40px;
  }
  h1 {
    color: #38bdf8;
  }
  h2 {
    color: #38bdf8;
    border-bottom: 2px solid #1e293b;
    padding-bottom: 8px;
  }
  h3 {
    color: #94a3b8;
  }
  strong {
    color: #38bdf8;
  }
  code {
    background-color: #1e293b;
    color: #38bdf8;
    font-size: 0.85em;
  }
  footer {
    font-size: 0.55em;
    color: #64748b;
  }
---

<!-- footer: RideShare Platform • Modern DevOps & Distributed Systems Architecture -->

# Cloud-Native Microservices & Modern DevOps Engineering
### End-to-End Production Architecture & GitOps Implementation

**Presenter:** DevOps & Cloud Engineering Lead
**Platform:** RideShare Distributed Architecture (`ms`)
**Stack:** Go • gRPC • NATS • Kubernetes • Podman Desktop • Argo CD • Prometheus • Jaeger

---

## Executive Summary: The Engineering Challenge

### The Problem
* Monolithic ride-hailing backends suffer from single points of failure, cascading crashes, and opaque latency bottlenecks.
* Traditional deployments rely on fragile manual scripts and heavy container images (>800 MB) burdened with security CVEs.

### The Solution: Cloud-Native Architecture
* **7 Go Microservices** communicating over high-throughput **gRPC (HTTP/2)**.
* Asynchronous event coordination through **NATS Core Pub/Sub**.
* **Zero-CVE Distroless Containers** running non-root (< 22 MB).
* **Enterprise Kubernetes Lifecycle**: HPA autoscaling, PDB high-availability, Zero-Trust NetworkPolicies, dynamic PVC storage, and automated CronJobs.
* **Declarative GitOps Delivery**: Argo CD continuous automated synchronization and self-healing.

---

## High-Level System Architecture

```
                       [ CLIENTS / WEB BROWSERS ]
                                   │
                     ┌─────────────▼─────────────┐
                     │ Contour (Envoy) Ingress   │ (Port 9093 / 9443)
                     │ - Infinite SSE Timeout    │
                     └─────────────┬─────────────┘
                                   │ HTTP/SSE
                     ┌─────────────▼─────────────┐
                     │        API Gateway        │
                     │ (HPA: 1-5 | PDB: min=1)   │
                     └──────┬─────────────┬──────┘
             gRPC (HTTP/2)  │             │  Events (Pub/Sub)
       ┌────────────────────┼─────────────┼────────────────────┐
       ▼                    ▼             ▼                    ▼
┌──────────────┐     ┌──────────────┐ ┌──────────────┐  ┌──────────────┐
│ User Service │     │ Ride Service │ │  NATS Core   │  │Driver Service│
│ (:50051)     │     │ (:50054)     │ │   Broker     │  │ (:50052)     │
└──────────────┘     └──────────────┘ └──────────────┘  └──────────────┘
```

* **API Gateway**: Single ingress edge proxy serving Datastar SSE hypermedia.
* **Storage**: Embedded libSQL (SQLite in WAL mode) per microservice for zero-dependency distributed operations.

---

## Containerization & Distroless Engineering

### Minimalist, Immutable, and Secure Containers

* **Multi-Stage Build**:
  * Build stage: Compiles statically linked Go binaries (`CGO_ENABLED=0 -ldflags="-s -w"`).
  * Runtime stage: Copied into `gcr.io/distroless/static:nonroot`.
* **Zero-CVE Security Posture**:
  * No package manager (`apt`, `apk`), no utilities (`curl`, `bash`), and **no shell**.
  * Prevents remote command injection attacks cold: arbitrary shell execution is physically impossible.
* **Principle of Least Privilege**:
  * Runs under strictly unprivileged `uid 65532:65532` (`nonroot`).
* **Footprint**:
  * Entire microservice images are **under 22 MB** (vs 800+ MB standard Docker images).

---

## Kubernetes Topology: Declarative Orchestration

### Podman Desktop + Kind Integration

* **Local Production Parity**:
  * Runs a multi-node Kubernetes cluster natively inside **Podman Desktop** using **Kind**.
  * Maps ingress ports (`9093:80`, `9443:443`) for local developer access.
* **Declarative Manifests with Kustomize**:
  * No template bloat: pure, standard Kubernetes YAML organized cleanly via Kustomize overlays.
  * Resources managed:
    * 10 Deployments & ClusterIP Services
    * 1 Contour (Envoy) Ingress with infinite streaming timeout
    * 1 PersistentVolumeClaim (1Gi dynamic storage)
    * 1 Verification Job & 1 Maintenance CronJob
    * 2 HPAs, 3 PDBs, and 3 Zero-Trust NetworkPolicies

---

## Advanced Ingress: Real-Time Event Streaming

### Envoy & Contour Ingress for Server-Sent Events (SSE)

* **The Problem with Standard Ingress**:
  * Traditional reverse proxies (NGINX/Envoy) have default 30–60 second read/write timeouts.
  * Real-time GPS driver tracking and live ride-matching streams get abruptly killed.
* **The Solution**:
  ```yaml
  apiVersion: networking.k8s.io/v1
  kind: Ingress
  metadata:
    name: rideshare-ingress
    annotations:
      projectcontour.io/response-timeout: "infinity"
  ```
* **Result**:
  * Seamless, uninterrupted live map telemetry rendered via Leaflet and Datastar hypermedia.

---

## Zero-Trust Security & Network Segmentation

### Micro-Segmentation with Kubernetes NetworkPolicies

* **Default-Deny Posture**:
  * Prevent lateral movement across the cluster if an edge container is ever compromised.

```
External Internet ──► [ Ingress / API Gateway ] (Port 8080)
                              │
               Allowed gRPC   ▼  Blocked from Internet
                     [ Backend Microservices ]
                     (driver, location, ride, payment)
                              │
               Allowed PubSub ▼  Blocked from Internet
                     [ NATS Message Broker ]
```

* **Secret Isolation**:
  * Decouples credentials (`JWT_SECRET`) from source code using Kubernetes Secrets.

---

## High Availability & Elastic Autoscaling

### Handling Traffic Spikes Gracefully

* **Horizontal Pod Autoscaling (HPA)**:
  * Automatically scales `api-gateway` and `ride-service` from **1 to 5 replicas** when CPU exceeds 75%.
  * Accommodates rush-hour ride matching bursts seamlessly.
* **PodDisruptionBudgets (PDB)**:
  * Guarantees `minAvailable: 1` across `api-gateway`, `ride-service`, and `nats`.
  * Protects quorum during rolling node upgrades and voluntary evictions.
* **Native gRPC Health Probes**:
  * Uses Kubernetes native gRPC health checking (`grpc.health.v1.Health`) on port `50051`.
  * Eliminates fragile custom HTTP wrapper endpoints or shell-based probe scripts.

---

## Cloud-Native Workload Typology

### Deployments vs. Jobs vs. CronJobs

| Workload Type | Object | Purpose in Platform |
|---|---|---|
| **Continuous** | `Deployment` | 10 continuously running backend microservices and message brokers. |
| **Run-to-Completion** | `Job` | `rideshare-smoke-test`: Automated post-deploy verification checking 5 core endpoints. |
| **Scheduled Batch** | `CronJob` | `rideshare-maintenance`: Runs every 10 min (`*/10 * * * *`) to purge stale driver GPS coordinates. |

* **Storage Architecture**:
  * Ephemeral scratch spaces (`emptyDir: {}`) for volatile operational data.
  * **PersistentVolumeClaim (PVC)**: 1Gi disk dynamically bound via `rancher.io/local-path` ensuring Prometheus metric history survives restarts.

---

## Full-Stack SRE & Observability

### Distributed Tracing, Metrics & Telemetry

* **OpenTelemetry (OTel)**:
  * Propagates W3C `traceparent` headers across gRPC calls and asynchronous NATS events.
* **Distributed Tracing with Jaeger**:
  * Full waterfall breakdown: Gateway HTTP Request ──► Ride Service ──► Location Service ──► Driver Matching.
* **Metrics Collection via Prometheus**:
  * Instrumenting RED Metrics: **Rate**, **Errors**, and **Duration** (latency percentiles $P_{50}$, $P_{95}$, $P_{99}$).
* **Operations Command Center (`/ops`)**:
  * Live dark-mode operations dispatch map with real-time NATS event ticker.

---

## Dynamic Surge Pricing & Spatial Indexing

### Discrete Hexagonal Spatial Indexing

* **The Problem**: Arbitrary GPS coordinate radiuses cause expensive $O(N)$ geospatial math on every ping.
* **The Solution: Hexagonal Spatial Cells**:
  * Discrete spatial cells with deterministic `CellID` and mathematical center coordinates.
  * Rolling 60-second window tracking driver supply vs ride demand per cell.
* **Dynamic Surge Multiplier**:
  $$\text{Surge} = \min\left(2.5, \, \max\left(1.0, \, 1.0 + \frac{\text{Demand} - 2 \cdot \text{Supply}}{10}\right)\right)$$
* **Visualization**:
  * Real-time SVG vector markers and color-coded polygons dynamically rendered on the dispatch map.

---

## GitOps Continuous Delivery: Argo CD

### Git as the Single Source of Truth

```
[ Developer / CI ] ──► [ Git Commit ] ──► [ Argo CD Controller ] ──► [ Kubernetes State ]
                                                   │
                                     Detects Cluster Tampering
                                                   │
                                                   ▼
                                       Automatic Self-Healing
```

* **Pull-Based Delivery**:
  * Argo CD operator runs in-cluster; zero cluster credentials exposed to external CI runners.
* **Automated Drift Detection & Self-Healing**:
  * If a deployment replica count is manually changed via `kubectl`, Argo CD detects the drift within seconds and automatically reverts the cluster to match Git.
* **Enterprise ApplicationSets**:
  * Multi-environment matrix generator managing `rideshare-dev` and `rideshare-prod` overlays.

---

## Chaos Engineering & Self-Healing Proof

### Testing Hypotheses Under Failure Conditions

* **Failure Scenario 1: Pod Crash**:
  * Terminating an active `ride-service` pod via `task k8s:chaos`.
  * *Result*: Kubernetes ReplicaSet replaces the pod in **< 1.5 seconds**; traffic routes seamlessly with zero dropped rides.
* **Failure Scenario 2: Network Latency / Downstream Failure**:
  * Token-bucket rate limiting rejects excessive traffic.
  * Circuit Breaker trips to `Open` state, preventing cascading resource exhaustion.
* **Autonomous Fleet Simulator**:
  * Generates 20 autonomous drivers emitting live GPS pings and synthetic rider requests.

---

## Developer Ergonomics: The Taskfile Engine

### Unified Single-Command Lifecycle Management

| Task Command | Execution Workflow |
|---|---|
| `task k8s:up` | **All-in-one deploy**: Builds images, loads Kind, applies manifests, waits for health, and runs smoke tests. |
| `task k8s:status` | Formatted matrix of Deployments, Pods, Services, PVC, Jobs, and CronJobs. |
| `task k8s:smoke-test` | Triggers the post-deploy in-cluster verification Job and streams logs. |
| `task k8s:chaos` | Simulates immediate pod failure to demonstrate self-healing. |
| `task gitops:drift:demo` | Demonstrates Argo CD detecting drift and auto-healing back to Git state. |
| `task simulate` | Runs the 20-driver autonomous traffic simulator. |

---

## Technical Proof & Verification SLIs

### Measurable Architecture Metrics

| Metric | Target | Achieved in Platform |
|---|---|---|
| **Container Size** | < 50 MB | **18 – 22.5 MB** (Distroless) |
| **Container CVEs** | 0 Critical / High | **0 CVEs** (No OS shell or package manager) |
| **Failover MTTR** | < 5 seconds | **< 1.8 seconds** (ReplicaSet resurrection) |
| **End-to-End Deploy** | < 2 minutes | **~35 seconds** (Cached Podman build + load) |
| **Distributed Tracing**| 100% Core Flows | **W3C TraceContext** across Gateway, gRPC & NATS |
| **GitOps Drift Healing**| Fully Automated | **Immediate** (Argo CD self-heal enabled) |

---

## Conclusion & Q&A

### Key Takeaways
1. **Modern DevOps is Multi-Dimensional**: True cloud-native systems require container hygiene, declarative orchestration, zero-trust security, and observability.
2. **GitOps is the Gold Standard**: Shifting cluster mutations to Git commits enables continuous drift detection, compliance, and instant disaster recovery.
3. **Resilience Must Be Layered**: Self-healing Kubernetes controllers + application circuit breakers + atomic distributed locks = bulletproof reliability.

### Live Demonstration & Codebase
* **Repository:** `https://github.com/srikanthrayudu/ms`
* **Local Web UI:** `http://localhost:9093`
* **Operations Command Center:** `http://localhost:9093/ops`

**Thank you! Open for Questions.**
