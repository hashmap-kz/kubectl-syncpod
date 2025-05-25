## 🛠️ Usage

```
mkdir -p backups
go run main.go download --namespace pgrwl-test --pvc distroless-data --mount-path=/tmp . backups
go run main.go download --namespace pgrwl-test --pvc postgres-data --mount-path=/var/lib/postgresql/data pgdata backups

go run main.go download --namespace vault --pvc postgresql --mount-path=/var/lib/postgresql/data pgdata backups
go run main.go download --namespace mon --pvc storage-victoriametrics --mount-path=/victoria-metrics-data . backups
```

## 🔍 Comparison Table

| Feature                                   | `kubectl cp`                    | `kubectl exec`          | `kubectl-syncpod`                |
|-------------------------------------------|---------------------------------|-------------------------|----------------------------------|
| Uses sidecar or helper pod                | ❌                               | ❌                       | ✅                                |
| Works with PVCs                           | ⚠️ Only if mounted in container | ⚠️ Manual path required | ✅ Injects helper pod with volume |
| Requires tools in container (`tar`, `sh`) | ✅                               | ✅                       | ❌ (runs tools in helper pod)     |
| Supports readOnlyRootFilesystem pods      | ❌                               | ❌                       | ✅                                |
| Works on `distroless`/`scratch` images    | ❌                               | ❌                       | ✅                                |
| Affects main application container        | ✅                               | ✅                       | ❌                                |
| Requires container to run as root         | Often yes                       | Often yes               | ❌ (helper pod runs separately)   |
| Safe for production workloads             | ⚠️ Risky                        | ⚠️ Risky                | ✅                                |
| Auto-cleans after sync                    | ❌                               | ❌                       | ✅ (optional)                     |

### 🚀 When to Use This Plugin

Use kubectl-syncpod instead of kubectl cp or kubectl exec when:

- Your main pod has restricted permissions or runs with readOnlyRootFilesystem
- Your containers are minimal (distroless, scratch, etc.)
- You want to sync to a volume (PVC) rather than the container FS
- You need a safe way to upload or download large files without modifying your workload
