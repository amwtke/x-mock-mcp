#!/usr/bin/env bash
# Shared by setup and verification; source this file from repository scripts.
JAVA_HOME="${X_MOCK_JAVA_HOME:-${JAVA_HOME:-}}"
if [[ -z "$JAVA_HOME" && "$(uname -s)" == Darwin ]]; then
  JAVA_HOME="$(/usr/libexec/java_home -v 21 2>/dev/null || true)"
  if [[ -z "$JAVA_HOME" ]] && command -v brew >/dev/null; then
    JAVA_HOME="$(brew --prefix openjdk@21)/libexec/openjdk.jdk/Contents/Home"
  fi
fi
if [[ ! -x "$JAVA_HOME/bin/java" ]]; then
  echo 'Set X_MOCK_JAVA_HOME (or JAVA_HOME) to a JDK 21 installation.' >&2
  exit 1
fi
TASK_JAVA_VERSION="$("$JAVA_HOME/bin/java" -version 2>&1)"
if [[ "$TASK_JAVA_VERSION" != *'version "21.'* ]]; then
  echo 'P0 verification requires JDK 21.' >&2
  exit 1
fi
export JAVA_HOME X_MOCK_JAVA_HOME="$JAVA_HOME"
export PLAYWRIGHT_BROWSERS_PATH="${PLAYWRIGHT_BROWSERS_PATH:-$TASK_ROOT/.tools/playwright-browsers}"
