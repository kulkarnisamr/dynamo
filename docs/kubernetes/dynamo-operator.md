---
# SPDX-FileCopyrightText: Copyright (c) 2025-2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0
title: Dynamo Operator
subtitle: Reference for the Dynamo Kubernetes operator covering its controllers, deployment modes, and reconciliation workflow.
---

## Overview

Dynamo operator is a Kubernetes operator that simplifies the deployment, configuration, and lifecycle management of DynamoGraphs. It automates the reconciliation of custom resources to ensure your desired state is always achieved. This operator is ideal for users who want to manage complex deployments using declarative YAML definitions and Kubernetes-native tooling.

## Architecture

- **Operator Deployment:**
  Deployed as a Kubernetes `Deployment` in a specific namespace.

- **Controllers:**
  - `DynamoGraphDeploymentController`: Watches `DynamoGraphDeployment` CRs and orchestrates graph deployments.
  - `DynamoComponentDeploymentController`: Watches `DynamoComponentDeployment` CRs and handles individual component deployments.
  - `DynamoGraphDeploymentRequestController`: Watches `DynamoGraphDeploymentRequest` CRs and runs the profiling/generation flow that produces a `DynamoGraphDeployment`.
  - `DynamoGraphDeploymentScalingAdapterController`: Watches scaling adapter CRs used by external autoscalers and Planner-driven scaling flows.
  - `DynamoModelController`: Watches `DynamoModel` CRs and manages model lifecycle (e.g., loading LoRA adapters).
  - `DynamoCheckpointController`: Watches `DynamoCheckpoint` CRs for GPU worker checkpoint/restore workflows.

- **Workflow:**
  1. A custom resource is created by the user or API server.
  2. The corresponding controller detects the change and runs reconciliation.
  3. Kubernetes resources (Deployments, Services, etc.) are created or updated to match the CR spec.
  4. Status fields are updated to reflect the current state.

## Deployment Modes

The Dynamo operator has one supported production mode and two development/test configurations:

### 1. Cluster-Wide Mode (Default, Recommended)

The operator monitors and manages DynamoGraph resources across **all namespaces** in the cluster.

**When to Use:**

- You have full cluster admin access
- You want centralized management of all Dynamo workloads
- Standard production deployment on a dedicated cluster

---

### 2. Namespace-Scoped Mode (Development and Testing Only)

> [!WARNING]
> Namespace-scoped mode (`namespaceRestriction.enabled=true`) is not supported for production. Use it only for development and testing.

The operator monitors and manages DynamoGraph resources **only in a specific namespace**. A Lease claim makes the cluster-wide reconciler stand down there and publishes the namespaced operator's effective feature gates. The cluster-wide operator continues to serve all admission and conversion webhooks.

**When to Use:**

- You want to test a new operator version in isolation
- You are developing controller behavior in one namespace

**Installation:**

```bash
helm install dynamo-platform dynamo-platform-${RELEASE_VERSION}.tgz \
  --namespace my-namespace \
  --create-namespace \
  --skip-crds \
  --set dynamo-operator.namespaceRestriction.enabled=true \
  --set dynamo-operator.upgradeCRD=false
```

The namespaced operator requires a cluster-wide operator of the same or a newer version
that supports Lease-based feature gates. It does not create or serve admission, conversion,
or defaulting webhooks and does not manage webhook certificates.

---

### 3. Cluster-Wide Plus Namespace-Scoped Mode (Development and Testing Only)

> [!WARNING]
> This configuration is not supported for production. Use a single cluster-wide operator in production.

A **cluster-wide operator** manages most namespaces in a development cluster, while **one or more namespace-scoped operators** run in specific namespaces for testing. The cluster-wide operator automatically detects and excludes namespaces with namespace-scoped operators using lease markers.

**When to Use:**

- Testing new operator versions in isolated namespaces on a development cluster
- Developing or testing controller feature gates in one namespace

**How It Works:**

1. Namespace-scoped operator creates a lease named `dynamo-operator-namespace-scope` in its namespace
2. Cluster-wide operator watches for these lease markers across all namespaces
3. Cluster-wide operator excludes reconciliation for any namespace with a lease marker
4. Cluster-wide admission applies the feature-gate snapshot from that namespace's Lease,
   including validation and feature-dependent mutating admission such as defaulting
