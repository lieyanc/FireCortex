#!/usr/bin/env bash
# Build the single-binary FireCortex server with the web UI embedded.
#
# Steps: export the Next.js frontend, stage apps/web/out into
# backend/internal/webui/dist (picked up by go:embed), then build a pure-Go
# (CGO_ENABLED=0, PostgreSQL-only) server binary by default. Set
# FIRECORTEX_SQLITE=1 to opt into an unsupported CGO build with -tags sqlite.
#
# Usage:
#   scripts/build-binary.sh [--skip-frontend] [OUTPUT]
#     OUTPUT           binary path (default: dist/firecortex)
#     --skip-frontend  reuse an existing apps/web/out (CI builds it once)
#   FIRECORTEX_SQLITE=1 scripts/build-binary.sh ...  # unsupported SQLite + CGO build
#
# Build metadata defaults to the VERSION file and git; CI overrides it with
# the VERSION, COMMIT and BUILD_TIME environment variables. The updater
# compares these values, so release builds must set them consistently.

set -euo pipefail

skip_frontend=0
if [[ "${1:-}" == "--skip-frontend" ]]; then
  skip_frontend=1
  shift
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="${1:-dist/firecortex}"
[[ "$output" == /* ]] || output="$root/$output"

version="${VERSION:-$(tr -d '[:space:]' < "$root/VERSION")}"
commit="${COMMIT:-$(git -C "$root" rev-parse --short=7 HEAD 2>/dev/null || echo unknown)}"
build_time="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

if ((!skip_frontend)); then
  echo "==> Building frontend"
  # The embedded UI is served same-origin. An empty value still overrides a
  # developer's apps/web/.env.local, which would otherwise bake in its API URL.
  NEXT_PUBLIC_API_BASE_URL="" pnpm --dir "$root" --filter @firecortex/web build
fi

frontend_out="$root/apps/web/out"
if [[ ! -f "$frontend_out/index.html" ]]; then
  echo "frontend export not found at $frontend_out (run without --skip-frontend)" >&2
  exit 1
fi

embed_dir="$root/backend/internal/webui/dist"
reset_embed_dir() {
  find "$embed_dir" -mindepth 1 -maxdepth 1 ! -name .gitkeep -exec rm -rf {} +
}
trap reset_embed_dir EXIT

echo "==> Staging frontend into $embed_dir"
reset_embed_dir
cp -R "$frontend_out/." "$embed_dir/"

echo "==> Building $output ($version, $commit)"
pkg="github.com/lieyanc/FireCortex/backend/internal/shared/buildinfo"
mkdir -p "$(dirname "$output")"
(
  cd "$root/backend"
  build_args=(-trimpath -ldflags "-s -w -X $pkg.Version=$version -X $pkg.Commit=$commit -X $pkg.BuildTime=$build_time" -o "$output" ./cmd/server)
  if [[ "${FIRECORTEX_SQLITE:-0}" == "1" ]]; then
    echo "WARNING: building with -tags sqlite and CGO_ENABLED=1 (SQLite deployment is unsupported by default)." >&2
    CGO_ENABLED=1 go build -tags sqlite "${build_args[@]}"
  else
    CGO_ENABLED=0 go build "${build_args[@]}"
  fi
)
echo "==> Done: $output"
