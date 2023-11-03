# General
WORKDIR = $(PWD)
BASHCMD = /usr/bin/env bash

# Go parameters
GOCMD = go
GOTEST = $(GOCMD) test 

# Git config
GIT_VERSION ?=
GIT_DIST_PATH ?= $(PWD)/.git-dist
GIT_REPOSITORY = http://github.com/git/git.git

# Coverage
COVERAGE_REPORT ?= coverage.out
COVERAGE_MODE = count

# https://www.gnu.org/software/make/manual/html_node/One-Shell.html
# Fix for cat <<EOF >.env, include multiline command
.ONESHELL:

build-git:
	@if [ -f $(GIT_DIST_PATH)/git ]; then \
		echo "nothing to do, using cache $(GIT_DIST_PATH)"; \
	else \
		git clone $(GIT_REPOSITORY) -b $(GIT_VERSION) --depth 1 --single-branch $(GIT_DIST_PATH); \
		cd $(GIT_DIST_PATH); \
		make configure; \
		./configure; \
		make all; \
	fi

test:
	@echo "running against `git version`"; \
	$(GOTEST) -race ./...

TEST_ENV=.env
DOCKER_ENV=.env-docker
DOCKER_GOGIT_KEY = go-git
DOCKER_GOGIT_LABEL = testing
DOCKER_GOGIT_NAME = $(DOCKER_GOGIT_KEY)-dist
.PHONY: test-env test-go1.19 test-go1.20 test-go1.21 test-go-all
test-env:
	cat <<EOF >$(TEST_ENV)
	DOCKER_ENV=$(DOCKER_ENV)
	DOCKER_GOGIT_KEY=$(DOCKER_GOGIT_KEY)
	DOCKER_GOGIT_LABEL=$(DOCKER_GOGIT_LABEL)
	DOCKER_GOGIT_NAME=$(DOCKER_GOGIT_NAME)
	GIT_REPOSITORY=$(GIT_REPOSITORY)
	GIT_VERSIONS=(master v2.11.0)
	GO_VERSION_WGIT=1.21
	GO_VERSIONS=(1.19 1.20 1.21)
	WORKDIR=$(WORKDIR)
	EOF

test-go1.19: test-env
	export BASH_ENV=$(TEST_ENV)
	$(BASHCMD) _local/test-go.sh "1.19"

test-go1.20: test-env
	export BASH_ENV=$(TEST_ENV)
	$(BASHCMD) _local/test-go.sh "1.20"

test-go1.21: test-env
	BASH_ENV=$(TEST_ENV)
	$(BASHCMD) _local/test-go.sh "1.21"

test-go-all: test-go1.19 test-go1.20 test-go1.21

.PHONY: test-git-master test-git-v2.11
test-git-v2.11:
	BASH_ENV=$(TEST_ENV)
	$(BASHCMD) _local/test-git-compatability.sh "v2.11.0"

test-git-master:
	BASH_ENV=$(TEST_ENV)
	$(BASHCMD) _local/test-git-compatability.sh "master"

test-git-all: test-git-master test-git-v2.11

TEMP_REPO := $(shell mktemp)
test-sha256:
	$(GOCMD) run -tags sha256 _examples/sha256/main.go $(TEMP_REPO)
	cd $(TEMP_REPO) && git fsck
	rm -rf $(TEMP_REPO)

test-coverage:
	@echo "running against `git version`"; \
	echo "" > $(COVERAGE_REPORT); \
	$(GOTEST) -coverprofile=$(COVERAGE_REPORT) -coverpkg=./... -covermode=$(COVERAGE_MODE) ./...

.PHONY: clean clean-docker clean-all

clean:
	@if [ -d $(GIT_DIST_PATH) ]; then
		rm -rf $(GIT_DIST_PATH)
	fi
	if [ -f $(TEST_ENV) ] || [ -f $(DOCKER_ENV) ]; then
		rm $(TEST_ENV) $(DOCKER_ENV)
	fi
	rm coverage*

# doc sys prune in this case only removes dangling images, but filter it anyway
GOGIT_IMAGES := $(shell docker image ls --filter reference=go-git -q)
clean-docker:
	docker system prune -f --filter "label=$(DOCKER_GOGIT_KEY)=$(DOCKER_GOGIT_LABEL)"
	docker volume prune -f --filter "label=$(DOCKER_GOGIT_KEY)=$(DOCKER_GOGIT_LABEL)"
	docker image rm -f $(GOGIT_IMAGES)

clean-all: clean clean-docker

fuzz:
	@go test -fuzz=FuzzParser				$(PWD)/internal/revision
	@go test -fuzz=FuzzDecoder				$(PWD)/plumbing/format/config
	@go test -fuzz=FuzzPatchDelta			$(PWD)/plumbing/format/packfile
	@go test -fuzz=FuzzParseSignedBytes		$(PWD)/plumbing/object
	@go test -fuzz=FuzzDecode				$(PWD)/plumbing/object
	@go test -fuzz=FuzzDecoder				$(PWD)/plumbing/protocol/packp
	@go test -fuzz=FuzzNewEndpoint			$(PWD)/plumbing/transport
