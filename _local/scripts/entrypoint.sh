#!/bin/bash

if [ "${GIT_EXEC_PATH}" != "" ]; then
    export PATH="$GIT_EXEC_PATH:$PATH"
fi

exec "$@"