5. If the namespace-scoped operator stops, its lease expires and cluster-wide reconciliation resumes

> [!CAUTION]
> Always pass `--skip-crds` and set `dynamo-operator.upgradeCRD=false` for a namespaced operator.
> Helm installs the chart's `crds/` directory before rendering templates, so the chart cannot detect
> a missing `--skip-crds` flag.

Checkpoint restore mutation uses the namespace's `checkpoint` gate. Checkpoint storage
and seccomp settings still come from the cluster-wide operator, so namespaced checkpoint
tests must use compatible cluster-wide configuration.

**Setup Example:**

```bash
# 1. Install the cluster-wide operator in a development cluster
helm install dynamo-platform dynamo-platform-${RELEASE_VERSION}.tgz \
  --namespace dynamo-system \
  --create-namespace

# 2. Install namespace-scoped operator (testing, v2.0.0-beta)
helm install dynamo-test dynamo-platform-${RELEASE_VERSION}.tgz \
  --namespace test-namespace \
  --create-namespace \
  --skip-crds \
  --set dynamo-operator.namespaceRestriction.enabled=true \
  --set dynamo-operator.upgradeCRD=false \
  --set dynamo-operator.controllerManager.manager.image.tag=v2.0.0-beta
```

Every released namespaced operator requires a cluster-wide operator of the same or a newer
version that ships the newest APIs in the cluster. A 1.3 namespaced operator is not
supported with a 1.2 cluster-wide operator. For controller development, newer namespaced
code may run if it remains compatible with the cluster-wide CRDs and global webhooks.

**Observability:**

```bash
# List all namespaces with local operators
kubectl get lease -A --field-selector metadata.name=dynamo-operator-namespace-scope

# Check which operator version is running in a namespace
kubectl get lease -n my-namespace dynamo-operator-namespace-scope \
  -o jsonpath='{.spec.holderIdentity}'
```


## Custom Resource Definitions (CRDs)

Dynamo installs the following Custom Resources. The main deployment path is:
create or generate a `DynamoGraphDeployment`, then let the operator create the
lower-level resources that run it.

| Custom Resource | What it represents | Typical use |
|---|---|---|
| `DynamoGraphDeployment` (DGD) | The canonical live deployment for a Dynamo inference graph. | Author directly, apply a tuned recipe, or let DGDR generate it. |
| `DynamoGraphDeploymentRequest` (DGDR) | A deploy-by-intent request that profiles a model/hardware target and generates a DGD. | Start here when you want Dynamo to choose sizing, parallelism, or Planner-enabled generated config. |
| `DynamoComponentDeployment` (DCD) | Per-component deployments created from a DGD, such as frontend, router, prefill, decode, and planner components. | Usually inspected for debugging rather than authored directly. |
| `DynamoModel` | Model and adapter lifecycle management layered onto a running deployment. | Load, unload, or manage model artifacts such as LoRA adapters. |
| `DynamoCheckpoint` | Checkpoint metadata and job configuration for snapshotting GPU workers. | Use with Snapshotting GPU Workers to restore warm workers faster than cold start. |

Advanced and operator-owned resources:

- `DynamoGraphDeploymentScalingAdapter`: scaling interface used by Planner or external autoscalers to adjust component replicas.
- `DynamoWorkerMetadata`: discovery metadata written for worker pods.

For the complete technical API reference for Dynamo Custom Resource Definitions, see:

**📖 [Dynamo CRD API Reference](./api-reference.md)**

For user-focused workflows, see:

- **[Deployment Overview](./model-deployment-guide.md)** for DGD, DCD, DGDR, and recipes
- **[DGDR Reference](./dgdr.md)** for deploy-by-intent generated deployments
- **[Managing Models with DynamoModel Guide](./deployment/dynamomodel-guide.md)**
- **[Snapshotting GPU Workers](./snapshot.md)** for `DynamoCheckpoint`

## Webhooks

