ARG GOTAG

FROM golang:${GOTAG}

ARG LOCUSER
ARG UID
ARG GID

RUN apt update && \
    apt install -y autoconf \
        gettext \
        libcrypt-openssl* \
        libcurl4-open* \
        libexpat1* \
        libzlcore-* \
        libzltext-* \
        tcl8.6* \
        zlib1g-dev

# worktree_test.go:677:
# c.Assert(idx.Entries[0].UID, Not(Equals), uint32(0))
# User MUST NOT == uid=0(root) gid=0(root) groups=0(root)
RUN DEBIAN_FRONTEND=noninteractive; \
    addgroup --gid ${GID} --force-badname ${LOCUSER}; \
    adduser --uid ${UID} --ingroup ${LOCUSER} \
        --disabled-password --quiet --force-badname ${LOCUSER}

COPY --chown=root:root _local/* /

USER ${LOCUSER}

RUN git config --global user.email "gha@example.com"; \
    git config --global user.name "GitHub Actions"

RUN mkdir -p  ~/.ssh; \
    ssh-keyscan -H github.com > ~/.ssh/known_hosts

ENTRYPOINT [ "/entrypoint.sh" ]