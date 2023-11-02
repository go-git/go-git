#!/usr/bin/env bash
PLATFORM=$(uname -s | tr '[[:upper:]]' '[[:lower:]]')
DOCKER_ENV=${DOCKER_ENV:-.env-docker}
GO_VERSIONS="${GO_VERSIONS-(1.19 1.20 1.21)}"
WORKDIR=${WORKDIR:-$(git rev-parse --show-toplevel)}

# shellcheck disable=SC1090
source "$WORKDIR/_local/commons.sh"

function usage() {
    cat << EOF
Usage: $(basename "$0") go-version
Runs \`make test-coverage\` for the given go-version.

Required:
    go-version      can be one of: ${GO_VERSIONS[*]}
EOF
}

if [ -z "$1" ]; then
    printf "error: missing required go version\n\n"
    usage && exit 1
elif ! [[ "${GO_VERSIONS[*]}" =~ $1 ]]; then
    printf "error: invalid go version provided: %s\nnot found in %s\n" "$1" "${GO_VERSIONS[*]}"
    usage && exit 1
fi

gover=$1
tag="$gover-bullseye"
image="go-git:$tag"
locuser=$(id -n -u)
uid=$(id -u)
gid=$(id -g)

# this happen on darwin cause user primary group was staff
if [ "$gid" -lt 1000 ]; then
    # something much higher than default
    gid=1200
fi

checkDocker

# additional platform checks
echo "Running checks for: ${PLATFORM}"
PLTFM_CHECKS="$WORKDIR/_local/platform/${PLATFORM}.sh"

### BEGIN Platform Specific Section

function preDockerCommands() {
    # noop for linux
    return
}

if [ -f $PLTFM_CHECKS ]; then
    # shellcheck disable=SC1090
    source "$PLTFM_CHECKS"
fi

### END Platform Specific Section

cat <<EOF >"$DOCKER_ENV"
COVERAGE_REPORT="coverage-${gover}.out"
EOF

preDockerCommands "$tag"

# exit

docker image build \
    --build-arg GOTAG="$tag" \
    --build-arg LOCUSER="$locuser" \
    --build-arg UID="$uid" \
    --build-arg GID="$gid" \
    -f "${WORKDIR}/_local/Dockerfile" -t "$image" "${WORKDIR}"

docker container run \
    -u "$locuser" --rm \
    --workdir /go/src/ \
    -v "${WORKDIR}:/go/src" \
    --env-file "${DOCKER_ENV}" \
    "$image" make test-coverage