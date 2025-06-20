# Kronos Scheduler – Quick KIND Setup

> Kronos runs as an **in-process Scheduler Framework plugin** (no HTTP extender). See the official docs for background:
> • Scheduling-framework: https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/
> • Scheduler-plugins:    https://kubernetes.io/docs/reference/scheduling/config/#scheduling-plugins

## Minimal command flow

```bash
# build plugin scheduler image
docker build -t kronos-scheduler:latest .

# start a local KIND cluster
kind create cluster --name kronos

# load the image into the cluster
kind load docker-image kronos-scheduler:latest --name kronos

# install Kronos (RBAC + config + deployment)
kubectl apply -f manifests/rbac.yaml
kubectl apply -f manifests/scheduler-config.yaml
kubectl apply -f manifests/kronos-kube-scheduler-deployment.yaml

# wait for scheduler to be ready
kubectl -n kube-system rollout status deploy/kronos-kube-scheduler

# smoke-test workload handled by Kronos
kubectl apply -f manifests/test-deployment.yaml
```

## Verify it’s working

```bash
# view scheduler logs – should mention EnergyAware plugin
kubectl logs -n kube-system deploy/kronos-kube-scheduler | grep EnergyAware

# watch test pods schedule
kubectl get pods -l app=test -o wide -w
```

That’s it—only the six `kubectl apply`/`kind`/`docker build` commands above are required. All manifests live under `manifests/` and include:
- `rbac.yaml` – ServiceAccount & ClusterRoleBinding
- `scheduler-config.yaml` – ConfigMap with plugin-enabled `KubeSchedulerConfiguration`
- `kronos-kube-scheduler-deployment.yaml` – Deployment running the custom scheduler
- `test-deployment.yaml` – Example workload (`schedulerName: kronos-scheduler`).

This guide explains how to build and deploy the Kronos Scheduler as a Kubernetes Scheduler Plugin in a KIND cluster, including all prerequisites and verification steps.

