# kubectl-syncpod

_High-Speed File Transfer to and from Kubernetes PVCs_

[![License](https://img.shields.io/github/license/hashmap-kz/kubectl-syncpod)](https://github.com/hashmap-kz/kubectl-syncpod/blob/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/hashmap-kz/kubectl-syncpod)](https://goreportcard.com/report/github.com/hashmap-kz/kubectl-syncpod)
[![Workflow Status](https://img.shields.io/github/actions/workflow/status/hashmap-kz/kubectl-syncpod/ci.yml?branch=master)](https://github.com/hashmap-kz/kubectl-syncpod/actions/workflows/ci.yml?query=branch:master)
[![GitHub Issues](https://img.shields.io/github/issues/hashmap-kz/kubectl-syncpod)](https://github.com/hashmap-kz/kubectl-syncpod/issues)
[![Go Version](https://img.shields.io/github/go-mod/go-version/hashmap-kz/kubectl-syncpod)](https://github.com/hashmap-kz/kubectl-syncpod/blob/master/go.mod#L3)
[![Latest Release](https://img.shields.io/github/v/release/hashmap-kz/kubectl-syncpod)](https://github.com/hashmap-kz/kubectl-syncpod/releases/latest)

---

## About

- While `kubectl cp` and `kubectl exec` can both be used to copy files, performance degrades significantly when the
  target size is large (e.g., ~100Gi). In such cases, execution becomes drastically slow.

- Additionally, both approaches have limitations: they either require extra tools (such as `tar`) to be installed in the
  container, or they cannot be used at all with minimal base images like `distroless` or `scratch`.

- Most importantly, these methods do not support concurrent read/write operations - a critical limitation when
  performance and throughput matter.

### Features

- Upload local files or directories to a pod's mounted volume
- Download pod volume files back to local machine
- Safe overwrite protection
- Auto-rename existing remote directories (`pgdata-new-original-YYYY-MM-DD-HHMMSS`)
- Concurrent file transfer with worker pool
- Preserves directory structure
- Optional automatic `chown` of uploaded files inside the pod (via Kubernetes exec API)
- Fully based on **SFTP** + **Kubernetes Exec API** — no side effects on other pod processes


### Typical Use Cases

You’re running PostgreSQL as a StatefulSet, and you need to restore a database from a basebackup and a WAL archive.
If the volume is hostPath-based, this is relatively straightforward - you simply copy the required files onto the target
node.
But when using CSI-backed volumes (e.g., via a cloud provider), where the PVC is mounted as a block device, the
situation becomes more complex. In such cases, conventional tools fall short.

Also - you may want to scale your StatefulSet to zero and back up the PVC contents safely and efficiently - for local
testing, migration, or recovery.

Basic Scenarios:

- ✅ Download backup from PVC for verification / restore
- ✅ Sync files between PVCs and local environment
- ✅ Testing PVC mount behavior
- ✅ CI/CD pipelines to prepare volume data

### How It Works

`kubectl-syncpod` spins up a **temporary helper pod** that:

- Mounts your target PVC
- Runs an `sshd` server with an in-memory public key
- Listens on a randomized NodePort
- Accepts connections only via a secure, ephemeral SSH private key (never written to disk)

The CLI then:

- Uses an in-memory SFTP client to **recursively transfer files**
- Skips files that are **already present and match by SHA-256**
- Cleans up the helper pod and service automatically

---

## Installation

### Using `krew`

1. Install the [Krew](https://krew.sigs.k8s.io/docs/user-guide/setup/) plugin manager if you haven’t already.
2. Run the following command:

```bash
kubectl krew install syncpod
```

### Homebrew installation

```bash
brew tap hashmap-kz/homebrew-tap
brew install kubectl-syncpod
```

### Manual Installation

1. Download the latest binary for your platform from
   the [Releases page](https://github.com/hashmap-kz/kubectl-syncpod/releases).
2. Place the binary in your system's `PATH` (e.g., `/usr/local/bin`).

#### Example installation script for Unix-Based OS _(requirements: tar, curl, jq)_:

```bash
(
set -euo pipefail

OS="$(uname | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/\(arm\)\(64\)\?.*/\1\2/' -e 's/aarch64$/arm64/')"
TAG="$(curl -s https://api.github.com/repos/hashmap-kz/kubectl-syncpod/releases/latest | jq -r .tag_name)"

curl -L "https://github.com/hashmap-kz/kubectl-syncpod/releases/download/${TAG}/kubectl-syncpod_${TAG}_${OS}_${ARCH}.tar.gz" |
tar -xzf - -C /usr/local/bin && \
chmod +x /usr/local/bin/kubectl-syncpod
)
```

### Package-Based installation (suitable in CI/CD)

#### Debian

```
sudo apt update -y && sudo apt install -y curl
curl -LO https://github.com/hashmap-kz/kubectl-syncpod/releases/latest/download/kubectl-syncpod_linux_amd64.deb
sudo dpkg -i kubectl-syncpod_linux_amd64.deb
```

#### Apline Linux

```
apk update && apk add --no-cache bash curl
curl -LO https://github.com/hashmap-kz/kubectl-syncpod/releases/latest/download/kubectl-syncpod_linux_amd64.apk
apk add kubectl-syncpod_linux_amd64.apk --allow-untrusted
```

---

---

## Example CLI Usage

### Upload with safe rename and chown:

```bash
kubectl-syncpod upload \
  --namespace pgrwl-test \
  --pvc postgres-data \
  --mount-path=/var/lib/postgresql/data \
  --src=backups \
  --dst=pgdata-new \
  --allow-overwrite \
  --owner="999:999"
```

Behavior:

- `/var/lib/postgresql/data/pgdata-new` -> renamed if exists
- `backups/*` -> uploaded to `pgdata-new/`
- Ownership set to `999:999` inside pod

---

### Download remote directory:

```bash
kubectl-syncpod download \
  --namespace pgrwl-test \
  --pvc postgres-data \
  --mount-path=/var/lib/postgresql/data \
  --src=pgdata-new \
  --dst=backups-copy
```

Behavior:

- `/var/lib/postgresql/data/pgdata-new/*` -> downloaded to `./backups-copy/` on local machine
- Preserves directory structure

---

## Comparison Table

| Feature                                   | `kubectl cp`                    | `kubectl exec`          | `kubectl-syncpod` (SFTP mode)         |
|-------------------------------------------|---------------------------------|-------------------------|---------------------------------------|
| Uses sidecar or helper pod                | ❌                               | ❌                       | ✅                                     |
| Works with PVCs                           | ⚠️ Only if mounted in container | ⚠️ Manual path required | ✅ Helper pod mounts PVC               |
| Requires tools in container (`tar`, `sh`) | ✅                               | ✅                       | ❌ (uses `sshd` in helper pod)         |
| Supports `readOnlyRootFilesystem` pods    | ❌                               | ❌                       | ✅                                     |
| Works on `distroless`/`scratch` images    | ❌                               | ❌                       | ✅                                     |
| Affects main application container        | ✅                               | ✅                       | ❌                                     |
| Requires container to run as root         | Often yes                       | Often yes               | ❌ or configurable via helper pod spec |
| Safe for production workloads             | ⚠️ Risky                        | ⚠️ Risky                | ✅ (safe for read)                     |
| Auto-cleans after sync                    | ❌                               | ❌                       | ✅                                     |
| Supports concurrent transfers             | ❌                               | ❌                       | ✅ (parallel SFTP workers)             |
| Performance on large file trees           | 🐢 Slow                         | 🐢 Slow                 | 🚀 Fast (streaming + concurrency)     |

---

## **License**

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
