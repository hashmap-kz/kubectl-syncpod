#!/bin/bash
set -euo pipefail

(
  cd images/distroless
  bash build.sh
)

kubectl apply -f manifests/
