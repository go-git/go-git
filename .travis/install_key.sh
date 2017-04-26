#!/bin/bash
export SSH_TEST_PRIVATE_KEY=$PWD/.travis/deploy.pem

openssl aes-256-cbc \
    -K $encrypted_1477e58fe67a_key \
    -iv $encrypted_1477e58fe67a_iv \
    -in .travis/deploy.pem.enc \
    -out .travis/deploy.pem -d

chmod 600 .travis/deploy.pem

