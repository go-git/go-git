ARG GOVER

FROM golang:${GOVER}

USER root

RUN git config --global user.email "gha@example.com"; \
    git config --global user.name "GitHub Actions"

RUN mkdir -p  ~/.ssh; \
    ssh-keyscan -H github.com > ~/.ssh/known_hosts;
