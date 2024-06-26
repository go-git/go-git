#!/usr/bin/env bash

### BEGIN Platform Specific Section

### END Platform Specific Section

VERSION=$(bash --version | grep version)
if echo "$VERSION" | grep -Eq "version ([3]\.)"; then
    cat << EOF
error: invalid version of bash, must be 4.x or greater

In order to run local CI tests, please install a modern version of bash.
If you use 'brew' you can do the following:
    brew install bash

Please consult documentation of your desired method, if you use an alternative to managing your environment.
EOF
    usage && exit 1
fi
