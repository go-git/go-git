#!/usr/bin/env bash
DOCKER_ENV=${DOCKER_ENV:-.env-docker}
GO_VERSIONS="${GO_VERSIONS-(1.19 1.20 1.21)}"
WORKDIR=${WORKDIR:-$(git rev-parse --show-toplevel)}

# shellcheck disable=SC1091
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

# additional checks
checkDocker

gover=$1
tag="$gover-bullseye"
image="go-git:$tag"
locuser=$(id -n -u)
uid=$(id -u)
gid=$(id -g)

cat <<EOF >"$DOCKER_ENV"
COVERAGE_REPORT="coverage-${gover}.out"
EOF

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