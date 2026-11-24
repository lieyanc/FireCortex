# AGENTS.md — FireCortex Engineering & Maintenance Guide

This file is the authoritative rulebook for AI coding agents working in **FireCortex**. The repository owner delegates low-level Git hygiene, upstream-fork maintenance, branding compliance, API contract regeneration, and verification to agents—follow these rules proactively on every task without waiting to be reminded.

---

## 1. Product Identity & Fork Scope

FireCortex is a heavily rebranded, independently maintained **server-first** fork of **DEEIX Chat** (`https://github.com/DEEIX-AI/DEEIX-Chat`, currently synced through `v0.4.6`; see `docs/UPSTREAM.md`).

- **Supported deployment (default):** Single self-contained, pure-Go (`CGO_ENABLED=0`) server binary (`scripts/build-binary.sh` / `pnpm build:binary`) serving the statically exported Next.js web UI (`apps/web/out` embedded into `backend/internal/webui/dist`) backed by **PostgreSQL + pgvector** (with Redis or single-process memory cache).
- **Opt-in / unsupported by default:**
  - **SQLite** stays strictly behind the `sqlite` / `cgo` build tags (`FIRECORTEX_SQLITE=1` / `-tags sqlite`). Never introduce untagged `go-sqlite3`, `sqlite-vec`, or CGO imports into default builds.
  - **Desktop app (`apps/desktop`)** is retained from upstream only for manual `workflow_dispatch` builds. Never re-enable automatic dispatch of `.github/workflows/desktop-release.yml` from `.github/workflows/release.yml`.
  - **Docker app images** are intentionally dropped. Do not add an application `Dockerfile`, root `docker-compose*.yml`, or main-application container image workflows. Optional extraction sidecars under `deploy/services/` stay as-is and may publish prebuilt sidecar-only images via `.github/workflows/sidecar-images.yml` for FireCortex's built-in Docker infrastructure manager (`managedinfra`).
