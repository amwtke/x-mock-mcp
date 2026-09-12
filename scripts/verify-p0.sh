#!/usr/bin/env bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$TASK_ROOT"
source scripts/java-env.sh
GO_BIN="${X_MOCK_GO:-$TASK_ROOT/.tools/go/1.27.1/go/bin/go}"
if [[ ! -x "$GO_BIN" ]]; then GO_BIN=go; fi
mkdir -p artifacts/p0
run_check() {
  local name="$1"
  shift
  printf '%q ' "$@" >> artifacts/p0/verification-commands.log
  printf '\n' >> artifacts/p0/verification-commands.log
  if "$@" > "artifacts/p0/$name.log" 2>&1; then
    echo "PASS $name"
  else
    tail -60 "artifacts/p0/$name.log" >&2
    echo "FAIL $name" >&2
    return 1
  fi
}
date -u > artifacts/p0/verification-commands.log
run_check go-version "$GO_BIN" version
run_check java-version "$JAVA_HOME/bin/java" -version
run_check maven-version mvn --version
run_check java-dependencies mvn -B -ntp -o -f examples/springboot-shop/pom.xml dependency:tree
run_check core-race "$GO_BIN" test -race ./cmd/... ./pluginapi/... ./contracts/... ./internal/... ./plugins/... -count=1
run_check integration "$GO_BIN" test ./integration -count=1 -v -timeout=10m
run_check vet "$GO_BIN" vet ./...
run_check build bash scripts/build.sh
echo 'P0 automated verification passed. Native host evidence is recorded separately in docs/compatibility/p0.md.'
