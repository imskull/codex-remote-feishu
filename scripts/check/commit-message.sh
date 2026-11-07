#!/usr/bin/env bash
set -euo pipefail

message_file="${1:?usage: commit-message.sh <message-file>}"
subject="$(head -n 1 "${message_file}" | tr -d '\r')"
if grep -Eiq '^Co-Authored-By:' "${message_file}"; then
  echo 'Do not append Co-Authored-By trailers in this repository.' >&2
  exit 1
fi
# Git generates these subjects when merging or reverting existing commits.
case "${subject}" in
  "Merge "*|"Revert \""*) exit 0 ;;
esac
pattern='^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-zA-Z0-9_./-]+\))?!?: [^[:space:]].*$'
if [[ ! "${subject}" =~ ${pattern} ]]; then
  echo 'Commit subject must use <type>[optional scope][!]: <description> (for example, fix(feishu): restore workspace switching).' >&2
  exit 1
fi
