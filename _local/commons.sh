#!/usr/bin/env bash

function checkDocker() {
    if ! command -v docker | grep -q docker; then
        printf "error: missing required dependency, docker. \n\n"
        printf "In order to run local CI tests, please install docker. https://docs.docker.com/engine/install/ \n"
        exit 1
    fi
}