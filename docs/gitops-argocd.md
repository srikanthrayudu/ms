# GitOps Continuous Delivery with Argo CD & Kubernetes

This guide details the **GitOps Continuous Delivery architecture** of the RideShare Platform using **Argo CD**, **Kustomize**, and **Kubernetes**.

---

## 1. What is GitOps & Why It Matters

GitOps is an operational framework that takes DevOps best practices used for application development (version control, collaboration, compliance, and CI/CD) and applies them to infrastructure automation.

```
┌─────────────────┐        Git Commit        ┌─────────────────┐
│ Developer / CI  │ ───────────────────────► │  Git Repository │
└─────────────────┘                          │ (Single Source  │
                                             │    of Truth)    │
                                             └────────┬────────┘
                                                      │
                       ┌──────────────────────────────┘
                       │ Continuous Monitoring (Polling / Webhook)
                       ▼
         ┌───────────────────────────┐
         │     Argo CD Controller    │
         │  (Inside Kubernetes Node) │
         └─────────────┬─────────────┘
                       │ Automated Sync & Self-Healing
                       ▼
         ┌───────────────────────────┐
         │ Kubernetes Cluster State  │
         │ (rideshare Namespace)     │
         └───────────────────────────┘
```

### Core Principles Demonstrated:
1. **Declarative State**: The entire desired system state (Deployments, HPA, PDB, Services, Ingress, NetworkPolicies, Secrets, PVCs, Jobs, CronJobs) is declared in Git (`deploy/k8s/`).
2. **Versioned & Immutable Source of Truth**: Any change to production is made via a Git commit or Pull Request—never through manual `kubectl edit` or ad-hoc scripts.
3. **Automated Pull-Based Delivery**: An in-cluster operator (**Argo CD**) continuously pulls from the Git repository. Unlike traditional push-based CI/CD runners, cluster credentials never leave the Kubernetes cluster.
4. **Drift Detection & Continuous Self-Healing**: If an unauthorized user or disaster alters cluster state (e.g. scales a deployment to 0 or deletes a pod), Argo CD instantly detects the divergence and automatically resets the cluster to match Git.

---

## 2. GitOps Directory Structure

```text
deploy/gitops/
├── argocd/
│   ├── project.yaml          # Argo CD AppProject defining security boundaries and RBAC
│   ├── application.yaml      # Argo CD Application connecting Git repo -> 'rideshare' namespace
│   └── applicationset.yaml   # Enterprise ApplicationSet matrix generator (dev vs prod)
└── overlays/
    ├── dev/
    │   └── kustomization.yaml # Dev environment: debug logging, 1 replica
    └── prod/
        └── kustomization.yaml # Prod environment: info logging, 3 replicas, strict limits
```

---

## 3. Argo CD Manifests Breakdown

### A. The AppProject (`deploy/gitops/argocd/project.yaml`)
Enforces multi-tenant isolation, whitelisting only authorized Git repositories and destination namespaces:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: rideshare-project
  namespace: argocd
spec:
  sourceRepos:
    - "https://github.com/srikanthrayudu/ms.git"
    - "*"
  destinations:
    - namespace: rideshare
      server: https://kubernetes.default.svc
```

### B. The Application (`deploy/gitops/argocd/application.yaml`)
Defines the synchronization contract, self-healing, and automated pruning:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: rideshare-platform
  namespace: argocd
spec:
  project: rideshare-project
  source:
    repoURL: https://github.com/srikanthrayudu/ms.git
    targetRevision: HEAD
    path: deploy/k8s
  destination:
    server: https://kubernetes.default.svc
    namespace: rideshare
  syncPolicy:
    automated:
      prune: true     # Prunes resources removed from Git
      selfHeal: true  # Reverts out-of-band manual cluster mutations
    syncOptions:
      - CreateNamespace=true
```

### C. The ApplicationSet (`deploy/gitops/argocd/applicationset.yaml`)
Demonstrates the enterprise multi-cluster and multi-environment GitOps pattern, using a list generator to dynamically generate `rideshare-dev` and `rideshare-prod` from Kustomize overlays.

---

## 4. Hands-On GitOps Workflows with Taskfile

### Step 1: Install Argo CD on Kind
```bash
task gitops:install
```
*Creates the `argocd` namespace and installs the official Argo CD control plane (API Server, Repo Server, Controller, Dex).*

### Step 2: Apply GitOps Applications
```bash
task gitops:apply
```
*Registers the `rideshare-project` and `rideshare-platform` Application with the Argo CD controller.*

### Step 3: Open the Argo CD Dashboard
```bash
task gitops:ui
```
*Port-forwards the Argo CD Web UI to `https://localhost:8081` and prints the auto-generated `admin` password.*

### Step 4: Live Drift & Self-Healing Demonstration
To demonstrate GitOps self-healing in an interview or demo:
```bash
task gitops:drift:demo
```
*This command manually tampers with the cluster (e.g., sets replicas of `ride-service` to 0). Within seconds, Argo CD detects the drift, flags the status as `OutOfSync`, and immediately self-heals it back to the Git state!*

---

## 5. Push-Based vs. Pull-Based (GitOps) Comparison

| Feature | Traditional Push CI/CD (e.g. Jenkins/GitHub Actions) | Modern Pull GitOps (Argo CD) |
|---|---|---|
| **Cluster Access** | CI runner requires admin `kubeconfig` stored as a secret | In-cluster agent pulls from Git; no external credentials needed |
| **Drift Detection** | None (only knows what it pushed last) | Continuous real-time detection against live API server |
| **Self-Healing** | Manual re-run required | Automatic within seconds |
| **Disaster Recovery** | Complex rebuild scripts | Point Argo CD at Git repo on a new cluster; 100% recovered |
| **Audit Trail** | CI build logs | Git commit log (`git log`) is the legal audit trail |
