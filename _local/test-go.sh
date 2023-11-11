#!/usr/bin/env bash
PLATFORM=$(uname -s | tr '[[:upper:]]' '[[:lower:]]')
WORKDIR=${WORKDIR:-$(git rev-parse --show-toplevel)}
DOCKER_ENV=${DOCKER_ENV:-$WORKDIR/build/.env-docker}
GO_VERSION=${1:-$(go env GOVERSION | sed -e "s/[A-Za-z]*//" | sed -e "s/\.[0-9]*$//")}

# shellcheck disable=SC1090
source "$WORKDIR/_local/commons.sh"

function usage() {
    cat << EOF
Usage: $(basename "$0") [go-version]
Runs \`make test-coverage\` for the given go-version.

Required:
    go-version      Should be formatted like "1.xx", defaults to \$(go env GOVERSION).
EOF
}

if [ -z "$GO_VERSION" ]; then
    printf "error: missing required go version\n\n"
    usage && exit 1
elif ! [[ $GO_VERSION =~ 1\.[0-9]* ]]; then
    printf "error: invalid go version provided, '%s', please use format '1.xx'\n" "$GO_VERSION"
    usage && exit 1
fi

tag="$GO_VERSION-bullseye"
image="go-git:$tag"
locuser=$(id -n -u)
uid=$(id -u)
gid=$(id -g)

# run common checks
buildDir
checkDocker
gid=$(patchGID "$gid")

# additional platform checks
echo "Running checks for: ${PLATFORM}"
PLTFM_CHECKS="$WORKDIR/_local/platform/${PLATFORM}.sh"

### BEGIN Platform Specific Section

if [ -f $PLTFM_CHECKS ]; then
    # shellcheck disable=SC1090
    source "$PLTFM_CHECKS"
fi

### END Platform Specific Section

cat <<EOF >"$DOCKER_ENV"
COVERAGE_REPORT="/go/src/build/coverage-${GO_VERSION}.out"
EOF

preDockerCommands "$tag"

if ! buildDockerImage "${WORKDIR}" "$image" "$tag" "$locuser" "$uid" "$gid"; then
    exit 1
fi

docker container run \
    -u "$locuser" --rm \
    --workdir /go/src/ \
    -v "${WORKDIR}:/go/src" \
    --env-file "${DOCKER_ENV}" \
    --label "$DOCKER_GOGIT_KEY=$DOCKER_GOGIT_LABEL" \
    "$image" make test-coverage
