#!/usr/bin/env bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$TASK_ROOT"
source scripts/java-env.sh
bash scripts/build.sh
exec python3 example/springboot/run.py "$@"
