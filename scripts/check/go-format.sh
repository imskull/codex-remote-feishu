#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

# Batch the file list through xargs instead of expanding every path into a
# single gofmt argv: passing all Go files at once overflows the OS argument
# limit on some platforms (notably Windows git-bash, which fails with
# "Argument list too long"). -print0/-0 keeps paths with spaces safe and -r
# skips gofmt entirely when no files match (avoiding a stdin-read hang).
output="$(find cmd internal testkit -name '*.go' -print0 | xargs -0 -r gofmt -l)"
if [[ -z "${output}" ]]; then
  exit 0
fi

echo "${output}" >&2
echo "Run make fmt to format remaining Go files before continuing." >&2
exit 1
