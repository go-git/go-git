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
.PHONY: test-env test-go1.19 test-go1.20 test-go1.21 test-go-all
test-env:
	cat <<EOF >$(TEST_ENV)
	WORKDIR=$(WORKDIR)
	DOCKER_ENV=$(DOCKER_ENV)
	GOCMD=$(GOCMD)
	GOTEST=$(GOTEST)
	GO_VERSIONS=(1.19 1.20 1.21)
	GIT_DIST_PATH=$(GIT_DIST_PATH)
	GIT_REPOSITORY=$(GIT_REPOSITORY)
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

test-git-master:

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

clean:
	rm -rf $(GIT_DIST_PATH)
	rm $(TEST_ENV) $(DOCKER_ENV)
	rm coverage.*

fuzz:
	@go test -fuzz=FuzzParser				$(PWD)/internal/revision
	@go test -fuzz=FuzzDecoder				$(PWD)/plumbing/format/config
	@go test -fuzz=FuzzPatchDelta			$(PWD)/plumbing/format/packfile
	@go test -fuzz=FuzzParseSignedBytes		$(PWD)/plumbing/object
	@go test -fuzz=FuzzDecode				$(PWD)/plumbing/object
	@go test -fuzz=FuzzDecoder				$(PWD)/plumbing/protocol/packp
	@go test -fuzz=FuzzNewEndpoint			$(PWD)/plumbing/transport
