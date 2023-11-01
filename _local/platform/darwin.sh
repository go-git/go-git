#!/usr/bin/env bash

echo "Running checks for: Darwin"

VERSION=$(bash --version | grep version)
if echo "$VERSION" | grep -Eq "version ([34]\.)"; then
    cat << EOF
error: invalid version of bash, mush be 5.x or greater

In order to run local CI tests, please install a modern version of bash. If
you use 'brew' you can do the following:
    brew install bash

Please consult documentation if you perfer other methods of managing your environment.
EOF
fi