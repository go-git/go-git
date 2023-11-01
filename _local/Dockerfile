ARG GOTAG

FROM golang:${GOTAG}

ARG LOCUSER
ARG UID
ARG GID

# worktree_test.go:677:
# c.Assert(idx.Entries[0].UID, Not(Equals), uint32(0))
# User MUST NOT == uid=0(root) gid=0(root) groups=0(root)
RUN echo "$UID $GID $LOCUSER"; \
    addgroup --gid $GID $LOCUSER; \
    adduser --uid ${UID} --ingroup ${LOCUSER} --disabled-password ${LOCUSER}

USER ${LOCUSER}

RUN git config --global user.email "gha@example.com"; \
    git config --global user.name "GitHub Actions"

RUN mkdir -p  ~/.ssh; \
    ssh-keyscan -H github.com > ~/.ssh/known_hosts
