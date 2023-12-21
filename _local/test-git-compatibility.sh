#!/usr/bin/env bash
PLATFORM=$(uname -s | tr '[[:upper:]]' '[[:lower:]]')
GIT_DIST_PATH="/git/src"
GIT_REPOSITORY="${GIT_REPOSITORY:-http://github.com/git/git.git}"
GIT_VERSION="${GIT_VERSION:-master}"
GO_VERSION=${2:-$(go env GOVERSION | sed -e "s/[A-Za-z]*//" | sed -e "s/\.[0-9]*$//")}
WORKDIR=${WORKDIR:-$(git rev-parse --show-toplevel)}
DOCKER_ENV=${DOCKER_ENV:-$WORKDIR/build/.env-docker}

# shellcheck disable=SC1090
source "$WORKDIR/_local/commons.sh"

function usage() {
    cat << EOF
Usage: $(basename "$0") git-ref [go-version]
Runs \`make build-git\` and\`make test\` for the given git-ref on go-version.

Required:
    git-ref         Must be a git tag or branch which exits on the git repository: $GIT_REPOSITORY
    go-version      Should be formatted like "1.xx", defaults to \$(go env GOVERSION).
EOF
}

if [ -z "$1" ]; then
    printf "error: missing required git ref\n\n"
    usage && exit 1
fi

if [ -z "$GO_VERSION" ]; then
    printf "error: missing required go version\n\n"
    usage && exit 1
elif ! [[ $GO_VERSION =~ 1\.[0-9]* ]]; then
    printf "error: invalid go version provided, '%s', please use format '1.xx'\n" "$GO_VERSION"
    usage && exit 1
fi

gitref=$1
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

preDockerCommands "$tag"

if [ "$(docker volume ls -q --filter "name=$DOCKER_GOGIT_NAME")" == "" ]; then
    echo "Creating git distribution volume"
    docker volume create --label "$DOCKER_GOGIT_KEY=$DOCKER_GOGIT_LABEL" "$DOCKER_GOGIT_NAME"
else
    echo "Git distribution volume, already exits"
fi

if ! buildDockerImage "${WORKDIR}" "$image" "$tag" "$locuser" "$uid" "$gid"; then
    exit 1
fi

# exit if build-git fails
set -e

cat <<EOF >"$DOCKER_ENV"
COVERAGE_REPORT="/go/src/build/coverage-${GO_VERSION}.out"
GIT_VERSION=$gitref
GIT_DIST_PATH=$GIT_DIST_PATH/$gitref
GIT_EXEC_PATH=$GIT_DIST_PATH/$gitref
EOF

echo "Building git-dist: $gitref"
docker container run \
     -u root --privileged --rm \
    --workdir /go/src/ \
    -v "${WORKDIR}:/go/src" \
    -v "$DOCKER_GOGIT_NAME:$GIT_DIST_PATH" \
    --env-file "${DOCKER_ENV}" \
    --label "$DOCKER_GOGIT_KEY=$DOCKER_GOGIT_LABEL" \
    "$image" /build-git.sh

docker container run \
    -u "$locuser" --rm \
    --workdir /go/src/ \
    -v "${WORKDIR}:/go/src" \
    -v "$DOCKER_GOGIT_NAME:$GIT_DIST_PATH" \
    --env-file "${DOCKER_ENV}" \
    --label "$DOCKER_GOGIT_KEY=$DOCKER_GOGIT_LABEL" \
    "$image" bash -c "git --exec-path && make test"
