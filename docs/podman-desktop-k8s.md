# Podman Desktop & Kubernetes Integration Guide

This guide details how to leverage **Podman Desktop** to showcase the DevOps, Kubernetes, and distributed microservices architecture of the RideShare platform.

---

## Architecture at a Glance in Podman Desktop

```
┌────────────────────────────────────────────────────────────────────────┐
│                          PODMAN DESKTOP UI                             │
├───────────────────┬────────────────────────────────────────────────────┤
│ 📦 Images         │ Multi-stage distroless images (< 22 MB, non-root)  │
│ 🚀 Pods           │ Kubernetes pods grouped by service with live stats │
│ ☸️  Kubernetes     │ Deployments, Services, ConfigMaps, Secrets, Ingress│
│ 📊 Observability  │ Prometheus (:9090) + Jaeger (:16686)               │
└───────────────────┴────────────────────────────────────────────────────┘
```

The platform consists of **7 Go microservices** communicating over **gRPC**, decoupled through **NATS pub/sub**, storing state in embedded **libSQL (SQLite)** engines, and serving real-time UI via **Server-Sent Events (SSE)**.

---

## 2 Ways to Run in Podman Desktop

Podman Desktop gives you two deployment modes depending on your workflow:

### Mode 1: Native "Play Kubernetes YAML" (Lightweight & Fast)
Podman has native support for reading Kubernetes YAML directly and spinning up Linux pods without needing a heavy VM or full Kubernetes control plane.

1. **Build images**:
   ```bash
   task podman:build
   ```
2. **Play Kubernetes YAML in Podman Desktop**:
   - In Podman Desktop, navigate to **Pods** in the left sidebar.
   - Click the **"Play Kubernetes YAML"** button in the top right.
   - Select `deploy/k8s/rideshare-stack.yaml`.
   - Click **Play**.
   *(Alternatively from your terminal: `task podman:play`)*
3. **Open Web UI**:
   - In Podman Desktop's **Pods** list, click on `api-gateway` port `8080`, or visit:
   - **http://localhost:8080**

To stop and clean up:
```bash
task podman:down
```

---

### Mode 2: Full Kubernetes Cluster (Kind inside Podman Desktop)
For a complete Kubernetes experience showcasing Deployments, Services, Health Probes, and Rolling Updates:

1. **Enable Kind in Podman Desktop**:
   - Open Podman Desktop **Settings** (gear icon bottom left).
   - Go to **Resources** → find **Kind**.
   - Click **Create new...** → Name: `kind-cluster` → Click **Create**.
   - Podman Desktop will spin up a local Kubernetes cluster and set your current context automatically. A new **Kubernetes** section will appear in the sidebar!
2. **Build and Load Images into the Cluster**:
   ```bash
   task podman:build
   task k8s:kind:load
   ```
   *(This loads your local `rideshare/*` images directly into Kind so Kubernetes doesn't try to pull them from Docker Hub).*
3. **Deploy the Stack**:
   ```bash
   task k8s:apply
   ```
4. **Inspect in Podman Desktop**:
   - In the sidebar, click **Kubernetes** → explore **Deployments**, **Pods**, and **Services**.
   - Click the port link for `api-gateway` (or forward port `8080`).

---

## Demonstrating DevOps & Microservices Features

Here is a checklist of demonstrations you can present directly in Podman Desktop:

### 1. Distroless Secure Container Images
- **Location in UI:** **Images** tab.
- **What to show:**
  - Filter by `rideshare/`. Notice the image sizes are **under 22 MB**.
  - Click on any image (e.g., `rideshare/ride-service:latest`) → look at **Inspect** / **Layers**.
  - Highlight that binaries are statically compiled (`CGO_ENABLED=0`), stripped of debug symbols (`-ldflags="-s -w"`), and run on `gcr.io/distroless/static:nonroot` under `uid 65532` without shell or root privileges for maximum container security.

### 2. Kubernetes Probes & Health Checks
- **Location in UI:** **Kubernetes** → **Pods** tab.
- **What to show:**
  - Look at the pod statuses. Notice all 6 backend services show healthy/ready status.
  - In `deploy/k8s/user-service.yaml`, show the native **gRPC liveness and readiness probes**:
    ```yaml
    livenessProbe:
      grpc:
        port: 50051
    readinessProbe:
      grpc:
        port: 50051
    ```
  - For `api-gateway`, show the HTTP health probe on `/healthz`.

### 3. Self-Healing & Resilience Test
- **Location in UI:** **Kubernetes** → **Pods** tab.
- **Demonstration:**
  1. Find one of the running backend pods (e.g., `payment-service`).
  2. Click the **Delete** (trash) icon to simulate a node or pod crash.
  3. Watch the Kubernetes ReplicaSet detect the failure and immediately spin up a new pod within 1–2 seconds.
  4. Notice the web application remains uninterrupted because of gRPC client retry and circuit resilience!

### 4. Zero-Downtime Scaling
- **Location in UI:** **Kubernetes** → **Deployments** tab.
- **Demonstration:**
  1. Click on `ride-service`.
  2. Increase the replica count from `1` to `3`.
  3. Switch to the **Pods** tab and watch Kubernetes schedule and initialize 2 additional instances with green readiness probes.
  4. Explain how stateless microservices scale horizontally behind Kubernetes ClusterIP services.

### 5. ConfigMaps & Secrets Decoupling
- **Location in UI:** **Kubernetes** → **ConfigMaps & Secrets**.
- **What to show:**
  - Inspect `rideshare-config` (ConfigMap): contains service discovery DNS names (e.g. `location-service:50053`, `nats:4222`).
  - Inspect `rideshare-secrets` (Secret): holds the `JWT_SECRET` key, decoupled from code.
  - Explain 12-factor application configuration principles.

### 6. Observability (Prometheus & Distributed Tracing)
- **Location in UI:** **Kubernetes** → **Services** / **Pods**.
- **Demonstration:**
  - In `deploy/k8s/observability.yaml`, Prometheus is deployed to scrape all 7 services.
  - Click on the Prometheus service port `:9090` to open Prometheus:
    - Query `grpc_server_handled_total` or `http_requests_total`.
  - Click on the Jaeger service port `:16686` to open Jaeger:
    - Search for traces across `api-gateway` → `ride-service` → `location-service` to show distributed trace context propagation via W3C TraceContext headers.

### 7. Interactive In-Container Shell (Exec)
- **Location in UI:** Click any Pod → **Terminal** tab.
- **Demonstration:**
  - Select the `nats` pod → **Terminal** tab.
  - Run `nats-server -v` or inspect the monitoring endpoint at `http://localhost:8222/varz`.

---

## Quick Reference Commands

| Task | Command | Purpose |
|------|---------|---------|
| Build images | `task podman:build` | Build all 7 microservice images with Podman |
| Play Kube | `task podman:play` | Run the Kubernetes stack natively in Podman |
| Stop Play Kube | `task podman:down` | Stop and clean up Podman pods |
| Load to Kind | `task k8s:kind:load` | Pre-load local images into Podman Desktop's Kind cluster |
| Deploy K8s | `task k8s:apply` | Deploy full stack to Kubernetes via Kustomize |
| Delete K8s | `task k8s:delete` | Tear down Kubernetes deployment |
| Check K8s | `task k8s:status` | Check pods, services, and ingress |
