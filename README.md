```
mkdir -p backups
go run main.go download --namespace pgrwl-test --pvc pgrwl-data --mount-path=/wals wal-archive backups
go run main.go download --namespace pgrwl-test --pvc postgres-data --mount-path=/var/lib/postgresql/data pgdata backups
```