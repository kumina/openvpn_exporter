# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# The binary to build (just the basename).
BIN := $(shell basename $$PWD)

GOOS ?= linux
GOARCH ?= amd64

# Turn on / off go modules.
GO111MODULE = on

# Specify GOFLAGS. E.g. "-mod=vendor"
GOFLAGS =

# Where to push the docker image.
REGISTRY ?= docker.io
REGISTRY_USER ?= startersclan

###
### These variables should not need tweaking.
###

# This version-strategy uses git tags to set the version string
# Get the following from left to right: tag > branch > branch of detached HEAD commit
VERSION = $(shell git describe --tags --exact-match 2>/dev/null || git symbolic-ref -q --short HEAD 2>/dev/null || git name-rev --name-only "$$( git rev-parse --short HEAD )" | sed 's@remotes/origin/@@' | sed 's@~.*@@' )
# Get the short SHA
SHA_SHORT = $(shell git rev-parse --short HEAD)

SRC_DIRS := cmd pkg # directories which hold app source (not vendored)

ALL_PLATFORMS := linux/amd64 linux/arm linux/arm64 linux/ppc64le linux/s390x

# Used internally.  Users should pass GOOS and/or GOARCH.
OS := $(if $(GOOS),$(GOOS),$(shell go env GOOS))
ARCH := $(if $(GOARCH),$(GOARCH),$(shell go env GOARCH))

# BASEIMAGE ?= gcr.io/distroless/static

IMAGE ?= $(REGISTRY)/$(REGISTRY_USER)/$(BIN)
TAG_SUFFIX := $(OS)-$(ARCH)

BUILD_IMAGE ?= golang:1.12
SHELL_IMAGE ?= golang:1.12

PWD := $$PWD

# Build directories
BUILD_GOPATH := $(PWD)/.go
BUILD_GOCACHE := $(PWD)/.go/.cache
BUILD_BIN_DIR := $(PWD)/.go/bin
BUILD_DIR := $(PWD)/build

# Directories that we need created to build/test.
BUILD_DIRS := $(BUILD_GOPATH) \
			  $(BUILD_GOCACHE) \
			  $(BUILD_BIN_DIR) \

OUTBIN = $(BUILD_BIN_DIR)/$(BIN)_$(VERSION)_$(OS)_$(ARCH)

COVERAGE_FILE ?= $(BUILD_GOPATH)/coverage.txt

# If you want to build all binaries, see the 'all-build' rule.
# If you want to build all containers, see the 'all-container' rule.
# If you want to build AND push all containers, see the 'all-push' rule.
all: build

# For the following OS/ARCH expansions, we transform OS/ARCH into OS_ARCH
# because make pattern rules don't match with embedded '/' characters.

build-%:
	@$(MAKE) build \
		--no-print-directory \
		GOOS=$(firstword $(subst _, ,$*)) \
		GOARCH=$(lastword $(subst _, ,$*))

build-image-%:
	@$(MAKE) build-image \
		--no-print-directory \
		GOOS=$(firstword $(subst _, ,$*)) \
		GOARCH=$(lastword $(subst _, ,$*))

# container-%:
# 	@$(MAKE) container \
# 		--no-print-directory \
# 		GOOS=$(firstword $(subst _, ,$*)) \
# 		GOARCH=$(lastword $(subst _, ,$*))

push-%:
	@$(MAKE) push-image \
		--no-print-directory \
		GOOS=$(firstword $(subst _, ,$*)) \
		GOARCH=$(lastword $(subst _, ,$*))

all-build: $(addprefix build-, $(subst /,_, $(ALL_PLATFORMS)))

# all-container: $(addprefix container-, $(subst /,_, $(ALL_PLATFORMS)))
all-build-image: $(addprefix build-image-, $(subst /,_, $(ALL_PLATFORMS)))

all-push: $(addprefix push-, $(subst /,_, $(ALL_PLATFORMS)))

# Mounts a ramdisk on ./go/bin
mount-ramdisk:
	@mkdir -p $(BUILD_BIN_DIR)
	@mount | grep $(BUILD_BIN_DIR) && echo "tmpfs already mounted on $(BUILD_BIN_DIR)" || ( sudo mount -t tmpfs -o size=128M tmpfs $(BUILD_BIN_DIR) && mount | grep $(BUILD_BIN_DIR) && echo "tmpfs mounted on $(BUILD_BIN_DIR)" )

