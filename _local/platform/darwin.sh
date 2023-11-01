#!/usr/bin/env bash

### BEGIN Platform Specific Section

function preDockerCommands() {
    local tag=$1
    # Docker Desktop for Darwin is stupid, and doesn't read FROM image
    # Also fails with confusing "denied: request access" in addition to error
    # when build fails, instead of just error.
    if [ "$(docker image ls -q --filter "reference=golang:$tag")" == "" ]; then
        docker image pull "golang:$tag"
    fi
}

### END Platform Specific Section

VERSION=$(bash --version | grep version)
if echo "$VERSION" | grep -Eq "version ([34]\.)"; then
    cat << EOF
error: invalid version of bash, must be 5.x or greater

In order to run local CI tests, please install a modern version of bash.
If you use 'brew' you can do the following:
    brew install bash

Please consult documentation of your desired method, if you use an alternative to managing your environment.
EOF
    usage && exit 1
fi