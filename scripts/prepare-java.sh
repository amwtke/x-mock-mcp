#!/usr/bin/env bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$TASK_ROOT"
source scripts/java-env.sh
mkdir -p artifacts/p0
mvn -B -ntp -f examples/springboot-shop/pom.xml -DskipTests test dependency:go-offline dependency:build-classpath -Dmdep.outputFile=target/test-classpath.txt -DincludeScope=test > artifacts/p0/prepare-java.log 2>&1
"$JAVA_HOME/bin/java" -cp "$(cat examples/springboot-shop/target/test-classpath.txt)" com.microsoft.playwright.CLI install chromium >> artifacts/p0/prepare-java.log 2>&1
echo 'Java dependencies and Chromium are ready; see artifacts/p0/prepare-java.log.'
