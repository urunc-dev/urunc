# Copyright (c) 2023-2026, Nubificus LTD
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

# Versioning variables
COMMIT         := $(shell git describe --dirty --long --always)
VERSION        := $(shell cat $(CURDIR)/VERSION)-$(COMMIT)

# Path variables
#
# Use absolute paths just for sanity.
#? BUILD_DIR Directory to place produced binaries (default: ${CWD}/dist)
BUILD_DIR      ?= ${CURDIR}/dist
VENDOR_DIR     := ${CURDIR}/vendor
#? PREFIX Directory to install urunc and shim (default: /usr/local/bin)
PREFIX         ?= /usr/local/bin

# Architecture specific variables
#
# Do not allow building of unsupported or untested host architectures
#? ARCH Target architecture (default: Host arch)
ifeq ($(origin ARCH), undefined)
    UNAME_ARCH := $(shell uname -m)
    ifeq ($(UNAME_ARCH),x86_64)
        ARCH := amd64
    else ifeq ($(UNAME_ARCH),aarch64)
        ARCH := arm64
    else
        $(error Unsupported architecture: $(UNAME_ARCH))
    endif
endif

# Binary variables
URUNC_BIN      := $(BUILD_DIR)/urunc
SHIM_BIN       := $(BUILD_DIR)/containerd-shim-urunc-v2

# Golang variables
#? GO go binary to use (default: go)
GO             ?= go
GO_FLAGS       := GOOS=linux
CGO            := CGO_ENABLED=1
NOCGO          := CGO_ENABLED=0
TEST_FLAGS     := "-count=1"
TEST_OPTS      += -timeout 15m
BUILD_TAGS     ?= netgo osusergo

# Linking variables
LDFLAGS_COMMON   := -X main.version=$(VERSION)
LDFLAGS_EXTERNAL := -linkmode external
LDFLAGS_STATIC   := -extldflags -static
LDFLAGS_OPT      := -s -w

