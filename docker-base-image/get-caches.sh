#!/usr/bin/env bash
set -euo pipefail

# --- Config (override with env) ---
IMAGE="${IMAGE:-golang:1.24-bookworm}"
PKGS="${PKGS:-gettext libcurl4-openssl-dev ca-certificates build-essential pkg-config git curl}"

# --- Paths (all under current CWD) ---
ROOT="$PWD/ci-cache"
APT_ARCHIVES="$ROOT/apt/archives"
APT_LISTS="$ROOT/apt/lists"
GO_MOD="$ROOT/go-mod"           # -> /go/pkg/mod
GO_BUILD="$ROOT/go-build"       # -> /root/.cache/go-build

# Determine repository workspace root (prefer Git toplevel)
if git -C "$PWD" rev-parse --show-toplevel >/dev/null 2>&1; then
  WORKSPACE="$(git -C "$PWD" rev-parse --show-toplevel)"
else
  WORKSPACE="$PWD"
fi

# --- Prepare dirs ---
mkdir -p "$APT_ARCHIVES/partial" "$APT_LISTS/partial" "$GO_MOD" "$GO_BUILD"

echo "==> Using image: $IMAGE"
echo "==> Writing caches to: $ROOT"
echo "==> Workspace: $WORKSPACE"

# Determine if there are go modules in the repo (host-side)
has_go_mods=false
if find "$WORKSPACE" -type f -name go.mod -not -path "*/vendor/*" -print -quit | grep -q .; then
  has_go_mods=true
fi

# Single container run to prefetch APT and warm Go caches
# APT prefetch is skipped if PKGS is empty

docker run --rm \
  -v "$WORKSPACE:/workspace" \
  -v "$APT_ARCHIVES:/var/cache/apt/archives" \
  -v "$APT_LISTS:/var/lib/apt/lists" \
  -v "$GO_MOD:/go/pkg/mod" \
  -v "$GO_BUILD:/root/.cache/go-build" \
  -e PKGS="$PKGS" \
  -w /workspace \
  "$IMAGE" bash -lc '
    set -e

    # Optionally prefetch APT packages to the mounted archives/lists
    if [ -n "${PKGS:-}" ]; then
      apt-get update
      DEBIAN_FRONTEND=noninteractive apt-get install -y --download-only --no-install-recommends ${PKGS}
    else
      echo "NOTE: PKGS is empty – skipping APT prefetch."
    fi

    # Go cache warming
    export PATH=/usr/local/go/bin:$PATH
    if ! command -v go >/dev/null 2>&1; then
      echo "ERROR: go not found in image '"$IMAGE"'" >&2
      exit 1
    fi

    export GOPATH=/go
    export GOMODCACHE=/go/pkg/mod
    export GOCACHE=/root/.cache/go-build

    echo "==> go version: $(go version)"
    echo "==> GOMODCACHE: $(go env GOMODCACHE)"
    echo "==> GOCACHE:    $(go env GOCACHE)"

    # Find all go.mod files (ignore vendor)
    mapfile -t mods < <(find /workspace -type f -name go.mod -not -path "*/vendor/*" | sort)
    if [ "${#mods[@]}" -eq 0 ]; then
      echo "NOTE: No go.mod found under /workspace – skipping Go cache warm."
      exit 0
    fi

    for m in "${mods[@]}"; do
      dir="$(dirname "$m")"
      echo "==> Warming module: ${dir#/workspace/}"
      ( cd "$dir" && go mod download )
      # Build to heat GOCACHE; ignore failures (e.g., CGO deps)
      ( cd "$dir" && CGO_ENABLED=0 go build ./... ) || true
    done

    # Show counts inside container (for logs)
    num_mod_files=$(find "$GOMODCACHE" -type f | wc -l || true)
    num_go_cache_files=$(find "$GOCACHE" -type f | wc -l || true)
    echo "==> GOMODCACHE files: ${num_mod_files}"
    echo "==> GOCACHE files:    ${num_go_cache_files}"
'

# Verify host caches contain files if there were any modules
if [ "$has_go_mods" = true ]; then
  if ! find "$GO_MOD" -type f -print -quit 2>/dev/null | grep -q .; then
    echo "ERROR: Go module cache directory '$GO_MOD' is empty after warming." >&2
    exit 1
  fi
  if ! find "$GO_BUILD" -type f -print -quit 2>/dev/null | grep -q .; then
    echo "ERROR: Go build cache directory '$GO_BUILD' is empty after warming." >&2
    exit 1
  fi
else
  echo "NOTE: No go.mod files in repo – skipping host cache verification."
fi

echo "==> Done."
echo "   APT archives: $APT_ARCHIVES"
echo "   APT lists:    $APT_LISTS"
echo "   GOMODCACHE:   $GO_MOD"
echo "   GOCACHE:      $GO_BUILD"

