#!/usr/bin/env bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$TASK_ROOT"
source scripts/java-env.sh
GO_BIN="${X_MOCK_GO:-$TASK_ROOT/.tools/go/1.27.1/go/bin/go}"
if [[ ! -x "$GO_BIN" ]]; then GO_BIN=go; fi
mkdir -p artifacts/orders
"$GO_BIN" test ./integration -run '^TestSpringBootOrderExample$' -count=1 -v -timeout=5m 2>&1 | tee artifacts/orders/verification.log
