#!/usr/bin/env bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$TASK_ROOT"
source scripts/java-env.sh
GO_BIN="${X_MOCK_GO:-$TASK_ROOT/.tools/go/1.27.1/go/bin/go}"
if [[ ! -x "$GO_BIN" ]]; then GO_BIN=go; fi
mkdir -p artifacts/p1
# Includes P0 regressions, MyBatis HTTP/browser, AgentFill + three replays,
# application defects, actual old/new plugin packages, and MCP compatibility.
bash scripts/verify-p0.sh
mvn -B -ntp -o -f examples/springboot-shop/pom.xml -Dtest=MapperContractTest test > artifacts/p1/mapper-contract.log 2>&1
echo 'PASS offline Mapper contract'
"$GO_BIN" list -deps -test ./... > artifacts/p1/go-dependencies.txt
python3 -m unittest discover -s scripts -p test_no_database_services.py > artifacts/p1/dependency-boundary-test.log 2>&1
python3 scripts/check_no_database_services.py --go-deps artifacts/p1/go-dependencies.txt --java-deps artifacts/p0/java-dependencies.log > artifacts/p1/dependency-boundary.log
cat artifacts/p1/dependency-boundary.log
echo 'P1.1 MyBatis verification passed; no database service is installed or launched by these scripts.'
