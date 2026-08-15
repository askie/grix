#!/usr/bin/env bash

set -euo pipefail

commit_range="${1:-HEAD}"
forbidden_pattern='^[[:space:]]*co-authored-by:[[:space:]].*(claude|cursor|noreply@anthropic\.com|cursoragent@cursor\.com)'
violations=0

while IFS= read -r commit; do
  if git show -s --format=%B "$commit" | grep -Eiq "$forbidden_pattern"; then
    printf 'Forbidden AI co-author attribution in %s: %s\n' \
      "$commit" "$(git show -s --format=%s "$commit")" >&2
    violations=1
  fi
done < <(git rev-list "$commit_range")

if (( violations != 0 )); then
  printf 'Remove Claude/Cursor Co-authored-by trailers before pushing.\n' >&2
  exit 1
fi

printf 'Commit attribution check passed for %s.\n' "$commit_range"
