#!/usr/bin/env bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$TASK_ROOT"
source scripts/java-env.sh
mkdir -p example/springboot/target
mvn -B -ntp -f example/springboot/pom.xml -DskipTests test dependency:go-offline dependency:build-classpath -Dmdep.outputFile=target/test-classpath.txt -DincludeScope=test > example/springboot/target/prepare.log 2>&1
"$JAVA_HOME/bin/java" -cp "$(cat example/springboot/target/test-classpath.txt)" com.microsoft.playwright.CLI install chromium >> example/springboot/target/prepare.log 2>&1
echo 'Java dependencies and Chromium ready; log: example/springboot/target/prepare.log'
