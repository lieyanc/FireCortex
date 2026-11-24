# Syncing with Upstream

FireCortex is derived from [DEEIX Chat](https://github.com/DEEIX-AI/DEEIX-Chat), forked at v0.4.3 and currently synced through v0.4.6. The fork renames the product everywhere (Go module path, npm scope, identifiers, storage keys, template variables, Docker names), yet it can still merge full upstream releases or selectively port individual features with minimal conflicts.

This file is excluded from `scripts/rebrand.sh`, so upstream names in it stay verbatim.

## How it works

- `scripts/rebrand.sh` is the single source of truth for every mechanical rename. It is idempotent, and `--check` (also enforced by CI in `.github/workflows/ci.yml`) fails if any upstream brand token remains.
- The `upstream-rebranded` branch holds upstream snapshots with the rebrand script applied and nothing else: no fork code, no hand edits, never rebased.
- `scripts/sync-upstream.sh <ref>` fetches upstream branches and isolated tags (`refs/upstream-tags/*`, never polluting `refs/tags/*`), rebrands the raw upstream ref in a temporary worktree, and commits it on top of `upstream-rebranded` with the raw upstream commit as second parent.
- `dev` either merges `upstream-rebranded` (full release sync) or extracts already-rebranded diffs between snapshots (selective feature porting).

```text
upstream   v0.4.3 ─────────────── v0.4.6 ─────────── v0.5.0
              │                     │                  │
rebranded  R(v0.4.3) ───────────── R(v0.4.6) ──────── R(v0.5.0)
              │                        \                ┊ (full merge OR selective diff)
dev           └── fork work ─────────── merge ── fork ──┴── merge / cherry-pick
```

## One-time setup

Run the idempotent setup script on any fresh clone (also available via `make setup-git`):

```bash
bash scripts/setup-git.sh
```

This configures:

- `remote.upstream` pointing to `https://github.com/DEEIX-AI/DEEIX-Chat.git` with `pushurl = DISABLED` so upstream can never be pushed to accidentally.
- `remote.upstream.tagOpt = --no-tags` and fetch refspec `+refs/tags/*:refs/upstream-tags/*` so upstream tags stay out of `refs/tags/*`.
- `merge.ours.driver = true` (activates `.gitattributes` `merge=ours` rules for fork-exclusive docs, policy, and release files) and `rerere.enabled = true` / `rerere.autoupdate = true`.

If an existing local clone already fetched upstream tags into `refs/tags/*`, migrate them cleanly into `refs/upstream-tags/*`:

```bash
bash scripts/setup-git.sh --migrate-upstream-tags
```

## Tag & versioning policy

- `refs/upstream-tags/*` holds raw upstream tags (`v0.4.3`, `v0.4.6`, ...). Never run `git fetch upstream --tags` into `refs/tags/*` and never `git push origin --tags`.
- `refs/tags/*` is reserved exclusively for FireCortex's own releases (`dev-latest` rolling prereleases and FireCortex `v*.*.*` release tags consumed by `.github/workflows/release.yml`).
- FireCortex server versions append a positive revision to the integrated upstream tag. On the v0.4.6 baseline, `0.4.6.2` is followed by `0.4.6.3`. When the upstream baseline advances, start its revision at 1. Both server channels embed the four-part `VERSION`. The rolling dev release also records its commit. npm, Cargo and Tauri use the SemVer representation `0.4.6-fc.2`.

## Syncing a stable release

Always update `upstream-rebranded` from an upstream **stable release tag** first and publish it to `origin`:

```bash
bash scripts/sync-upstream.sh v0.4.7
git push origin upstream-rebranded
```

Then choose the sync mode that fits the current divergence level:

### Mode A: Full release merge (standard while divergence is manageable)

```bash
git switch -c sync/upstream-v0.4.7 dev
git merge --no-ff upstream-rebranded
```

After resolving conflicts (never squash or rebase across the merge commit):

```bash
bash scripts/rebrand.sh --check        # no upstream brand tokens slipped in
pnpm install                           # regenerate lockfile and workspace names
pnpm api:generate                      # regenerate Swagger docs and TS API contract
cd backend && CGO_ENABLED=0 go build ./... && go build -tags sqlite ./... && go test ./...
cd .. && pnpm --filter @firecortex/web lint && pnpm --filter @firecortex/web typecheck
```

Then merge `sync/upstream-v0.4.7` into `dev` with `git merge --no-ff`.

### Mode B: Selective feature porting (recommended once customization deepens)

If an upstream release introduces heavy UI/architecture churn that conflicts with FireCortex's customizations, skip the full merge and port only the desired features using the already-rebranded snapshots on `upstream-rebranded`:

```bash
# Inspect commits and changed files between the previous and new rebranded snapshots
git log --first-parent <prev-rebranded>..<new-rebranded> --oneline
git diff --stat <prev-rebranded>..<new-rebranded>

# Extract and 3-way apply a specific subsystem or feature
git diff <prev-rebranded>..<new-rebranded> -- backend/internal/infra/llm/ | git apply -3
```

Because `upstream-rebranded` has already rewritten Go module paths, npm scopes, and identifiers to FireCortex, extracted patches apply cleanly without manual package renaming.

## Fork-exclusive files (`.gitattributes`)

Pure FireCortex documentation, policy, and release files (`AGENTS.md`, `CLAUDE.md`, `README.md`, `docs/README.zh-CN.md`, `docs/UPSTREAM.md`, `deploy/README.md`, `deploy/firecortex.service`, `.github/CODE_OF_CONDUCT.md`, `.github/SECURITY.md`, `.github/workflows/release.yml`) are marked `merge=ours` in `.gitattributes`. During merges from `upstream-rebranded`, FireCortex's version is preserved automatically; inspect `git diff <prev>..<curr> -- <file>` when checking whether upstream added anything worth porting by hand.

Generated files (`pnpm-lock.yaml`, `backend/docs/*`, `packages/api-contract/src/types.generated.ts`) are intentionally **not** marked `merge=ours`—resolve conflicts in them by taking one side and regenerating via `pnpm install && pnpm api:generate`.

## Rules for fork changes

- Rename logic belongs in `scripts/rebrand.sh`, never in hand edits. When a new upstream token appears (reported by `--check`), add a rule, rerun the script on `dev`, and commit the result on its own. The next sync applies the same rule to upstream.
- Upstream attribution that must keep the DEEIX name lives in excluded files (`AGENTS.md`, `LICENSE`, `NOTICE`, this file, `apps/web/shared/config/upstream.ts`, `scripts/{rebrand,setup-git,sync-upstream}.sh`) or on lines ending with a `rebrand:keep` marker (for example `<!-- rebrand:keep -->` in Markdown or `// rebrand:keep` in code).
- Keep hand edits to upstream files small and prefer new files for FireCortex-only features. Every line changed in an upstream file is a potential conflict on the next sync.
- Upstream contact addresses (`support@deeix.com`) are skipped by the script and must be rewritten by hand when they appear.

## Deployment divergence

FireCortex ships only as a single binary with the web UI embedded, so the fork deleted upstream's `Dockerfile`, `.dockerignore`, root `docker-compose.yml`, `docker-compose.sqlite.yml`, `docker-compose.full.yml`, and the image workflows `.github/workflows/ghcr-image.yml` and `.github/workflows/dockerhub-image.yml`. An upstream sync that touches any of them stops with a modify/delete conflict. Resolve it by keeping them deleted:

```bash
git rm Dockerfile docker-compose.full.yml   # whichever deleted paths conflict
```

Before discarding such a change, check whether it carries something the binary still needs, such as a new build dependency, a Go or Node.js version bump, or a new persistent path, and port it to `scripts/build-binary.sh`, `.github/workflows/release.yml`, the config examples, or the README. The optional extraction services under `deploy/services/` are kept and merge normally, and `.github/workflows/sidecar-images.yml` prebuilds multi-arch sidecar-only images (`docling`, `tesseract`, `rapidocr`) to GHCR so single-binary servers can manage them without a source checkout.

FireCortex also introduces self-managed Docker infrastructure (`backend/internal/infra/dockermgr`, `backend/internal/application/managedinfra`, `backend/internal/application/setup`, `backend/internal/transport/http/{setup,infra}`, `firecortex install-service`, and the `/setup` + `/admin/infrastructure` web surfaces) using pure-Go Docker Engine Socket API calls, runtime-synced `<managed.data_dir>/docker-compose.yml` + `0600` `.env` files, automated `pg_dump` backups, and Danger Zone operations. Setup retains private per-service credentials in the managed data directory across retries and database switches. Adopting an existing database verifies its stored secrets with the configured keyring before saving the completed setup. Keep all fork-specific code in these dedicated packages so upstream syncs conflict minimally.

Upstream serves the static frontend from disk through `registerFrontendStatic` in `backend/internal/transport/http/server.go`, driven by `server.frontend_dist_dir`. FireCortex serves the UI embedded from `backend/internal/webui` and treats `frontend_dist_dir` only as an optional disk override. Upstream changes to `registerFrontendStatic` or `frontend_dist_dir` (routing, fallbacks, cache headers, manifest handling) do not carry over automatically; port them by hand to the embedded-filesystem implementation.

## Configuration divergence

Upstream reads a YAML `config.yaml` and keeps its defaults in Go literals. FireCortex reads `config.json` instead. The complete default configuration is the JSON template `backend/internal/infra/config/template.json`, embedded into the binary. On start, a missing file is written from it with random secrets, and keys missing from an existing file are filled in and saved (`backend/internal/infra/config/file.go`). The fork deleted upstream's `config.example.yaml`, `config.full.example.yaml`, and `config.sqlite.example.yaml` in favor of partial `config*.example.json` overlays, so an upstream sync that touches them stops with a modify/delete conflict. Keep them deleted.

When upstream adds or changes a config field, port it by hand: add the key to `template.json` and the matching field to `fileConfig` in the same position (`TestTemplateMatchesFileConfig` fails otherwise), then read it in `Load`. Move any default upstream puts in Go code into the template. If upstream changes a field that one of the example profiles sets, update the JSON overlay as well.

## Fork policy

FireCortex prioritizes server deployments (PostgreSQL + pgvector only, pure Go `CGO_ENABLED=0`). SQLite deployments and the desktop application are unsupported by default:

- **SQLite stays behind the `sqlite` build tag.** The gating files are `backend/internal/infra/persistence/sqlite_driver*.go`, `backend/internal/infra/openwebui/sqlite_source*.go`, `backend/internal/infra/persistence/sqlitevec/register_*.go`, and `backend/internal/infra/persistence/dberror/sqlite_*.go`. Upstream syncs must not reintroduce untagged SQLite or CGO imports into the default build; CI's `CGO_ENABLED=0 go build ./...` check guards this.
- **Desktop releases are manual.** `.github/workflows/release.yml` must not automatically dispatch `.github/workflows/desktop-release.yml`; desktop builds run only via explicit `workflow_dispatch` (with `-tags ...,sqlite` for the sidecar).
- **Keep `config.sqlite.example.json`.** It remains in the repository as referenced above for `-tags sqlite` opt-in builds, while documentation points server deployments to `config.full.example.json`.
