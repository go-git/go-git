# General
WORKDIR = $(PWD)
BASHCMD = /usr/bin/env bash

# Go parameters
GOCMD = go
GOTEST = $(GOCMD) test 
GO_VERSION ?= $(go env GOVERSION)

# Git config
GIT_VERSION ?= master
GIT_DIST_PATH ?= $(PWD)/.git-dist
GIT_REPOSITORY = http://github.com/git/git.git

# Coverage
COVERAGE_REPORT ?= coverage.out
COVERAGE_MODE = count

# https://www.gnu.org/software/make/manual/html_node/One-Shell.html
# Fix for cat <<EOF >.env, include multiline command
.ONESHELL:

.PHONY: build-git clean test test-coverage test-env test-go test-git test-sha256
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

TEST_DIR = build
TEST_ENV = $(TEST_DIR)/.env
DOCKER_ENV = $(TEST_DIR)/.env-docker
test-env:
	@mkdir -p $(TEST_DIR); \
	cat <<EOF >$(TEST_ENV)
	DOCKER_ENV=$(DOCKER_ENV)
	GIT_REPOSITORY=$(GIT_REPOSITORY)
	GIT_VERSION=$(GIT_VERSION)
	GO_VERSION=$(GO_VERSION)
	WORKDIR=$(WORKDIR)
	EOF

test-go: test-env
	export BASH_ENV=$(TEST_ENV)
	$(BASHCMD) _local/test-go.sh $(GO_VERSION)

test-git: test-env
	export BASH_ENV=$(TEST_ENV)
	$(BASHCMD) _local/test-git-compatability.sh $(GIT_VERSION)

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
	rm -rf $(GIT_DIST_PATH) $(TEST_DIR)

fuzz:
	@go test -fuzz=FuzzParser				$(PWD)/internal/revision
	@go test -fuzz=FuzzDecoder				$(PWD)/plumbing/format/config
	@go test -fuzz=FuzzPatchDelta			$(PWD)/plumbing/format/packfile
	@go test -fuzz=FuzzParseSignedBytes		$(PWD)/plumbing/object
	@go test -fuzz=FuzzDecode				$(PWD)/plumbing/object
	@go test -fuzz=FuzzDecoder				$(PWD)/plumbing/protocol/packp
	@go test -fuzz=FuzzNewEndpoint			$(PWD)/plumbing/transport
