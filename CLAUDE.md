# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is an AI Storage Cluster Orchestrator for Kubernetes that implements optimized pod migration using Persistent Volumes. The system is based on a research paper focused on container optimization during migration - it identifies and excludes completed containers, reducing CPU usage by 50% and memory by 40%.

**Key Concept**: Unlike standard Kubernetes pod migration that moves all containers, this orchestrator analyzes container states (waiting/running/completed) and only migrates containers that are actually running, using PV-based checkpoints for state preservation.

## Architecture

The system follows a three-layer request flow:

1. **HTTP API Layer** (`pkg/apis/handler.go`): Gin-based REST API that validates requests and delegates to the controller
2. **Controller Layer** (`pkg/controller/migration.go`): Orchestrates the migration workflow, manages job state with `sync.RWMutex`, and tracks metrics
3. **Kubernetes Integration Layer** (`pkg/k8s/client.go`): Interacts with Kubernetes API and metrics API for all cluster operations

**Migration Pipeline** (3-step process from research paper):
1. Capture container states and collect resource metrics
2. Create checkpoint in PersistentVolumeClaim (if `preserve_pv: true`)
3. Create optimized pod with only running containers, wait for Ready state, delete original pod

State is managed in-memory using `map[string]*MigrationJob` protected by `sync.RWMutex`. Each migration gets a UUID-based ID and runs in a goroutine with context-based timeout control.

## Development Commands

### Build & Deploy
```bash
# Build Docker image and Go binary
./scripts/build.sh [tag]  # default: latest
# - Builds CGO_ENABLED=0 static binary
# - Creates Docker image
# - Imports to containerd (k8s.io namespace)

# Deploy to Kubernetes
./scripts/deploy.sh [tag]
# - Checks cluster connectivity
# - Labels nodes with layer=orchestration
# - Applies deployments/cluster-orchestrator.yaml
# - Waits for rollout with 300s timeout

# Build components separately
go build -o main ./cmd/main.go  # Build binary only
docker build -t ai-storage-orchestrator:latest .  # Build image only
```

### Testing & Validation
```bash
# Run unit tests
go test ./pkg/...

# View deployment logs
kubectl logs -n kube-system -l app=ai-storage-orchestrator -f

# Check deployment status
kubectl get pods -n kube-system -l app=ai-storage-orchestrator

# Port forward for local testing
kubectl port-forward -n kube-system svc/ai-storage-orchestrator 8080:8080

# Test health endpoint
curl http://localhost:8080/health

# Test migration API
curl -X POST http://localhost:8080/api/v1/migrations \
  -H "Content-Type: application/json" \
  -d '{
    "pod_name": "example-pod",
    "pod_namespace": "default",
    "source_node": "worker-1",
    "target_node": "worker-2",
    "preserve_pv": true,
    "timeout": 600
  }'

# Check migration status
curl http://localhost:8080/api/v1/migrations/{migration-id}

# View performance metrics
curl http://localhost:8080/api/v1/metrics
```

### Dependencies
```bash
# Update dependencies
go mod tidy
go mod download

# Required: Go 1.21+, Kubernetes 1.25+, kubectl, Docker
```

## Key Implementation Details

### Container State Analysis (`pkg/k8s/client.go:64-105`)
The `GetPodContainerStates()` function determines which containers to migrate:
- **waiting**: `ShouldMigrate = false` - not yet started
- **running**: `ShouldMigrate = true` - actively executing
- **completed** (exit code 0): `ShouldMigrate = false` - already finished
- **failed** (non-zero exit): `ShouldMigrate = true` - retry on target node

This is the core optimization that reduces resource usage.

### PersistentVolumeClaim Checkpoints (`pkg/k8s/client.go:107-132`)
When `preserve_pv: true`, creates a PVC named `checkpoint-{podname}-{timestamp}`:
- Default size: 1Gi (configurable in controller)
- AccessMode: ReadWriteOnce
- Labels: `app=ai-storage-orchestrator`, `component=migration-checkpoint`
- Mounted at `/migration-checkpoint` in new pod containers

### Optimized Pod Creation (`pkg/k8s/client.go:143-199`)
- Creates new pod with name `{original-name}-migrated-{timestamp}`
- Filters `Spec.Containers` to only include containers where `ShouldMigrate == true`
- Sets `Spec.NodeName` to target node (bypasses scheduler)
- Adds labels: `migration.ai-storage/original-pod`, `migration.ai-storage/target-node`
- Mounts checkpoint PVC if provided

