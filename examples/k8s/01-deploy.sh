#!/bin/bash
set -euo pipefail

(
  cd images/distroless
  bash build.sh
)

kubectl apply -f manifests/
kubectl -n pgrwl-test rollout restart sts postgres
kubectl -n pgrwl-test rollout restart deploy distroless
kubectl -n rmq1-test rollout restart sts rabbitmq
kubectl -n rmq2-test rollout restart sts rabbitmq
