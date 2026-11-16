#!/usr/bin/env bash
# Print the release version from the repository's VERSION file.
#
# Format: <upstream>+gotv<revision>-<debian-revision>, e.g. 0.8.5+gotv2-1.
# The matching git tag is release/v<upstream>-gotv<revision>.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
version=$(tr -d '[:space:]' < "$root/VERSION")

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(\+gotv[0-9]+)?-[0-9]+$ ]]; then
  echo "Invalid VERSION: '$version'" >&2
  echo "Expected <upstream>+gotv<revision>-<debian-revision>, e.g. 0.8.5+gotv2-1." >&2
  exit 1
fi

printf '%s\n' "$version"
