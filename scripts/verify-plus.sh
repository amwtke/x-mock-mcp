#!/usr/bin/env bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$TASK_ROOT"
source scripts/java-env.sh
mkdir -p artifacts/p1-plus
# P1 already includes all Go integrations: three data-access profiles, Plus
# deletion, MCP AgentFill/replays, defects, versions and the dependency audit.
bash scripts/verify-p1.sh
mvn -B -ntp -o -f examples/springboot-shop/pom.xml -Dtest=PlusMapperContractTest test > artifacts/p1-plus/mapper-contract.log 2>&1
cp examples/springboot-shop/target/plus-statements.json artifacts/p1-plus/plus-statements.json
echo 'PASS offline MyBatis-Plus generated SQL / parameter / key contract'
echo 'MyBatis-Plus verification passed; Spring Boot uses only our MySQL protocol endpoint.'