# Unmounts a ramdisk on ./go/bin
unmount-ramdisk:
	@mount | grep $(BUILD_BIN_DIR) && sudo umount $(BUILD_BIN_DIR) && echo "unmount $(BUILD_BIN_DIR)" || echo "nothing to unmount on $(BUILD_BIN_DIR)"

build: $(OUTBIN)

# The following structure defeats Go's (intentional) behavior to always touch
# result files, even if they have not changed.  This will still run `go` but
# will not trigger further work if nothing has actually changed.
# $(OUTBIN): .go/$(OUTBIN).stamp
# 	@true

# This will build the binary under ./.go and update the real binary iff needed.
#.PHONY: .go/$(OUTBIN).stamp
#.go/$(OUTBIN).stamp: $(BUILD_DIRS)
$(OUTBIN): $(BUILD_DIRS)
	@echo "making $(OUTBIN)"
	@docker run \
		-i \
		--rm \
		-u $$(id -u):$$(id -g) \
		-v $(PWD):$(PWD) \
		-w $(PWD) \
		-v $(BUILD_GOPATH):/go \
		-v $(BUILD_GOCACHE):/.cache \
		--env HTTP_PROXY=$(HTTP_PROXY) \
		--env HTTPS_PROXY=$(HTTPS_PROXY) \
		$(BUILD_IMAGE) \
		/bin/sh -c " \
			ARCH=$(ARCH) \
			OS=$(OS) \
			GO111MODULE=$(GO111MODULE) \
			GOFLAGS=$(GOFLAGS) \
			OUTBIN=$(OUTBIN) \
			VERSION=$(VERSION) \
			COMMIT_SHA1=$(SHA_SHORT) \
			BUILD_DATE=$(shell date -u '+%Y-%m-%dT%H:%M:%S%z') \
			./build/build.sh \
		";
#	@if ! cmp -s .go/$(OUTBIN) $(OUTBIN); then \
		mv .go/$(OUTBIN) $(OUTBIN); \
		date >$@; \
	fi

build-image: $(BUILD_DIRS)
	@echo "IMAGE: $(IMAGE)"
	@echo "VERSION: $(VERSION)"
	@echo "SHA_SHORT: $(SHA_SHORT)"
	@docker build \
		--build-arg "BUILD_IMAGE=$(BUILD_IMAGE)" \
		--build-arg "BUILD_DIR=$(BUILD_DIR)" \
		--build-arg "ARCH=$(ARCH)" \
		--build-arg "OS=$(OS)" \
		--build-arg "GO111MODULE=$(GO111MODULE)" \
		--build-arg "GOFLAGS=$(GOFLAGS)" \
		--build-arg "OUTBIN=$(OUTBIN)" \
		--build-arg "VERSION=$(VERSION)" \
		--build-arg "COMMIT_SHA1=$(SHA_SHORT)" \
		--build-arg "BUILD_DATE=$(shell date -u '+%Y-%m-%dT%H:%M:%S%z')" \
		--build-arg "PWD=$(PWD)" \
		--build-arg "BIN=$(BIN)" \
		--tag "$(IMAGE):$(VERSION)" \
		--tag "$(IMAGE):$(VERSION)-$(TAG_SUFFIX)" \
		--tag "$(IMAGE):$(VERSION)-$(SHA_SHORT)-$(TAG_SUFFIX)" \
		--tag "$(IMAGE):latest" \
		--file "$(BUILD_DIR)/Dockerfile" \
		"."
	@docker history --no-trunc "$(IMAGE):latest"

push-image: $(BUILD_DIRS)
	@docker push "$(IMAGE):$(VERSION)"
	@docker push "$(IMAGE):$(VERSION)-$(TAG_SUFFIX)"
	@docker push "$(IMAGE):$(VERSION)-$(SHA_SHORT)-$(TAG_SUFFIX)"
	@docker push "$(IMAGE):latest"