- **Configuration:** FireCortex uses `config.json` backed by embedded `backend/internal/infra/config/template.json` (not upstream's `config.yaml`). Whenever adding or changing a config field, update `template.json` and `fileConfig` in matching field order (`TestTemplateMatchesFileConfig` enforces this) and update `config*.example.json` overlays if needed.

---

## 2. Repository Map & Code Boundaries

- `backend/` — Go 1.26 (`Gin`, `Gorm`, `Zap`). Layer direction: `cmd/server` → `internal/cli` → `internal/app` → `internal/transport/http` → `internal/application` → `internal/domain` / `internal/repository` / `internal/ports` → `internal/infra`.
- `apps/web/` — Next.js 16 (static export), React 19, TypeScript, Tailwind CSS, Biome. Follow `apps/web/scripts/check-architecture.mjs` naming/boundary rules.
- `packages/core/` — Shared client logic (auth state machine, API base URL resolution).
- `packages/api-contract/` — Auto-generated TypeScript API contract derived from `backend/docs/swagger.json`.
  - **Never hand-edit** `backend/docs/{docs.go,swagger.json,swagger.yaml}` or `packages/api-contract/src/types.generated.ts`. Always regenerate them with `pnpm api:generate`.

---

## 3. Fork-Friendly Coding & Branding Rules

Write code assuming future upstream feature syncs will happen:

1. **Extension-first / new-file-first:** Put FireCortex-specific capabilities into new packages or dedicated files (e.g., `backend/internal/application/modelrouter/`, `service_model_router.go`, `openwebui_conversation_import.go`, `sqlite_driver*.go`). Keep edits inside upstream-origin files as small wiring/hook points so future merges or patches conflict minimally.
2. **Mechanical renames live only in `scripts/rebrand.sh`:**
   - Never leave stray upstream brand tokens anywhere in the repository; `bash scripts/rebrand.sh --check` (also enforced in CI) must pass.
   - If a new upstream token appears, add a rule to `scripts/rebrand.sh`, rerun it, and commit the mechanical rename separately from feature code.
   - Legitimate upstream attribution belongs either in `EXCLUDED_FILES` in `scripts/rebrand.sh` or on lines marked with `rebrand:keep` (`<!-- rebrand:keep -->` / `// rebrand:keep`).
   - Runtime white-label branding is configured via `config.json` (`branding.*`; see `docs/BRANDING.md`). Never remove or alter `LICENSE` or `NOTICE`.
3. **Record architectural divergences:** Whenever introducing a permanent structural divergence from upstream (deployment, config, removed subsystem, API change), append a short entry to `docs/UPSTREAM.md`.

---

## 4. Git Hygiene & Commit Rules (Agent-Owned)

### Local Git setup
- On any fresh clone (or if upstream remotes/config look misconfigured), run `bash scripts/setup-git.sh` (or `make setup-git`). This script is idempotent and enforces:
  - `remote.upstream.pushurl = DISABLED` (never push to upstream),
  - `remote.upstream.tagOpt = --no-tags` with isolated fetch refspec `+refs/tags/*:refs/upstream-tags/*`,
  - `merge.ours.driver = true` (activates `.gitattributes` `merge=ours` for fork-exclusive files),
  - `rerere.enabled = true` and `rerere.autoupdate = true`.

### Branches & tags
- **Server versions:** Set `VERSION` to `X.Y.Z.N`. The first three numbers match the upstream stable tag currently integrated (see `docs/UPSTREAM.md`). Increment only the positive FireCortex revision for each version bump on that baseline (for example `0.4.6.2` → `0.4.6.3`). Start at revision 1 after syncing a newer upstream version. Run `pnpm api:generate` to synchronize manifests and contracts.
- `dev` — Primary integration branch. Pushing to `origin/dev` updates the rolling `dev-latest` prerelease.
- `main` — Stable release branch promoted from verified `dev`.
- `upstream-rebranded` — Mechanical rebrand snapshots of upstream tags ONLY. **Never hand-edit, rebase, squash, or force-push this branch.** Always push `upstream-rebranded` to `origin` after running `scripts/sync-upstream.sh`.
- **Tags:** Upstream tags live exclusively in `refs/upstream-tags/*`. Never run `git fetch upstream --tags` into `refs/tags/*` and never run `git push origin --tags`. `refs/tags/*` belongs solely to FireCortex releases (`v*.*.*` triggers `.github/workflows/release.yml`).

### Commit discipline
- **CI commit subject regex** (`.github/workflows/ci.yml`):
  ```text
  ^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test): [[:print:]]+$
  ```
  - **English ASCII only.**
  - **No `(scope)` parentheses** (`feat: ...`, NOT `feat(chat): ...` — scopes fail CI!).
- **Atomic commits:** Split unrelated changes (e.g., build/CI, docs, distinct features, backend vs frontend when independently buildable) into logical, self-contained commits that each compile and pass checks.
- **Keep contracts synchronized per commit:** Any commit that changes Go HTTP handlers, routes, or Swagger DTOs must include regenerated `backend/docs/*` and `packages/api-contract/src/types.generated.ts` in the same commit so `pnpm api:check` passes.
- **Discard build noise:** Never commit `apps/web/next-env.d.ts` when `next build` flips `./.next/dev/types/*` ↔ `./.next/types/*`; restore it with `git checkout -- apps/web/next-env.d.ts`.
- **Never rewrite history across an upstream merge:** Do not squash or rebase commits containing a merge from `upstream-rebranded`.

---

## 5. Upstream Sync Playbook

Only sync from **upstream stable release tags** (e.g., `v0.4.7`), never arbitrary `upstream/dev` commits unless explicitly requested.

### Step 1: Record the rebranded upstream snapshot (required for both modes)
```bash
bash scripts/setup-git.sh                # safe no-op if already configured
bash scripts/sync-upstream.sh v0.4.7     # updates upstream-rebranded without touching refs/tags/*
git push origin upstream-rebranded
```

### Mode A — Full stable release merge (when divergence is manageable)
1. Create a sync branch: `git switch -c sync/upstream-v0.4.7 dev`
2. Merge preserving merge history: `git merge --no-ff upstream-rebranded`
3. Resolve conflicts following `docs/UPSTREAM.md`:
   - Fork-exclusive files listed in `.gitattributes` (`README.md`, `docs/README.zh-CN.md`, `docs/UPSTREAM.md`, `deploy/README.md`, `deploy/firecortex.service`, `.github/workflows/release.yml`, `AGENTS.md`, etc.) keep FireCortex's copy automatically via `merge=ours`. Review `git diff <prev>..<curr> -- <file>` if upstream might have relevant notes or dependency changes.
   - Keep intentionally deleted Dockerfiles, compose files, image workflows, and `config*.yaml` examples deleted (`git rm`), porting any needed runtime/version changes by hand.
   - Port any new upstream config fields into `backend/internal/infra/config/template.json` + `fileConfig`.
   - Ensure no untagged SQLite/CGO imports crept into the default build (`CGO_ENABLED=0 go build ./...`).
   - Do not hand-merge `pnpm-lock.yaml`, `backend/docs/*`, or `packages/api-contract/src/types.generated.ts`; resolve by regenerating (`pnpm install && pnpm api:generate`).
4. Run the full verification suite (Section 6), then merge `--no-ff` into `dev`.

### Mode B — Selective feature porting (when full merge brings too much churn)
When the user only wants specific upstream features/fixes without merging the whole release:
1. Keep updating `upstream-rebranded` via Step 1 so every upstream diff is already mechanically rebranded (`github.com/lieyanc/FireCortex`, `@firecortex/*`, etc.).
2. Inspect changes between rebranded snapshots:
   ```bash
   git log --first-parent <prev>..<curr> --oneline
   git diff --stat <prev>..<curr>
   ```
3. Port only the desired paths or feature patches:
   ```bash
   git diff <prev>..<curr> -- <path1> <path2> | git apply -3
   ```
4. Adapt wiring to FireCortex conventions, regenerate API contracts if needed, and run the verification suite.

---

## 6. Verification Checklist (Run Before Reporting Done)

Run every check relevant to the files you touched:

- **Backend (Go):**
  ```bash
  cd backend
  CGO_ENABLED=0 go build ./...                                                   # MUST pass (pure-Go default)
  go build -tags sqlite ./...                                                    # opt-in SQLite build
  go build -tags nopostgres,noredis,nos3,noswagger,nomsgpack,nootlp,sqlite ./... # desktop sidecar tags
  go vet ./...
  go tool deadcode -tags sqlite ./...                                            # all Go code must be reachable
  go test ./...
  ```
  *Local environment tip:* If the host lacks system `libsqlite3-dev` (`sqlite3.h` missing during CGO builds/tests), copy `sqlite3-binding.h` → `sqlite3.h` and `sqlite3ext.h` from `$(cd backend && go list -m -f '{{.Dir}}' github.com/mattn/go-sqlite3)` into a temp directory and export `CGO_CFLAGS="-O2 -g -I<temp-dir>"`.
- **API contracts & versions:**
  ```bash
  pnpm api:check       # regenerate first via `pnpm api:generate` if backend HTTP/Swagger changed
  ```
- **Web frontend:**
  ```bash
  pnpm --filter @firecortex/web lint
  pnpm --filter @firecortex/web typecheck
  pnpm --filter @firecortex/web check:arch
  ```
- **Desktop workflow tests** (when touching `.github/workflows/*` or `apps/desktop/scripts/*`):
  ```bash
  node --test apps/desktop/scripts/*.test.mjs
  ```
- **Branding & commit compliance:**
  ```bash
  bash scripts/rebrand.sh --check
  ```
  Verify every new commit subject matches `^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test): [[:print:]]+$`.
