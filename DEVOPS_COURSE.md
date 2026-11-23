# Cloud-Native Microservices & Modern DevOps Engineering: Production Masterclass

An industry-grade, hands-on curriculum and reference laboratory based on the high-concurrency **RideShare Platform** (`ms`). This course guides students and engineers from container primitives to production Kubernetes orchestration, zero-trust security, distributed observability, resilience engineering, and GitOps automation.

---

## Course Overview & Real-World Lab Environment

```
                                  [ INTERNET / CLIENTS ]
                                             │
                       ┌─────────────────────▼─────────────────────┐
                       │   Contour (Envoy) Ingress Controller     │
                       │   - Port 9093 / 9443                      │
                       │   - Infinite SSE Response Timeout         │
                       └─────────────────────┬─────────────────────┘
                                             │ HTTP/SSE
                                  ┌──────────▼──────────┐
                                  │     API Gateway     │
                                  │ (HPA 1-5 | PDB = 1) │
                                  └──────────┬──────────┘
                         gRPC (HTTP/2)       │       Events (Pub/Sub)
            ┌───────────────────┬────────────┴────────────┬───────────────────┐
            ▼                   ▼                         ▼                   ▼
    ┌───────────────┐   ┌───────────────┐         ┌───────────────┐   ┌───────────────┐
    │ User Service  │   │ Ride Service  │◄───────►│  NATS Broker  │◄─►│Driver Service │
    │ (:50051)      │   │ (HPA 1-5)     │         │ (:4222)       │   │ (:50052)      │
    └───────┬───────┘   └───────┬───────┘         └───────────────┘   └───────┬───────┘
            │                   │                                             │
    ┌───────▼───────┐   ┌───────▼───────┐                             ┌───────▼───────┐
    │SQLite (libSQL)│   │SQLite (libSQL)│                             │Location Svc   │
    │(WAL Mode)     │   │(Surge Engine) │                             │ (:50053)      │
    └───────────────┘   └───────────────┘                             └───────────────┘
```

* **Core Stack**: Go 1.24, gRPC/Protobuf, NATS Core, libSQL (embedded SQLite in WAL mode), Leaflet + Datastar (Hypermedia SSE).
* **DevOps Stack**: Podman Desktop, Kind (Kubernetes in Docker/Podman), Contour Ingress (Envoy), Kustomize, Prometheus, Jaeger, Grafana, Taskfile.
* **Security & Reliability**: Distroless non-root containers (`uid 65532`), Zero-Trust NetworkPolicies, PodDisruptionBudgets, HorizontalPodAutoscalers, PersistentVolumeClaims, Jobs, and CronJobs.

---

## Course Curriculum: 10 Comprehensive Modules

### Module 1: Modern Container Runtime & Distroless Engineering
* **Theory**:
  * The OCI Specification, container namespaces (pid, net, ipc, mnt, user), and cgroups v2.
  * Docker daemon architecture vs daemonless, rootless container execution with **Podman**.
  * Attack surface minimization: why production workloads must never run as root or ship package managers/shells.
* **Architecture Implementation**:
  * Multi-stage build patterns: Go compilation in `golang:alpine` stage; final binary dropped into `gcr.io/distroless/static:nonroot`.
  * Stripping symbols and disabling CGO: `CGO_ENABLED=0 go build -ldflags="-s -w"`.
  * Enforcing `USER nonroot:nonroot` (`uid 65532:65532`).
  * Microservice image sizes reduced to **< 22 MB** with zero OS vulnerabilities (Zero-CVE posture).
