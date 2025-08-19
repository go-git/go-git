# Dockerfile
# Match the image you used in get-caches.sh
FROM golang:1.24-bookworm

# Keep Go in PATH and set default cache locations
ENV PATH=/usr/local/go/bin:$PATH \
    GOPATH=/go \
    GOMODCACHE=/go/pkg/mod \
    GOCACHE=/root/.cache/go-build \
    GO111MODULE=on

# --- Copy APT caches produced by scripts/get-caches.sh ---
# Expecting:
#   ./ci-cache/apt/lists
#   ./ci-cache/apt/archives
COPY ci-cache/apt/lists     /var/lib/apt/lists
COPY ci-cache/apt/archives  /var/cache/apt/archives

# Install strictly from cached .debs (no network). If anything is missing,
# this will fail — rerun scripts/get-caches.sh and rebuild.
RUN DEBIAN_FRONTEND=noninteractive \
    apt-get install -y --no-install-recommends --no-download \
      gettext libcurl4-openssl-dev ca-certificates build-essential pkg-config git curl \
 && rm -rf /var/lib/apt/lists/*

# --- Copy Go caches produced by scripts/get-caches.sh ---
# Expecting:
#   ./ci-cache/go-mod   -> /go/pkg/mod
#   ./ci-cache/go-build -> /root/.cache/go-build
RUN mkdir -p /go/pkg/mod /root/.cache/go-build
COPY ci-cache/go-mod/      /go/pkg/mod/
COPY ci-cache/go-build/    /root/.cache/go-build/

WORKDIR /workspace
