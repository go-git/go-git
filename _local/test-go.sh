#!/usr/bin/env bash
COVERAGE_REPORT=${COVERAGE_REPORT:-coverage.out}
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
elif ! [[ "${GO_VERSIONS}" =~ $1 ]]; then
    printf "error: invalid go version provided: %s\n\n" "$1"
    usage && exit 1
fi

tag="$1-bullseye"
image="go-git-golang:$tag"

# additional checks
checkDocker

cat <<EOF >>"$DOCKER_ENV"
COVERAGE_REPORT="/tmp/${COVERAGE_REPORT}"
EOF

docker image build --build-arg GOVER="$tag" -f "${WORKDIR}/_local/Dockerfile" -t "$image" "${WORKDIR}"
docker container run -v "${WORKDIR}:/go/src" --workdir /go/src/ --rm "$image" make test-coverage