* **Reference Artifact**:
  * [services/user-service/Dockerfile](file:///home/user/Projects/ms/services/user-service/Dockerfile)

---

### Module 2: Local Kubernetes Orchestration & Declarative Management
* **Theory**:
  * The Kubernetes Control Plane: API Server, etcd, Controller Manager, Kubelet, and CRI (`containerd`).
  * Local Kubernetes options: Kind (Kubernetes in Docker/Podman) vs Minikube vs Cloud K8s (EKS/GKE).
  * Package management & configuration drift: **Kustomize** (native overlay-based declarative engine) vs Helm.
* **Architecture Implementation**:
  * Automated Kind cluster configuration inside Podman Desktop with custom port mapping (`9093:80`, `9443:443`).
  * Pre-loading locally compiled distroless image archives directly into Kind containerd using `task k8s:kind:load`.
  * Clean resource management via [deploy/k8s/kustomization.yaml](file:///home/user/Projects/ms/deploy/k8s/kustomization.yaml).
* **DevOps Command**:
  ```bash
  task k8s:up      # All-in-one build, load, deploy, probe wait, and smoke-test
  ```

---

### Module 3: Advanced Microservice Ingress & Protocol Routing
* **Theory**:
  * Service topologies: `ClusterIP` vs `NodePort` vs `LoadBalancer`.
  * Edge routing with Envoy-based Ingress Controllers (**Project Contour**).
  * Long-lived HTTP streaming connections: Server-Sent Events (SSE) and WebSocket timeout considerations.
* **Architecture Implementation**:
  * Datastar SSE event streaming over Contour Ingress.
  * Configuring `projectcontour.io/response-timeout: "infinity"` in [deploy/k8s/ingress.yaml](file:///home/user/Projects/ms/deploy/k8s/ingress.yaml) to prevent Envoy from severing persistent location and ride-matching streams.
  * gRPC multiplexing over HTTP/2 between internal services via Kubernetes ClusterIP DNS (`user-service.rideshare.svc.cluster.local:50051`).

---

### Module 4: Zero-Trust Security & Network Segmentation
* **Theory**:
  * The Zero-Trust model in microservices: "Never trust, always verify".
  * Default-deny network architectures vs open pod-to-pod flat networks.
  * Decoupling secrets from application code (12-Factor App rules).
* **Architecture Implementation**:
  * Declarative Kubernetes `NetworkPolicy` rules in [deploy/k8s/networkpolicy.yaml](file:///home/user/Projects/ms/deploy/k8s/networkpolicy.yaml):
    * `allow-ingress-to-gateway`: Restricts external HTTP entry solely to `api-gateway`.
    * `backend-services-policy`: Blocks all direct external traffic to backend gRPC services; allows ingress only from `api-gateway`, `ride-service` (saga orchestrator), `prometheus` (scraper), and the `smoke-test` job.
    * `nats-message-broker-policy`: Restricts pub/sub ports (`:4222`, `:8222`) strictly to intra-cluster workloads.
  * Kubernetes `Secret` isolation for `JWT_SECRET` in [deploy/k8s/secret.yaml](file:///home/user/Projects/ms/deploy/k8s/secret.yaml).

---

### Module 5: Resilience Engineering, Health Probes & High Availability
* **Theory**:
  * Health checking protocols: HTTP `/healthz` vs native gRPC Health Checking Protocol (`grpc.health.v1.Health`).
  * Pod eviction guarantees: handling voluntary disruptions (node maintenance, kernel upgrades) without dropping requests.
  * Application-layer resilience: Token-bucket rate limiting, circuit breakers, and distributed concurrency controls.
* **Architecture Implementation**:
  * Native **gRPC Liveness and Readiness Probes** in service manifests:
    ```yaml
    livenessProbe:
      grpc:
        port: 50051
      initialDelaySeconds: 5
      periodSeconds: 10
    readinessProbe:
      grpc:
        port: 50051
      initialDelaySeconds: 2
      periodSeconds: 5
    ```
  * **PodDisruptionBudgets (PDB)** in [deploy/k8s/pdb.yaml](file:///home/user/Projects/ms/deploy/k8s/pdb.yaml) guaranteeing `minAvailable: 1` for critical microservices during maintenance.
  * **HorizontalPodAutoscalers (HPA)** in [deploy/k8s/hpa.yaml](file:///home/user/Projects/ms/deploy/k8s/hpa.yaml) dynamically scaling `api-gateway` and `ride-service` from 1 to 5 replicas based on CPU load.
  * **Resilience Suite** in [shared/resilience/](file:///home/user/Projects/ms/shared/resilience):
    * Distributed claim locking (`MemoryLocker`) preventing concurrent double-matching of drivers.
    * Circuit Breaker state machine (Closed/Open/Half-Open) isolating failing downstream dependencies.
    * Per-client token-bucket rate limiter middleware.

---

### Module 6: Cloud-Native Workload Typology: Deployments, Jobs & CronJobs
* **Theory**:
  * Understanding workload lifecycle semantics:
    * `Deployment` / `ReplicaSet`: Continuous, long-lived, declarative desired-state management.
    * `Job`: Finite, run-to-completion batch execution (must terminate with exit code 0).
    * `CronJob`: Time-triggered, automated execution based on standard cron syntax.
* **Architecture Implementation**:
  * **Deployments**: 10 continuously running services across the cluster.
  * **Kubernetes Job** ([deploy/k8s/job-smoke-test.yaml](file:///home/user/Projects/ms/deploy/k8s/job-smoke-test.yaml)):
    * Automated post-deployment verification job (`rideshare-smoke-test`) that spins up an alpine container, audits gateway routes, queries surge endpoints, verifies NATS socket connectivity, and validates Prometheus health.
  * **Kubernetes CronJob** ([deploy/k8s/cronjob-maintenance.yaml](file:///home/user/Projects/ms/deploy/k8s/cronjob-maintenance.yaml)):
    * Scheduled background reconciliation worker running every 10 minutes (`*/10 * * * *`) with `concurrencyPolicy: Forbid` to clean stale driver pings and reconcile orphaned ride requests.

---

### Module 7: Storage Architecture: Ephemeral vs Persistent Disks
* **Theory**:
  * Container file systems: OverlayFS writable layers vs Ephemeral `emptyDir` vs Stateful `PersistentVolumeClaim` (PVC).
  * StorageClasses and dynamic storage provisioners (`rancher.io/local-path`).
  * SQLite in production: WAL (Write-Ahead Logging) mode, busy timeouts, and disk persistence considerations.
* **Architecture Implementation**:
  * Ephemeral scratch volumes (`emptyDir: {}`) for temporary operational buffers.
  * **PersistentVolumeClaim (PVC)** in [deploy/k8s/pvc.yaml](file:///home/user/Projects/ms/deploy/k8s/pvc.yaml):
    * Dynamically provisioned 1Gi volume bound to Prometheus TSDB storage path `/prometheus` ensuring observability time-series data survives pod restarts and cluster rollouts.

---

### Module 8: Full-Stack SRE & Distributed Observability
* **Theory**:
  * The Three Pillars of Observability: Metrics, Distributed Traces, and Structured Logs.
  * OpenTelemetry (OTel) instrumentation and W3C TraceContext header propagation across asynchronous boundaries.
  * The RED Method: **Rate** (req/sec), **Errors** (failed req/sec), and **Duration** (latency distribution).
* **Architecture Implementation**:
  * Distributed tracing via **Jaeger** (`:16686`): every HTTP request to the gateway propagates an OTel span context through gRPC metadata across `ride-service`, `location-service`, and `payment-service`.
  * Metrics collection via **Prometheus** (`:9090`): scrapes custom gRPC metrics (`grpc_server_handled_total`, `grpc_server_handling_seconds`) and custom business metrics.
  * Real-time **Operations Command Center** (`/ops`): Live WebSocket/SSE telemetry feed with dynamic hexagonal surge heatmaps.
  * Pre-provisioned **Grafana** dashboard in [deploy/observability/grafana/](file:///home/user/Projects/ms/deploy/observability/grafana/).

---

### Module 9: CI/CD Automation & GitOps Pipelines
* **Theory**:
  * Continuous Integration best practices: static analysis, contract linting, matrix testing, and multi-architecture container builds.
  * Infrastructure-as-Code (IaC) verification: validating manifests against the Kubernetes OpenAPI schema before pushing.
* **Architecture Implementation**:
  * Multi-stage GitHub Actions pipeline in [.github/workflows/ci.yml](file:///home/user/Projects/ms/.github/workflows/ci.yml):
    * **Job 1 (Test & Vet)**: Protobuf linting, `go work sync`, `task vet`, and multi-module unit test execution.
    * **Job 2 (Compose Validate)**: Validates full compose and observability definitions.
    * **Job 3 (Container Matrix Build)**: Parallelized multi-service image compilation with Docker Buildx.
  * Taskfile-driven automation providing single-command developer ergonomics (`task --list`).

---

### Module 10: Chaos Engineering & Failure Drills
* **Theory**:
  * Principles of Chaos Engineering: hypothesizing steady state, injecting real-world faults, and verifying automatic recovery.
  * Simulating pod crashes, message broker downtime, and unexpected traffic spikes.
* **Architecture Implementation**:
  * Autonomous fleet traffic generator: [services/simulator/cmd/simulator/main.go](file:///home/user/Projects/ms/services/simulator/cmd/simulator/main.go) simulating 20+ concurrent drivers navigating dynamic coordinates and matching rides.
  * Automated chaos fault injection task (`task k8s:chaos`) that forcibly kills active service pods and verifies zero downtime.

---

## 10 Hands-on Practical Lab Assignments

Follow these hands-on labs using the project workspace:

### Lab 1: Distroless Container Inspection
1. Build the images: `task podman:build`
2. Inspect the image size: `podman images | grep rideshare` (verify all are < 23 MB).
3. Attempt to run a shell in the container:
   ```bash
   podman run --rm -it rideshare/user-service:latest /bin/sh
   ```
   *Expected result*: Command fails with `executable file not found in $PATH` — proving the container has no shell and cannot be exploited via shell injection.

---

### Lab 2: Deploying the Full Stack to Kubernetes
1. Ensure your Kind cluster is active in Podman Desktop (`kind-cluster`).
2. Run the all-in-one deployment command:
   ```bash
   task k8s:up
   ```
3. Watch as Podman tags the images, loads them into Kind containerd, applies Kustomize manifests, waits for health checks, runs the smoke-test job, and prints the access matrix.

---

### Lab 3: Exploring Resources in Podman Desktop
1. Open **Podman Desktop**.
2. Click the **☸️ Kubernetes** icon in the left sidebar.
3. Verify that all 10 resource categories are active:
   * **Deployments**: 10/10 active
   * **Pods**: 11 active (10 microservices + 1 completed job)
   * **Services**: 10 active
   * **Persistent Volume Claims**: 1 bound (`prometheus-storage`)
   * **Jobs**: 1 completed (`rideshare-smoke-test`)
   * **CronJobs**: 1 active (`rideshare-maintenance`)

---

### Lab 4: End-to-End Smoke Test Verification
1. Manually re-trigger the post-deployment smoke test Job:
   ```bash
   task k8s:smoke-test
   ```
2. Inspect the output verifying that HTTP gateway, Surge API, Operations Center, Prometheus, and NATS are all reporting green.

---

### Lab 5: Observing Scheduled CronJob Execution
1. View active CronJobs:
   ```bash
   kubectl -n rideshare get cronjob
   ```
2. Manually trigger an immediate execution of the maintenance cronjob:
   ```bash
   task k8s:cron:trigger
   ```
3. Check the completed job pods:
   ```bash
   kubectl -n rideshare get pods -l app.kubernetes.io/name=maintenance-worker
   ```

---

### Lab 6: Verifying Zero-Trust Network Policies
1. Inspect the network policies:
   ```bash
   kubectl -n rideshare get networkpolicy
   ```
2. Verify that `backend-services-policy` restricts ingress to `driver-service`, `location-service`, etc., allowing access only from the authorized API Gateway and Saga orchestrator.

---

### Lab 7: Chaos Engineering (Self-Healing Pod Kill Drill)
1. In a split terminal, run:
   ```bash
   task k8s:chaos
   ```
2. Observe the script terminate an active pod (e.g., `ride-service`).
3. Notice that the ReplicaSet immediately spawns a replacement pod in `< 1.5 seconds`.
4. Check the application in your browser (`http://localhost:9093`) to confirm zero service interruption.

---

### Lab 8: Inspecting Distributed Trace Waterfalls in Jaeger
1. Start port-forwarding for observability tools:
   ```bash
   task k8s:forward:obs
   ```
2. Open Jaeger in your browser: [http://localhost:16686](http://localhost:16686).
3. Select `api-gateway` from the Service dropdown and click **Find Traces**.
4. Open a trace to inspect the distributed span hierarchy:
   `api-gateway: HTTP POST /rides` ──► `ride-service: gRPC RequestRide` ──► `location-service: gRPC GetNearbyDrivers`.

---

### Lab 9: Querying RED Metrics in Prometheus
1. With `task k8s:forward:obs` running, open Prometheus: [http://localhost:9090](http://localhost:9090).
2. Query gRPC throughput:
   ```promql
   sum(rate(grpc_server_handled_total[1m])) by (grpc_service)
   ```
3. Query 99th percentile latency:
   ```promql
   histogram_quantile(0.99, sum(rate(grpc_server_handling_seconds_bucket[5m])) by (le))
   ```

---

### Lab 10: High-Concurrency Traffic Simulation & Dynamic Surge
1. Start the autonomous fleet simulator:
   ```bash
   task simulate
   ```
2. Open the Operations Command Center: [http://localhost:9093/ops](http://localhost:9093/ops).
3. Toggle the **Surge Heatmap** button:
   * Observe high request density in discrete hexagonal cells triggering the Surge Engine.
   * Watch pricing multipliers scale dynamically from $1.0\times$ up to $2.5\times$ in real time.

---

### Lab 11: GitOps Continuous Delivery & Self-Healing with Argo CD
1. Install Argo CD control plane:
   ```bash
   task gitops:install
   ```
2. Register the GitOps application:
   ```bash
   task gitops:apply
   ```
3. Open the Argo CD visual dashboard:
   ```bash
   task gitops:ui
   ```
   *(Navigate to `https://localhost:8081` and log in with user `admin` and printed secret).*
4. Run the live GitOps drift and self-healing demonstration:
   ```bash
   task gitops:drift:demo
   ```
   *Watch Argo CD detect manual replica drift and immediately self-heal the cluster back to the Git source of truth!*

---

## Command Reference Cheat Sheet

| Command | Purpose |
|---|---|
| `task k8s:up` | **Complete all-in-one deployment pipeline** |
| `task k8s:status` | Print full Kubernetes resource status table |
| `task k8s:smoke-test` | Run automated in-cluster health audit Job |
| `task k8s:cron:trigger` | Force an immediate run of maintenance CronJob |
| `task k8s:chaos` | Execute pod kill drill to prove self-healing |
| `task k8s:forward:obs` | Port-forward Prometheus (:9090) and Jaeger (:16686) |
| `task k8s:logs -- <svc>` | Tail live logs for a specific service in K8s |
| `task gitops:install` | Install Argo CD control plane into cluster |
| `task gitops:apply` | Register GitOps AppProject and Application |
| `task gitops:ui` | Open Argo CD Web UI at https://localhost:8081 |
| `task gitops:drift:demo` | Demonstrate GitOps drift detection & auto-healing |
| `task simulate` | Run 20 autonomous drivers with live GPS telemetry |
| `task test` | Run all Go unit and integration tests |
| `task vet` | Run static analysis (`go vet`) across all modules |
