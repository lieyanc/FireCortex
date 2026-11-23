#!/usr/bin/env bash
# Record a rebranded upstream DEEIX Chat snapshot on the upstream-rebranded branch.
#
# The raw upstream tree at <ref> is checked out into a temporary worktree,
# rewritten by scripts/rebrand.sh, and committed on top of upstream-rebranded
# (second parent: the raw upstream commit). Merging that branch into the fork
# then only conflicts where upstream and FireCortex changed the same lines.
# See docs/UPSTREAM.md for the full workflow.
#
# Usage:
#   scripts/sync-upstream.sh <upstream-ref>    e.g. v0.4.4 or upstream/dev
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
  echo "sync-upstream: remote '$remote' is missing; add it with:" >&2
  echo "  git remote add $remote https://github.com/DEEIX-AI/DEEIX-Chat.git" >&2
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

git fetch --quiet --tags "$remote"
upstream_commit="$(git rev-parse --verify "$ref^{commit}")"
label="$(git describe --tags --always "$upstream_commit")"

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
  -m "chore(upstream): rebrand DEEIX Chat $label")"
git update-ref -m "sync-upstream: $label" "refs/heads/$branch" "$commit" "$parent"

echo "sync-upstream: $branch -> $(git rev-parse --short "$commit") (rebranded $label)"
echo "next: git merge --no-ff $branch"
