#!/usr/bin/env bash
# Configure local Git safety and upstream-sync settings for FireCortex.
#
# Idempotent: safe to run on a fresh clone or an existing checkout.
#
# What it configures:
#   - Ensures the `upstream` remote points to DEEIX-AI/DEEIX-Chat and disables
#     pushing to it (`pushurl = DISABLED`).
#   - Isolates upstream tags under `refs/upstream-tags/*` (`tagOpt = --no-tags`)
#     so upstream tags never collide with FireCortex's own `refs/tags/v*` releases.
#   - Enables the `ours` merge driver used by `.gitattributes` for fork-exclusive
#     files, and turns on `rerere` to remember recurring conflict resolutions.
#
# Optional flags:
#   --migrate-upstream-tags   Move existing local `refs/tags/*` that point to
#                             upstream commits (and do not exist on `origin`)
#                             into `refs/upstream-tags/*`.
#
# Environment:
#   UPSTREAM_REMOTE   remote name for DEEIX-AI/DEEIX-Chat (default: upstream)
#   UPSTREAM_URL      URL used when adding the remote (default: https://github.com/DEEIX-AI/DEEIX-Chat.git)

set -euo pipefail

migrate_tags=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --migrate-upstream-tags)
      migrate_tags=1
      shift
      ;;
    -h|--help)
      sed -n '2,23p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "setup-git: unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

remote="${UPSTREAM_REMOTE:-upstream}"
upstream_url="${UPSTREAM_URL:-https://github.com/DEEIX-AI/DEEIX-Chat.git}"
upstream_tag_refspec="+refs/tags/*:refs/upstream-tags/*"
upstream_head_refspec="+refs/heads/*:refs/remotes/${remote}/*"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# 1. Ensure upstream remote exists and cannot be pushed to.
if ! git remote get-url "$remote" >/dev/null 2>&1; then
  git remote add "$remote" "$upstream_url"
  echo "setup-git: added remote '$remote' -> $upstream_url"
fi
git config "remote.${remote}.pushurl" DISABLED

# 2. Isolate upstream tags in refs/upstream-tags/* instead of refs/tags/*.
git config "remote.${remote}.tagOpt" --no-tags

existing_refspecs="$(git config --get-all "remote.${remote}.fetch" || true)"
if ! grep -Fxq "$upstream_head_refspec" <<<"$existing_refspecs"; then
  git config --add "remote.${remote}.fetch" "$upstream_head_refspec"
fi
if ! grep -Fxq "$upstream_tag_refspec" <<<"$existing_refspecs"; then
  git config --add "remote.${remote}.fetch" "$upstream_tag_refspec"
fi

# 3. Enable `.gitattributes` `merge=ours` driver and conflict-resolution cache.
git config merge.ours.name "Keep FireCortex version on upstream merge"
git config merge.ours.driver true
git config rerere.enabled true
git config rerere.autoupdate true

# 4. Ensure local upstream-rebranded tracking branch exists if origin has it.
if ! git rev-parse --verify -q refs/heads/upstream-rebranded >/dev/null 2>&1; then
  if git rev-parse --verify -q refs/remotes/origin/upstream-rebranded >/dev/null 2>&1; then
    git branch --track upstream-rebranded origin/upstream-rebranded
    echo "setup-git: created local branch 'upstream-rebranded' from origin/upstream-rebranded"
  fi
fi

# 5. Optionally migrate upstream tags out of refs/tags/* into refs/upstream-tags/*.
if ((migrate_tags)); then
  upstream_tips=()
  for ref in "refs/remotes/${remote}/main" "refs/remotes/${remote}/dev"; do
    if git rev-parse --verify -q "$ref" >/dev/null 2>&1; then
      upstream_tips+=("$ref")
    fi
  done
  if [[ ${#upstream_tips[@]} -eq 0 ]]; then
    echo "setup-git: fetch '$remote' before running --migrate-upstream-tags" >&2
    exit 1
  fi

  moved=0
  while IFS=' ' read -r tag obj; do
    [[ -z "$tag" ]] && continue
    # Skip FireCortex rolling/custom tags.
    if [[ "$tag" == "dev-latest" ]]; then
      continue
    fi
    commit="$(git rev-parse --verify -q "${obj}^{commit}" || true)"
    [[ -z "$commit" ]] && continue

    is_upstream=0
    for tip in "${upstream_tips[@]}"; do
      if git merge-base --is-ancestor "$commit" "$tip" 2>/dev/null; then
        is_upstream=1
        break
      fi
    done
    # Also handle upstream tags like desktop-beta whose commits may sit on an
    # upstream tag-only ref outside main/dev (no FireCortex rebrand commit in history).
    if ((!is_upstream)); then
      if git show "${commit}:go.mod" 2>/dev/null | grep -q 'DEEIX-AI/DEEIX-Chat' \
        || git show "${commit}:backend/go.mod" 2>/dev/null | grep -q 'DEEIX-AI/DEEIX-Chat'; then
        is_upstream=1
      fi
    fi

    if ((is_upstream)); then
      git update-ref "refs/upstream-tags/${tag}" "$obj"
      git update-ref -d "refs/tags/${tag}"
      moved=$((moved + 1))
    fi
  done < <(git for-each-ref --format='%(refname:short) %(objectname)' refs/tags/)

  echo "setup-git: migrated ${moved} upstream tag(s) from refs/tags/* to refs/upstream-tags/*"
fi

echo "setup-git: configured '$remote' (pushurl=DISABLED, tagOpt=--no-tags, refs/upstream-tags/*), merge.ours, and rerere."
