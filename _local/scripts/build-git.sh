#!/bin/bash
set -e

GIT_ARCHIVE="${GIT_VERSION}.tar.gz"
GIT_SRC_ROOT=$(dirname "${GIT_DIST_PATH}")
GIT_URL=https://github.com/git/git/archive/refs/tags
if [[ ${GIT_VERSION} =~ master ]]; then
    GIT_URL=https://github.com/git/git/archive/refs/heads
fi
GIT_URL="${GIT_URL}/${GIT_ARCHIVE}"

# git clone issues:
#   - fails in container with following due to incomplete support from git in container:
#       Cloning into '/git/src'...
#       git: 'remote-https' is not a git command. See 'git --help'.
#   - while using ssh git@github.com:git/git.git:
#       Permission denined (publickey)
#   - apt update && apt reinstall -y git; git clone fails with ... (publickey)
if [ -f "${GIT_DIST_PATH}/git" ]; then
    echo "nothing to do, using cache ${GIT_DIST_PATH}"
else
    # git clone "${GIT_REPOSITORY}" -b "${GIT_VERSION}" --depth 1 --single-branch "${GIT_DIST_PATH}"; \
    if ! [ -d "${GIT_DIST_PATH}" ]; then
        curl -L -o "${GIT_SRC_ROOT}/${GIT_VERSION}.tar.gz" "${GIT_URL}"
        mkdir -p "${GIT_DIST_PATH}"
    fi
    tar -xf "${GIT_SRC_ROOT}/${GIT_ARCHIVE}" --strip-components 1 -C "${GIT_DIST_PATH}"
    cd "${GIT_DIST_PATH}"
    make configure
    ./configure
    make all
fi
