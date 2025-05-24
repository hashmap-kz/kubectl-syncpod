```
mkdir -p backups
go run main.go download --namespace pgrwl-test --pvc distroless-data --mount-path=/tmp . backups
go run main.go download --namespace pgrwl-test --pvc postgres-data --mount-path=/var/lib/postgresql/data pgdata backups
```