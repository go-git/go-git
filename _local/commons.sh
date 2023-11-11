#!/usr/bin/env bash

# Docker objects label --lable go-git=testing
export DOCKER_GOGIT_KEY=go-git
export DOCKER_GOGIT_LABEL=testing
export DOCKER_GOGIT_NAME="$DOCKER_GOGIT_KEY-dist"

function buildDir() {
    if ! [ -d "$WORKDIR/build" ]; then
        mkdir -p "$WORKDIR/build"
    fi
}

function buildDockerImage() {
    local srcdir=$1 image=$2 gotag=$3 user=$4 uid=$5 gid=$6

    local ARGS=(image build \
        --build-arg "GOTAG=$gotag" \
        --build-arg "LOCUSER=$user" \
        --build-arg "UID=$uid" \
        --build-arg "GID=$gid" \
        --label "$DOCKER_GOGIT_KEY=$DOCKER_GOGIT_LABEL"
        -f "${srcdir}/_local/Dockerfile" -t "$image" "${srcdir}")

    if ! docker "${ARGS[@]}"; then
        return 1
    fi
    return 0
}

function checkDocker() {
    if ! command -v docker | grep -q docker; then
        printf "error: missing required dependency, docker. \n\n"
        printf "In order to run local CI tests, please install docker. https://docs.docker.com/engine/install/ \n"
        exit 1
    fi
}

function patchGID() {
    local gid=$1
    # Fix case when on darwin, cause user primary group was "staff"
    if [ "$gid" -lt 1000 ]; then
        # something much higher than default
        gid=1200
    fi
    echo "$gid"
}

function preDockerCommands() {
    local tag=$1
    # Docker Desktop for Darwin fails when trying to use an image that isn't available locally. Therefore, we first check if it exists in the machine and preemptively pull it if not.
    if [ "$(docker image ls -q --filter "reference=golang:$tag")" == "" ]; then
        docker image pull "golang:$tag"
    fi
}