# Example: make shell CMD="-c 'date > datefile'"
shell: $(BUILD_DIRS)
	@echo "launching a shell in the containerized build environment"
	@docker run \
		-ti \
		--rm \
		-u $$(id -u):$$(id -g) \
		-e GO111MODULE="$(GO111MODULE)" \
		-e GOFLAGS="$(GOFLAGS)" \
		-v $(PWD):$(PWD) \
		-w $(PWD) \
		-v $(BUILD_GOPATH):/go \
		-v $(BUILD_GOCACHE):/.cache \
		--env HTTP_PROXY=$(HTTP_PROXY) \
		--env HTTPS_PROXY=$(HTTPS_PROXY) \
		$(SHELL_IMAGE) \
		/bin/sh $(CMD)

# Used to track state in hidden files.
# DOTFILE_IMAGE = $(subst /,_,$(IMAGE))-$(TAG)

# container: .container-$(DOTFILE_IMAGE) say_container_name
# .container-$(DOTFILE_IMAGE): bin/$(OS)_$(ARCH)/$(BIN) Dockerfile.in
# 	@sed \
# 		-e 's|{ARG_BIN}|$(BIN)|g' \
# 		-e 's|{ARG_ARCH}|$(ARCH)|g' \
# 		-e 's|{ARG_OS}|$(OS)|g' \
# 		-e 's|{ARG_FROM}|$(BASEIMAGE)|g' \
# 		Dockerfile.in > .dockerfile-$(OS)_$(ARCH)
# 	@docker build -t $(IMAGE):$(TAG) -f .dockerfile-$(OS)_$(ARCH) .
# 	@docker images -q $(IMAGE):$(TAG) > $@

# say_container_name:
# 	@echo "container: $(IMAGE):$(TAG)"

# push: .push-$(DOTFILE_IMAGE) say_push_name
# .push-$(DOTFILE_IMAGE): .container-$(DOTFILE_IMAGE)
# 	@docker push $(IMAGE):$(TAG)

# say_push_name:
# 	@echo "pushed: $(IMAGE):$(TAG)"

# manifest-list: push
# 	platforms=$$(echo $(ALL_PLATFORMS) | sed 's/ /,/g'); \
# 	manifest-tool \
# 		--username=oauth2accesstoken \
# 		--password=$$(gcloud auth print-access-token) \
# 		push from-args \
# 		--platforms "$$platforms" \
# 		--template $(IMAGE):$(VERSION)__OS_ARCH \
# 		--target $(IMAGE):$(VERSION)

version:
	@echo $(VERSION)

# We replace .go and .cache with empty directories in the container
test: $(BUILD_DIRS)
	@docker run \
		-i \
		--rm \
		-u $$(id -u):$$(id -g) \
		-v $(PWD):$(PWD) \
		-w $(PWD) \
		-v $(BUILD_GOPATH):/go \
		-v $(BUILD_GOCACHE):/.cache \
		--env HTTP_PROXY=$(HTTP_PROXY) \
		--env HTTPS_PROXY=$(HTTPS_PROXY) \
		$(BUILD_IMAGE) \
		/bin/sh -c " \
			ARCH=$(ARCH) \
			OS=$(OS) \
			VERSION=$(VERSION) \
			GO111MODULE=$(GO111MODULE) \
			GOFLAGS=$(GOFLAGS) \
			COVERAGE_FILE=$(COVERAGE_FILE) \
			./build/test.sh $(SRC_DIRS) \
		"

coverage:
	@$(MAKE) test

checksums:
	@cd "$(BUILD_BIN_DIR)" && shasum -a 256 * > checksums.txt

$(BUILD_DIRS):
	@mkdir -p $@

# Development docker-compose up. Run build first
DEV_DOCKER_COMPOSE_YML := docker-compose.dev.yml
up: $(DEV_DOCKER_COMPOSE_YML)
	@$(MAKE) build
	@OUTBIN=$(OUTBIN) BIN=$(BIN) UID=$$(id -u) GID=$$(id -g) docker-compose -f $(DEV_DOCKER_COMPOSE_YML) up

# Development docker-compose down
down: $(DEV_DOCKER_COMPOSE_YML)
	@OUTBIN=$(OUTBIN) BIN=$(BIN) UID=$$(id -u) GID=$$(id -g) docker-compose -f $(DEV_DOCKER_COMPOSE_YML) down

# clean: container-clean bin-clean
clean: bin-clean

# container-clean:
# 	rm -rf .container-* .dockerfile-* .push-*

bin-clean:
	chmod -R +w $(BUILD_DIRS)
	rm -rf $(BUILD_DIRS)