The Dynamo Operator uses **Kubernetes admission webhooks** for real-time validation and mutation of custom resources before they are persisted to the cluster. Webhooks are a required component of the operator and ensure that invalid configurations are rejected immediately at the API server level.

**Key Features:**
- ✅ Shared certificate infrastructure across all webhook types
- ✅ Automatic certificate generation and rotation (default, all environments)
- ✅ cert-manager integration (optional, for custom PKI)
- ✅ Immutability enforcement for critical fields

For complete documentation on webhooks, certificate management, and troubleshooting, see:

**📖 [Webhooks Guide](./webhooks.md)**

## Observability

The Dynamo Operator provides comprehensive observability through Prometheus metrics and Grafana dashboards. This allows you to monitor:

- **Controller Performance**: Reconciliation loop duration, success rates, and error rates by resource type
- **Webhook Activity**: Validation performance, admission rates, and denial patterns
- **Resource Inventory**: Current count of managed resources by state and namespace
- **Operational Health**: Success rates and health indicators for controllers and webhooks

### Metrics Collection

Metrics are automatically exposed on the operator's `/metrics` endpoint (port 8443 by default) and collected by Prometheus via a ServiceMonitor. The ServiceMonitor is automatically created when you install the operator via Helm (controlled by `metricsService.enabled`, which defaults to `true`).

### Grafana Dashboard

A pre-built Grafana dashboard is available for visualizing operator metrics. The dashboard includes:

- **Reconciliation Metrics**: Rate, duration (P95), and errors by resource type
- **Webhook Metrics**: Request rate, duration (P95), and denials by resource type and operation
- **Resource Inventory**: Count of DynamoGraphDeployments by state and namespace
- **Operational Health**: Success rate gauges for controllers and webhooks

For complete setup instructions and metrics reference, see:

**📖 [Operator Metrics Guide](./observability/operator-metrics.md)**

## Installation

### Quick Install with Helm

```bash
# Set environment
export NAMESPACE=dynamo-system
export RELEASE_VERSION=0.x.x # any version of Dynamo 0.3.2+ listed at https://github.com/ai-dynamo/dynamo/releases

# Install Platform (includes operator)
helm fetch https://helm.ngc.nvidia.com/nvidia/ai-dynamo/charts/dynamo-platform-${RELEASE_VERSION}.tgz
helm install dynamo-platform dynamo-platform-${RELEASE_VERSION}.tgz --namespace ${NAMESPACE} --create-namespace
```

> [!NOTE]
> Namespace-scoped configurations are only for development and testing and are not supported for production. See [Deployment Modes](#deployment-modes).

### Building from Source

```bash
# Set environment
export NAMESPACE=dynamo-system
export DOCKER_SERVER=your-registry.com/  # your container registry
export IMAGE_TAG=latest

# Build operator image
cd deploy/operator
docker build -t $DOCKER_SERVER/kubernetes-operator:$IMAGE_TAG \
  --build-context snapshot=../snapshot \
  --build-arg DOCKER_PROXY="" \
  .
docker push $DOCKER_SERVER/kubernetes-operator:$IMAGE_TAG
cd -

# Install platform with custom operator image (CRDs are automatically installed by the chart)
cd deploy/helm/charts
helm install dynamo-platform ./platform/ \
  --namespace ${NAMESPACE} \
  --create-namespace \
  --set "dynamo-operator.controllerManager.manager.image.repository=${DOCKER_SERVER}/kubernetes-operator" \
  --set "dynamo-operator.controllerManager.manager.image.tag=${IMAGE_TAG}" \
  --set dynamo-operator.imagePullSecrets[0].name=docker-imagepullsecret
```

For detailed installation options, see the [Installation Guide](./installation-guide.md)


## Development

- **Code Structure:**

The operator is built using Kubebuilder and the operator-sdk, with the following structure:

- `controllers/`: Reconciliation logic
- `api/v1alpha1/`: CRD types
- `config/`: Manifests and Helm charts


## References

- [Kubernetes Operator Pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- [Custom Resource Definitions](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
- [Operator SDK](https://sdk.operatorframework.io/)
- [Helm Best Practices for CRDs](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/)