### Metrics Collection (`pkg/k8s/client.go:201-223`)
Uses `metrics.k8s.io/v1beta1` API to get actual CPU/memory usage:
- CPU in millicores, converted to cores (divide by 1000)
- Memory in bytes
- Aggregates across all containers in pod
- Falls back to simulated values (50% CPU, 60% memory) if metrics API unavailable

### Migration Job Lifecycle (`pkg/controller/migration.go:103-159`)
Each migration runs in a goroutine with these stages:
1. Status → Running
2. `captureContainerStates()` - analyze original pod
3. `createCheckpoint()` - optional PVC creation
4. `createOptimizedPod()` - create new pod, wait for Ready (5min timeout)
5. `deleteOriginalPod()` - graceful deletion (30s grace period)
6. `collectPostMigrationMetrics()` - wait 30s, collect new pod metrics
7. Status → Completed/Failed, update global metrics

Errors in steps 5-6 log warnings but don't fail the migration.

### API Validation (`pkg/apis/handler.go:153-175`)
Request validation enforces:
- All fields required except `preserve_pv`, `force_restart`, `timeout`
- `source_node` ≠ `target_node`
- `timeout` must be non-negative
- Default timeout: 600 seconds if not specified

## File Structure

```
cmd/main.go                    - Entry point, initializes k8s client → controller → HTTP server
pkg/apis/handler.go            - Gin routes and request validation
pkg/controller/migration.go    - Migration orchestration logic and state management
pkg/k8s/client.go              - Kubernetes API operations (pods, PVCs, metrics)
pkg/types/migration.go         - Type definitions for requests/responses/metrics
deployments/cluster-orchestrator.yaml - K8s Deployment, Service, RBAC manifests
scripts/build.sh               - Build automation
scripts/deploy.sh              - Deployment automation
```

## Common Development Tasks

### Adding a New Migration Step
1. Add step function in `pkg/controller/migration.go` (e.g., `myNewStep(job *MigrationJob)`)
2. Call it in `executeMigration()` pipeline in the appropriate order
3. Update `MigrationDetails` in `pkg/types/migration.go` if new data needs to be tracked
4. Error handling: use `mc.failMigration(job, message)` to abort or log warning to continue

### Modifying Container State Logic
Edit `GetPodContainerStates()` in `pkg/k8s/client.go:64-105`. The `ShouldMigrate` boolean controls which containers are copied to the optimized pod.

### Changing Metrics Collection
- Original metrics: `captureContainerStates()` in `pkg/controller/migration.go:161-205`
- Optimized metrics: `collectPostMigrationMetrics()` in `pkg/controller/migration.go:268-304`
- Actual collection logic: `GetPodMetrics()` in `pkg/k8s/client.go:201-223`

### Adding API Endpoints
1. Define route in `SetupRoutes()` in `pkg/apis/handler.go:26-47`
2. Add handler function following pattern of existing handlers
3. Use `migrationController` methods to interact with state

## Important Notes

- **No Database**: All state is in-memory. Restarting the orchestrator loses migration history.
- **RBAC Required**: The pod needs permissions for pods (get, create, delete), PVCs (create), and metrics (get). See `deployments/cluster-orchestrator.yaml`.
- **Node Labels**: The deployment uses `nodeSelector: layer: orchestration`. Ensure at least one node has this label.
- **Metrics API**: Requires `metrics-server` deployed in cluster. Without it, metrics fall back to simulated values.
- **ImagePullPolicy**: Set to `Never` in deployment - image must be in containerd via `build.sh`.
- **Graceful Shutdown**: Main server listens for SIGINT/SIGTERM but in-flight migrations may be interrupted.
- **Timeout Context**: Each migration has its own context with timeout. Exceeding it stops the migration goroutine.

## Performance Targets

From research paper (K8s baseline = 100%):
- CPU usage: 50% (50% reduction)
- Memory usage: 60% (40% reduction)
- Cold start time: 50% (50% reduction via PV checkpoints)

These are measured by comparing `OriginalResources` vs `OptimizedResources` in `MigrationDetails`.
