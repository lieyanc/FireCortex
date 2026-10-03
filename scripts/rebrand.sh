#!/usr/bin/env bash
# Mechanically rebrand an upstream DEEIX Chat tree as FireCortex.
#
# This script is the single source of truth for every mechanical rename
# (Go module path, npm scope, identifiers, storage keys, template variables,
# Docker names, file paths). It is idempotent and is applied to raw upstream
# trees by scripts/sync-upstream.sh, so both sides of an upstream merge carry
# identical renames and only genuine changes can conflict.
#
# Hand-written fork content must stay out of its reach:
#   - files listed in EXCLUDED_FILES are never touched;
#   - any line containing the marker "rebrand:keep" is left verbatim.
#
# Usage:
#   scripts/rebrand.sh [--check] [DIR]
#     DIR      git work tree to rewrite (default: repository root)
#     --check  rewrite nothing; exit 1 if any upstream brand token remains

set -euo pipefail

check_only=0
if [[ "${1:-}" == "--check" ]]; then
  check_only=1
  shift
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
target="${1:-$(cd "$script_dir/.." && pwd)}"
cd "$target"

EXCLUDED_FILES=(
  LICENSE
  NOTICE
  docs/UPSTREAM.md
  frontend/shared/config/upstream.ts
  scripts/rebrand.sh
  scripts/sync-upstream.sh
)

BRAND_PATTERN='deeix'

is_excluded() {
  local path="$1" excluded
  for excluded in "${EXCLUDED_FILES[@]}"; do
    [[ "$path" == "$excluded" ]] && return 0
  done
  return 1
}

# Ordered from most to least specific; later rules must not re-match earlier output.
read -r -d '' RULES <<'PERL' || true
next LINE if /rebrand:keep/;
# Contact addresses have no FireCortex equivalent and are rewritten by hand.
next LINE if /support\@deeix\.com/;

# Repository, image registry and website.
s{github\.com/DEEIX-AI/DEEIX-Chat}{github.com/lieyanc/FireCortex}g;
s{github\.com/DEEIX-AI}{github.com/lieyanc}g;
s{ghcr\.io/deeix-ai/deeix-chat}{ghcr.io/lieyanc/firecortex}g;
s{https?://deeix\.com(?:/[^\s"'`)<>\]]*)?}{https://github.com/lieyanc/FireCortex}g;
s{\bdeeix\.com\b}{github.com/lieyanc/FireCortex}g;

# Reserved usernames: keep the two upstream entries distinct after collapsing.
s{"deeix_chat"}{"fire_cortex"}g;

# Product name and constant-style identifiers.
s{DEEIX(?: |-|/|%20|%2F)Chat}{FireCortex}g;
s{DEEIX_CHAT_}{FIRECORTEX_}g;
s{DEEIX_}{FIRECORTEX_}g;
s{X-Deeix-}{X-FireCortex-}g;
s{Deeix}{FireCortex}g;
s{DEEIX}{FireCortex}g;

# Lowercase slugs: npm scope, native client IDs, storage keys, container names.
s{\@deeix/}{\@firecortex/}g;
s{com\.deeix\.chat}{com.firecortex}g;
s{deeix[-_]chat}{firecortex}g;
s{deeix}{firecortex}g;
PERL

rename_path() {
  local path="$1"
  perl -pe 's{DEEIX-Chat}{FireCortex}g; s{deeix}{firecortex}g' <<<"$path"
}

leftovers=0
changed=0

while IFS= read -r -d '' path; do
  is_excluded "$path" && continue
  [[ -f "$path" ]] || continue
  if ((check_only)); then
    matches="$(grep -n -i "$BRAND_PATTERN" "$path" | grep -v -e 'rebrand:keep' -e 'support@deeix\.com' || true)"
    if [[ -n "$matches" ]]; then
      sed "s|^|$path:|" <<<"$matches"
      leftovers=1
    fi
    continue
  fi
  before="$(git hash-object "$path")"
  perl -i -pe "$RULES" "$path"
  [[ "$(git hash-object "$path")" != "$before" ]] && changed=$((changed + 1))
done < <(git grep -I -i -l -z "$BRAND_PATTERN" -- . || true)

while IFS= read -r -d '' path; do
  is_excluded "$path" && continue
  renamed="$(rename_path "$path")"
  [[ "$renamed" == "$path" ]] && continue
  if ((check_only)); then
    echo "path: $path"
    leftovers=1
    continue
  fi
  mkdir -p "$(dirname "$renamed")"
  git mv "$path" "$renamed"
  changed=$((changed + 1))
done < <(git ls-files -z | grep -z -i "$BRAND_PATTERN" || true)

if ((check_only)); then
  if ((leftovers)); then
    echo "rebrand: upstream brand tokens remain (see above)" >&2
    exit 1
  fi
  echo "rebrand: clean"
else
  echo "rebrand: updated $changed file(s)"
fi
