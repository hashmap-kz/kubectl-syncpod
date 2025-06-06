#!/usr/bin/env bash
set -euo pipefail

docker buildx build -t localhost:5000/distroless .
docker push localhost:5000/distroless
