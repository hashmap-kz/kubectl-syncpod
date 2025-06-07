## Usage (WIP)

Create a basebackup from a running PostgreSQL instance

```bash
sudo rm -rf backups

docker run --rm -it \
  --network host \
  -v ./backups:/backups \
  -e PGHOST=localhost \
  -e PGPORT=30265 \
  -e PGUSER=postgres \
  -e PGPASSWORD=postgres \
  postgres:17 \
  pg_basebackup \
  --pgdata=/backups \
  --checkpoint=fast \
  --progress \
  --verbose
  
sudo chown -R "${USER}:${USER}" backups
```

Stop PostgreSQL running as a container, restore from a basebackup

```bash
#kubectl -n pgrwl-test scale --replicas=0 statefulset/postgres

(rm -rf bin && cd ../../ && make build && mv bin examples/k8s)

bin/kubectl-syncpod upload \
  --namespace pgrwl-test \
  --pvc postgres-data \
  --mount-path=/var/lib/postgresql/data \
  --src=backups \
  --dst=pgdata-new \
  --allow-overwrite \
  --owner="999:999"
```

Start PostgreSQL 

```bash
kubectl -n pgrwl-test scale --replicas=1 statefulset/postgres
```

