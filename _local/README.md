# Running CI Tests Localy

The goal of these scripts is to facilitate running the CI Tests locally with a container, from the Makefile in the repo root. It SHOULD be a simple command like `make test`. These probably won't be added to the CI, but they MUST however mirror the CI Tests.

The specific make targets related to local CI tests are the following:

- `test-go`
- `test-git`

## Platforms

Currently only attempting to support `Linux` and `Darwin (Apple)`. At least until either someone offers to add it or we get enough requests for more.

### Darwin

*Requirements:*

- bash 4 or greater. Recommend `brew install bash`.
- make 4 or greater. Recommend `brew install make` and it will install as gmake, so you can run with that or adjust path as you desire.

## Docker Cleanup

Each image, container, and volume that we're creating is `tag`ed with the label `go-git=testing`. So if you need to filter your `docker` commands you may do with the following.

```shell
# doc sys prune in this case only removes dangling images, but filter it anyway
docker system prune -f --filter "label=go-git=testing"
docker volume prune -f --filter "label=go-git=testing"
docker image rm -f "$(docker image ls --filter reference=go-git -q)"
```
