# Syncing with Upstream

FireCortex is derived from [DEEIX Chat](https://github.com/DEEIX-AI/DEEIX-Chat), forked at v0.4.3. The fork renames the product everywhere (Go module path, npm scope, identifiers, storage keys, template variables, Docker names), yet it still merges upstream releases with conflicts limited to lines both projects actually changed.

This file is excluded from `scripts/rebrand.sh`, so upstream names in it stay verbatim.

## How it works

- `scripts/rebrand.sh` is the single source of truth for every mechanical rename. It is idempotent, and `--check` fails if any upstream brand token remains.
- The `upstream-rebranded` branch holds upstream snapshots with the rebrand script applied and nothing else: no fork code, no hand edits.
- `scripts/sync-upstream.sh <ref>` rebrands a raw upstream ref in a temporary worktree and commits it on top of `upstream-rebranded`. The raw upstream commit is the second parent, so upstream history stays reachable.
- `dev` merges `upstream-rebranded`. The merge base is the previous rebranded snapshot, so a line that upstream left alone never conflicts, even if FireCortex rewrote it by hand.

```
upstream   v0.4.3 ─────────────── v0.4.4 ─────────── v0.4.5
              │                     │                  │
rebranded  R(v0.4.3) ───────────── R(v0.4.4) ──────── R(v0.4.5)
              │                        \                  \
dev           └── fork work ─────────── merge ── fork ──── merge
```

## One-time setup

```bash
git remote add upstream https://github.com/DEEIX-AI/DEEIX-Chat.git
git fetch upstream --tags
git branch upstream-rebranded <commit "chore: rebrand DEEIX Chat v0.4.3 as FireCortex">
```

The initial rebrand commit on `dev` contains only the output of `scripts/rebrand.sh` applied to v0.4.3, which makes it the first rebranded snapshot.

## Syncing a release

```bash
git switch dev
scripts/sync-upstream.sh v0.4.4        # or upstream/dev for unreleased changes
git merge --no-ff upstream-rebranded
```

After resolving conflicts:

```bash
scripts/rebrand.sh --check             # no upstream brand tokens slipped in
pnpm install                           # lockfile and workspace names
pnpm api:generate                      # if backend Swagger annotations changed
pnpm verify
```

## Rules for fork changes

- Rename logic belongs in `scripts/rebrand.sh`, never in hand edits. When a new upstream token appears (reported by `--check`), add a rule, rerun the script on `dev`, and commit the result on its own. The next sync applies the same rule to upstream.
- Upstream attribution that must keep the DEEIX name lives in excluded files (`NOTICE`, `LICENSE`, this file, `frontend/shared/config/upstream.ts`) or on lines ending with a `rebrand:keep` marker (for example `<!-- rebrand:keep -->` in Markdown or `// rebrand:keep` in code).
- Keep hand edits to upstream files small and prefer new files for FireCortex-only features. Every line changed in an upstream file is a potential conflict on the next sync.
- Upstream contact addresses (`support@deeix.com`) are skipped by the script and must be rewritten by hand when they appear.

## Deployment divergence

FireCortex ships only as a single binary with the web UI embedded, so the fork deleted upstream's `Dockerfile`, `.dockerignore`, root `docker-compose.yml`, `docker-compose.sqlite.yml`, `docker-compose.full.yml`, and the image workflows `.github/workflows/ghcr-image.yml` and `.github/workflows/dockerhub-image.yml`. An upstream sync that touches any of them stops with a modify/delete conflict. Resolve it by keeping them deleted:

```bash
git rm Dockerfile docker-compose.full.yml   # whichever deleted paths conflict
```

Before discarding such a change, check whether it carries something the binary still needs, such as a new build dependency, a Go or Node.js version bump, or a new persistent path, and port it to `scripts/build-binary.sh`, `.github/workflows/release.yml`, the config examples, or the README. The optional extraction services under `docker/` are kept and merge normally.

Upstream serves the static frontend from disk through `registerFrontendStatic` in `backend/internal/transport/http/server.go`, driven by `server.frontend_dist_dir`. FireCortex serves the UI embedded from `backend/internal/webui` and treats `frontend_dist_dir` only as an optional disk override. Upstream changes to `registerFrontendStatic` or `frontend_dist_dir` (routing, fallbacks, cache headers, manifest handling) do not carry over automatically; port them by hand to the embedded-filesystem implementation.
