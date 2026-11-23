#!/usr/bin/env bash
# Record a rebranded upstream DEEIX Chat snapshot on the upstream-rebranded branch.
#
# The raw upstream tree at <ref> is checked out into a temporary worktree,
# rewritten by scripts/rebrand.sh, and committed on top of upstream-rebranded
# (second parent: the raw upstream commit). Upstream tags are fetched into
# `refs/upstream-tags/*` rather than `refs/tags/*` so they never collide with
# FireCortex's own release tags.
#
# Both full release merges (`git merge --no-ff upstream-rebranded`) and
# selective feature porting (`git diff <prev>..<curr> -- <path> | git apply -3`)
# consume the snapshots recorded by this script. See docs/UPSTREAM.md.
#
# Usage:
#   scripts/sync-upstream.sh <upstream-ref>    e.g. v0.4.7 or upstream/main
#
# Environment:
#   UPSTREAM_REMOTE   remote name of DEEIX-AI/DEEIX-Chat (default: upstream)
#   REBRAND_BRANCH    branch holding rebranded snapshots (default: upstream-rebranded)

set -euo pipefail

ref="${1:?usage: scripts/sync-upstream.sh <upstream-ref>}"
remote="${UPSTREAM_REMOTE:-upstream}"
branch="${REBRAND_BRANCH:-upstream-rebranded}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! git remote get-url "$remote" >/dev/null 2>&1; then
  echo "sync-upstream: remote '$remote' is missing; run:" >&2
  echo "  bash scripts/setup-git.sh" >&2
  exit 1
fi
if ! parent="$(git rev-parse --verify -q "refs/heads/$branch")"; then
  echo "sync-upstream: branch '$branch' is missing; see docs/UPSTREAM.md (one-time setup)" >&2
  exit 1
fi
if [[ "$(git symbolic-ref -q --short HEAD || true)" == "$branch" ]]; then
  echo "sync-upstream: check out another branch before updating '$branch'" >&2
  exit 1
fi

# Fetch upstream branches and isolated tags without polluting refs/tags/*.
git fetch --quiet --no-tags "$remote" \
  "+refs/heads/*:refs/remotes/${remote}/*" \
  "+refs/tags/*:refs/upstream-tags/*"

# Prefer isolated refs/upstream-tags/<ref> when the caller passes a bare tag name (e.g. v0.4.7).
resolved_ref="$ref"
label=""
if git rev-parse --verify -q "refs/upstream-tags/${ref}^{commit}" >/dev/null 2>&1; then
  resolved_ref="refs/upstream-tags/${ref}"
  label="$ref"
elif [[ "$ref" == refs/upstream-tags/* ]] && git rev-parse --verify -q "${ref}^{commit}" >/dev/null 2>&1; then
  label="${ref#refs/upstream-tags/}"
fi

upstream_commit="$(git rev-parse --verify "${resolved_ref}^{commit}")"
if [[ -z "$label" ]]; then
  label="$(git for-each-ref --points-at="$upstream_commit" --format='%(refname:lstrip=2)' refs/upstream-tags/ | head -n 1 || true)"
fi
if [[ -z "$label" ]]; then
  label="$(git rev-parse --short "$upstream_commit")"
fi

tmp="$(mktemp -d)"
cleanup() {
  git worktree remove --force "$tmp/tree" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

# Run the fork's current rules, not whatever the upstream tree might contain.
cp scripts/rebrand.sh "$tmp/rebrand.sh"
git worktree add --detach --quiet "$tmp/tree" "$upstream_commit"
bash "$tmp/rebrand.sh" "$tmp/tree"
bash "$tmp/rebrand.sh" --check "$tmp/tree"

git -C "$tmp/tree" add -A
tree="$(git -C "$tmp/tree" write-tree)"
if [[ "$tree" == "$(git rev-parse "$parent^{tree}")" ]]; then
  echo "sync-upstream: $branch already matches rebranded $label"
  exit 0
fi

commit="$(git commit-tree "$tree" -p "$parent" -p "$upstream_commit" \
  -m "chore: rebrand DEEIX Chat $label")"
git update-ref -m "sync-upstream: $label" "refs/heads/$branch" "$commit" "$parent"

prev_short="$(git rev-parse --short "$parent")"
new_short="$(git rev-parse --short "$commit")"
echo "sync-upstream: $branch -> $new_short (rebranded $label)"
echo "next (publish mirror):      git push origin $branch"
echo "next (full release merge):  git switch -c sync/upstream-$label dev && git merge --no-ff $branch"
echo "next (selective port):      git diff $prev_short..$new_short -- <path> | git apply -3"
