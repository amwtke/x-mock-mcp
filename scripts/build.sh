#!/usr/bin/env bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$TASK_ROOT"
GO_BIN="${X_MOCK_GO:-$TASK_ROOT/.tools/go/1.27.1/go/bin/go}"
if [[ ! -x "$GO_BIN" ]]; then GO_BIN=go; fi
mkdir -p bin dist
"$GO_BIN" build -buildvcs=false -trimpath -o bin/x-mock ./cmd/x-mock
"$GO_BIN" build -buildvcs=false -trimpath -o bin/mysql-wire ./plugins/left/mysql-wire
"$GO_BIN" build -buildvcs=false -trimpath -o bin/mysql-mock ./plugins/right/mysql-mock
bin/mysql-mock --emit-schemas plugins/right/mysql-mock
python3 scripts/package-plugins.py