## Table of Contents
- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Detailed Steps](#detailed-steps)
  - [1. Create a KIND Cluster](#1-create-a-kind-cluster)
  - [2. Build the Scheduler Image](#2-build-the-scheduler-image)
  - [3. Load Image into KIND](#3-load-image-into-kind)
  - [4. Deploy RBAC and Configuration](#4-deploy-rbac-and-configuration)
  - [5. Deploy the Custom Scheduler](#5-deploy-the-custom-scheduler)
  - [6. Verify Installation](#6-verify-installation)
  - [7. Test Scheduling](#7-test-scheduling)
- [Troubleshooting](#troubleshooting)
- [Cleanup](#cleanup)
- [Architecture](#architecture)
- [References](#references)

## What is this?

`kronos-scheduler` is **not** an HTTP extender any more—it is a native [Scheduler Framework Plugin](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/).  The plugin is built into a custom `kube-scheduler` binary and runs **in-process**, giving you all extension points with zero network overhead.

If you want to understand why plugins are preferred, see the Kubernetes docs:
- Scheduling Framework overview  ➜ https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/
- Scheduler Plugins reference     ➜ https://kubernetes.io/docs/reference/scheduling/config/#scheduling-plugins

## TL;DR – Run it

Kronos Scheduler is implemented as a [Kubernetes Scheduler Framework Plugin](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/), which allows for custom scheduling logic to be injected directly into the Kubernetes scheduler. This approach is more performant and reliable than the older HTTP extender pattern.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) installed and running
- [kubectl](https://kubernetes.io/docs/tasks/tools/) installed
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/) (Kubernetes IN Docker) installed
- [Go](https://golang.org/dl/) 1.24 or later



```bash
# build the custom scheduler image (binary + plugin)
docker build -t kronos-scheduler:latest .

# create a local KIND cluster
a=$(mktemp); cat > $a <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
EOF
kind create cluster --name kronos --config=$a
rm $a

# load image into the cluster
kind load docker-image kronos-scheduler:latest --name kronos

# install RBAC, config, and scheduler deployment
kubectl apply -f manifests/rbac.yaml
kubectl apply -f manifests/scheduler-config.yaml
kubectl apply -f manifests/kronos-kube-scheduler-deployment.yaml

# wait for scheduler pod
kubectl -n kube-system rollout status deploy/kronos-kube-scheduler

# smoke-test with sample workload
kubectl apply -f manifests/test-deployment.yaml
kubectl get pods -w -l app=test
```

## Files you apply
- `manifests/rbac.yaml`              ServiceAccount + ClusterRoleBinding
- `manifests/scheduler-config.yaml`  `KubeSchedulerConfiguration` with `EnergyAware` plugin enabled
- `manifests/kronos-kube-scheduler-deployment.yaml` single-container deployment that runs the custom scheduler
- `manifests/test-deployment.yaml`   Example workload (`schedulerName: kronos-scheduler`) used for verification

No other files are needed—everything else in the repo is build artefacts or legacy.

## Verify

```bash
# logs should show the plugin registering
kubectl logs -n kube-system -l component=kronos-kube-scheduler -f | grep EnergyAware

# check that test pods are scheduled on nodes
kubectl get pod -l app=test -o wide
```

If the pods are Running and the scheduler logs show `EnergyAware` filter/score calls, the plugin is working.

### 1. Create a KIND Cluster

Create a KIND cluster with the following configuration:

```bash
cat > kind-config.yaml <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30000
    hostPort: 30000
    protocol: TCP
  - containerPort: 30001
    hostPort: 30001
    protocol: TCP
EOF

kind create cluster --name kronos --config=kind-config.yaml
```

### 2. Build the Scheduler Image

Build the scheduler Docker image:

```bash
docker build -t kronos-scheduler:latest .
```

### 3. Load Image into KIND

Load the image into the KIND cluster:

```bash
kind load docker-image kronos-scheduler:latest --name kronos
```

### 4. Deploy RBAC and Configuration

Apply the RBAC and scheduler configuration:

```bash
kubectl apply -f manifests/rbac.yaml
kubectl apply -f manifests/configmap.yaml
```

### 5. Deploy the Custom Scheduler

Deploy the Kronos scheduler:

```bash
kubectl apply -f manifests/deployment.yaml
```

### 6. Verify Installation

Check that the scheduler pod is running:

```bash
kubectl get pods -n kube-system -l component=kronos-kube-scheduler
```

View scheduler logs:

```bash
kubectl logs -n kube-system -l component=kronos-kube-scheduler -f
```

### 7. Test Scheduling

Create a test pod that uses the custom scheduler:

```yaml
cat > test-pod.yaml <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  schedulerName: kronos-scheduler
  containers:
  - name: pause
    image: registry.k8s.io/pause:3.9
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 100m
        memory: 128Mi
  nodeSelector:
    kubernetes.io/arch: amd64
EOF

kubectl apply -f test-pod.yaml
```

Verify the pod was scheduled by Kronos:

```bash
kubectl get pod test-pod -o wide
kubectl describe pod test-pod | grep -i scheduler
```

## Troubleshooting

### Scheduler Not Starting
- Check logs: `kubectl logs -n kube-system -l component=kronos-kube-scheduler`
- Verify image was loaded: `docker exec -it kronos-control-plane crictl images | grep kronos`

### Pods Stuck in Pending
- Check events: `kubectl get events --sort-by=.metadata.creationTimestamp`
- Verify scheduler name matches: `kubectl get pod <pod> -o jsonpath='{.spec.schedulerName}'`

### Permission Issues
- Check RBAC: `kubectl describe clusterrole kronos-scheduler`
- Check service account: `kubectl get sa -n kube-system kronos-scheduler`

## Cleanup

Delete the KIND cluster when done:

```bash
kind delete cluster --name kronos
```

## Architecture

### Components
1. **Scheduler Plugin**
   - Implements `Filter`, `Score`, and `ScoreExtensions` interfaces
   - Runs in-process with kube-scheduler
   - No HTTP server required

2. **Configuration**
   - Uses `KubeSchedulerConfiguration` for plugin setup
   - ConfigMap stores the scheduler config

3. **RBAC**
   - ServiceAccount with necessary permissions
   - ClusterRole and ClusterRoleBinding for scheduler operations

### How It Works
1. The custom scheduler is deployed alongside the default scheduler
2. Pods with `schedulerName: kronos-scheduler` are handled by our plugin
3. The plugin's `Filter` and `Score` methods are called during scheduling
4. Scheduling decisions are made based on energy efficiency and queue metrics

## References

1. [Kubernetes Scheduler Framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)
2. [Scheduling Framework KEP](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/624-scheduling-framework)
3. [Scheduler Plugins](https://kubernetes.io/docs/reference/scheduling/config/#scheduling-plugins)
4. [Extending the Kubernetes Scheduler](https://kubernetes.io/docs/concepts/configuration/scheduling-framework/)
5. [Scheduler Configuration](https://kubernetes.io/docs/reference/scheduling/config/)

## Changes from HTTP Extender Version

### Removed
- HTTP server and endpoints
- Fiber web framework
- Extender configuration
- Separate extender deployment and service

### Added
- Scheduler Framework plugin implementation
- In-process scheduling hooks
- Simplified deployment (single container)
- Better integration with kube-scheduler

### Benefits
- **Performance**: No network overhead
- **Reliability**: No separate service to monitor
- **Simplicity**: Single binary deployment
- **Features**: Access to all scheduler extension points

## Next Steps
- Add more sophisticated scheduling policies
- Implement additional scheduler extension points
- Add metrics and monitoring
- Set up CI/CD for the scheduler image



PS C:\Users\abhin> kubectl -n kube-system rollout status deploy/kronos-kube-scheduler --timeout=60s
E0620 17:55:28.483339   16432 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://127.0.0.1:60329/api?timeout=32s\": dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it."
E0620 17:55:28.485091   16432 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://127.0.0.1:60329/api?timeout=32s\": dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it."
E0620 17:55:28.487792   16432 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://127.0.0.1:60329/api?timeout=32s\": dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it."
E0620 17:55:28.489409   16432 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://127.0.0.1:60329/api?timeout=32s\": dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it."
Unable to connect to the server: dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it.
PS C:\Users\abhin>


PS C:\Users\abhin> kubectl logs -n kube-system -l component=kronos-kube-scheduler -f
E0620 17:56:02.530810   24956 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://127.0.0.1:60329/api?timeout=32s\": dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it."
E0620 17:56:02.537077   24956 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://127.0.0.1:60329/api?timeout=32s\": dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it."
E0620 17:56:02.540108   24956 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://127.0.0.1:60329/api?timeout=32s\": dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it."
E0620 17:56:02.541667   24956 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://127.0.0.1:60329/api?timeout=32s\": dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it."
Unable to connect to the server: dial tcp 127.0.0.1:60329: connectex: No connection could be made because the target machine actively refused it.
PS C:\Users\abhin>