# Source files variables
#
# Add all urunc specific go packages as dependency for building
# or the shimAny change ina go file will result to rebuilding urunc
URUNC_SRC      := $(wildcard $(CURDIR)/cmd/urunc/*.go)
URUNC_SRC      += $(wildcard $(CURDIR)/pkg/unikontainers/*.go)
URUNC_SRC      += $(wildcard $(CURDIR)/pkg/unikontainers/hypervisors/*.go)
URUNC_SRC      += $(wildcard $(CURDIR)/pkg/unikontainers/unikernels/*.go)
URUNC_SRC      += $(wildcard $(CURDIR)/pkg/unikontainers/types/*.go)
URUNC_SRC      += $(wildcard $(CURDIR)/pkg/unikontainers/initrd/*.go)
URUNC_SRC      += $(wildcard $(CURDIR)/pkg/network/*.go)
SHIM_SRC       := $(wildcard $(CURDIR)/cmd/containerd-shim-urunc-v2/*.go)

#? CNTR_TOOL Tool to run the linter container (default: docker)
CNTR_TOOL ?= docker
CNTR_OPTS ?= run --rm -it

# Linking variables
LINT_CNTR_OPTS ?= $(CNTR_OPTS) -v $(CURDIR):/app -w /app
#? LINT_CNTR_IMG The linter image to use (default: golangci/golangci-lint:v1.53.3)
LINT_CNTR_IMG  ?= golangci/golangci-lint:v2.7
LINT_CNTR_CMD  ?= golangci-lint run -v --timeout=5m

#? DOCS_CNTR_IMG The mkdocs image to use (default: harbor.nbfc.io/nubificus/urunc/mkdocs:test)
DOCS_CNTR_IMG  ?= harbor.nbfc.io/nubificus/urunc/mkdocs:latest

# Install dependencies variables
#
# If we have already built either static or dynamic version of urunc
# we do not have to rebuild it, but instead we can use whichever
# version is available to install it. However, the dynamic version
# has always a preference.
INSTALL_DEPS   =  $(shell test -e $(URUNC_BIN)_static_$(ARCH) \
                          && echo $(URUNC_BIN)_static_$(ARCH) && exit \
                          || test -e $(URUNC_BIN)_dynamic_$(ARCH) \
                             && echo $(URUNC_BIN)_dynamic_$(ARCH) \
                             || echo $(URUNC_BIN)_static_$(ARCH))

INSTALL_DEPS   += $(shell test -e $(SHIM_BIN)_static_$(ARCH) \
                          && echo $(SHIM_BIN)_static_$(ARCH) && exit \
                          || test -e $(SHIM_BIN)_dynamic_$(ARCH) \
                             && echo $(SHIM_BIN)_dynamic_$(ARCH) \
                             || echo $(SHIM_BIN)_static_$(ARCH))

# Main Building rules
#
# By default we opt to build static binaries targeting the host architecture.
# However, we build shim as a dynamically-linked binary.

## default Build shim and urunc statically for host arch.(default).
.PHONY: default
default: static

## static Build urunc and containerd shim statically for host arch.
.PHONY: static
static: urunc_static shim_static

## dynamic Build urunc and containerd shim dynamically-linked for host arch.
.PHONY: dynamic
dynamic: urunc_dynamic shim_dynamic

## urunc_static Build urunc and containerd shim statically for host arch.
.PHONY: urunc_static
urunc_static: $(URUNC_BIN)_static_$(ARCH)

## shim_static Build containerd shim statically for host arch.
.PHONY: shim_static
shim_static: $(SHIM_BIN)_static_$(ARCH)

## urunc_dynamic Build urunc and containerd shim dynamically for host arch.
.PHONY: urunc_dynamic
urunc_dynamic: $(URUNC_BIN)_dynamic_$(ARCH)

## shim_dynamic Build containerd shim dynamically for host arch.
.PHONY: shim_dynamic
shim_dynamic: $(SHIM_BIN)_dynamic_$(ARCH)

## dynamic Build urunc and containerd shim dynamically-linked for host arch.
.PHONY: dynamic
dynamic: $(URUNC_BIN)_dynamic_$(ARCH) $(SHIM_BIN)_dynamic_$(ARCH)

## all Build shim and urunc statically for all amd64 and aarch64
.PHONY: all
all: $(SHIM_BIN)_static_arm64 $(SHIM_BIN)_static_amd64 $(URUNC_BIN)_static_amd64 $(URUNC_BIN)_static_arm64

# Just an alias for $(VENDOR_DIR) for easy invocation
## prepare Run go mod tidy, vendor, and verify.
prepare: $(VENDOR_DIR)

# Add tidy as order-only prerequisite. In that way, since tidy does not
# produce any file and executes all the time, we avoid the execution
# of $(VENDOR_DIR) rule, if the file already exists
$(VENDOR_DIR):
	$(GO) mod tidy
	$(GO) mod vendor
	$(GO) mod verify

# Add tidy and as order-only prerequisite. In that way, since tidy and
# vendor do not produce any file and execute all the time,
# we avoid the rebuilding of urunc if it has previously built and the
# source files have not changed.
$(URUNC_BIN)_static_%: $(URUNC_SRC) | prepare
	$(GO_FLAGS) GOARCH=$* $(CGO) $(GO) build \
		-tags "$(BUILD_TAGS)" \
		-ldflags "$(LDFLAGS_COMMON) $(LDFLAGS_EXTERNAL) $(LDFLAGS_STATIC) $(LDFLAGS_OPT)" \
		-o $(URUNC_BIN)_static_$* $(CURDIR)/cmd/urunc

$(URUNC_BIN)_dynamic_%: $(URUNC_SRC) | prepare
	$(GO_FLAGS) GOARCH=$* $(GO) build \
		-ldflags "$(LDFLAGS_COMMON) $(LDFLAGS_OPT)" \
		-o $(URUNC_BIN)_dynamic_$* $(CURDIR)/cmd/urunc

$(SHIM_BIN)_static_%: $(SHIM_SRC) | prepare
	@sed -i 's/DefaultCommand = "runc"/DefaultCommand = "urunc"/g' \
		$(VENDOR_DIR)/github.com/containerd/go-runc/runc.go
	GOARCH=$* $(NOCGO) $(GO) build \
		-ldflags "$(LDFLAGS_STATIC) $(LDFLAGS_OPT)" \
		-o $(SHIM_BIN)_static_$* $(CURDIR)/cmd/containerd-shim-urunc-v2

$(SHIM_BIN)_dynamic_%: $(SHIM_SRC) | prepare
	@sed -i 's/DefaultCommand = "runc"/DefaultCommand = "urunc"/g' \
		$(VENDOR_DIR)/github.com/containerd/go-runc/runc.go
	GOARCH=$* $(GO) build \
		-o $(SHIM_BIN)_dynamic_$* $(CURDIR)/cmd/containerd-shim-urunc-v2

## install Install urunc and shim in PREFIX
.PHONY: install
install: $(INSTALL_DEPS)
	install -D -m0755 $(word 1,$^) $(PREFIX)/urunc
	install -D -m0755 $(word 2,$^) $(PREFIX)/containerd-shim-urunc-v2

## uninstall Remove urunc and shim from PREFIX
.PHONY: uninstall
uninstall:
	rm -f $(PREFIX)/urunc
	rm -f $(PREFIX)/containerd-shim-urunc-v2

## distclean Remove build and vendor directories
.PHONY: distclean
distclean: clean
	rm -fr $(VENDOR_DIR)

## clean build directory
.PHONY: clean
clean:
	rm -fr $(BUILD_DIR)

# Linting targets
## lint Run the lint test using a golang container
.PHONY: lint
lint:
	$(CNTR_TOOL) $(LINT_CNTR_OPTS) $(LINT_CNTR_IMG) $(LINT_CNTR_CMD)

# Dcos targets
## docs Build and serve urunc's docs locally
.PHONY: docs
docs:
	mkdocs serve

## docs_container Build and serve urunc's docs locally using a container
.PHONY: docs_container
docs_container:
	$(CNTR_TOOL) $(CNTR_OPTS) -p 8000:8000 -v $${PWD}:/docs $(DOCS_CNTR_IMG)

# Testing targets
## test Run all tests
.PHONY: test
test: unittest e2etest

## unittest Run all unit tests
.PHONY: unittest
unittest: test_unikontainers test_metrics test_network test_hypervisors

## e2etest Run all end-to-end tests
.PHONY: e2etest
e2etest: test_nerdctl test_ctr test_crictl test_docker

## test_unikontainers Run unit tests for unikontainers package
test_unikontainers:
	@echo "Unit testing in unikontainers"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./pkg/unikontainers -v
	@echo " "

## test_metrics Run unit tests for metrics package
test_metrics:
	@echo "Unit testing in internal/metrics"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./internal/metrics -v
	@echo " "

## test_network Run unit tests for network package
test_network:
	@echo "Unit testing in pkg/network"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./pkg/network -v
	@echo " "

## test_hypervisors Run unit tests for hypervisors package
test_hypervisors:
	@echo "Unit testing in hypervisors"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./pkg/unikontainers/hypervisors -v
	@echo " "

## test_nerdctl Run all end-to-end tests with nerdctl
.PHONY: test_nerdctl
test_nerdctl:
	@echo "Testing nerdctl"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./tests/e2e -run TestNerdctl -v
	@echo " "

## test_ctr Run all end-to-end tests with ctr
.PHONY: test_ctr
test_ctr:
	@echo "Testing ctr"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./tests/e2e -run TestCtr -v --ginkgo.v
	@echo " "

## test_crictl Run all end-to-end tests with crictl
.PHONY: test_crictl
test_crictl:
	@echo "Testing crictl"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./tests/e2e -run TestCrictl -v
	@echo " "

## test_docker Run all end-to-end tests with docker
.PHONY: test_docker
test_docker:
	@echo "Testing docker"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./tests/e2e -run TestDocker -v
	@echo " "

## test_nerdctl_[pattern] Run all end-to-end tests with nerdctl that match pattern
.PHONY: test_nerdctl_%
test_nerdctl_%:
	@echo "Testing nerdctl"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./tests/e2e -v -run "TestNerdctl/$*"
	@echo " "

## test_ctr_[pattern] Run all end-to-end tests with ctr that match pattern
.PHONY: test_ctr_%
test_ctr_%:
	@echo "Testing ctr"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./tests/e2e -v --ginkgo.v -run TestCtr --ginkgo.focus="$*"
	@echo " "

## test_crictl_[pattern] Run all end-to-end tests with crictl that match pattern
.PHONY: test_crictl_%
test_crictl_%:
	@echo "Testing crictl"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./tests/e2e -v -run "TestCrictl/$*"
	@echo " "

## test_docker_[pattern] Run all end-to-end tests with docker that match pattern
.PHONY: test_docker_%
test_docker_%:
	@echo "Testing docker"
	@GOFLAGS=$(TEST_FLAGS) $(GO) test $(TEST_OPTS) ./tests/e2e -v -run "TestDocker/$*"
	@echo " "

## help Show this help message
help:
	@echo 'Usage: make <target> <flags>'
	@echo 'Targets:'
	@grep -w "^##" $(MAKEFILE_LIST) | sed -n 's/^## /\t/p' | sed -n 's/ /\@/p' | column -s '\@' -t
	@echo 'Flags:'
	@grep -w "^#?" $(MAKEFILE_LIST) | sed -n 's/^#? /\t/p' | sed -n 's/ /\@/p' | column -s '\@' -t
