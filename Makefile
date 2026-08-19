# slivingdoc — build and test entry points.
#
# There is one Go test command and one npm test command. No target hides a
# subset of the suite behind a build tag, an environment variable, or a
# flag: `make test` runs every Go test that exists.

GO ?= go
GOFUMPT := go run mvdan.cc/gofumpt@v0.11.0
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@v0.7.0

BUILD_DIR := .build
BIN := $(BUILD_DIR)/slivingdoc
TEST_S3_LEASE := $(BUILD_DIR)/tests3-lease
TEST_S3_LEASE_SOURCES := $(shell find internal/tests3 -type f -name '*.go' -print)
COVER_PROFILE := $(BUILD_DIR)/cover.out
# The documented statement-coverage floor; 90 % is preferred.
COVER_FLOOR := 70
PKG_CONFIG_PATH := $(abspath $(BUILD_DIR)/libgit2/lib/pkgconfig)
export PKG_CONFIG_PATH

# VERSION is the release version reported by --version and used by the npm
# launcher's artifact grammar. Release pipelines override it with the
# tag-derived version; development builds keep the -dev suffix.
VERSION ?= 0.1.0-dev
VERSION_LDFLAG := -X github.com/baalimago/slivingdoc/internal/app.Version=$(VERSION)

# STATIC=1 builds a fully static binary (the musl linux release targets).
# The static libc link needs -extldflags "-static"; it is quoted because
# Go's -ldflags value is space-split, so -extldflags must receive "-static"
# as one token. The libgit2 archive must be built with the same musl-gcc
# compiler (see build-libgit2.sh) or the link aborts on the ABI mismatch.
STATIC ?=
LDFLAGS := -s -w $(VERSION_LDFLAG)
ifeq ($(STATIC),1)
LDFLAGS += -extldflags "-static"
endif

.PHONY: all libgit2 test cover npm-test lint fmt build release qa clean

all: qa

# libgit2 — build the pinned static libgit2 (idempotent; stamp on script).
libgit2: $(BUILD_DIR)/libgit2/.build-stamp

$(BUILD_DIR)/libgit2/.build-stamp: scripts/build-libgit2.sh
	./scripts/build-libgit2.sh
	touch $@

# The release-style binary. build and test share this target: test builds it
# first so the exact compile cache the release layer's in-suite build
# (release_test.go) reuses is warm. On a cold runner that in-suite build
# alone takes about 35 s — more than the strict gate's 30 s per-package
# budget — so without this prerequisite the gate can only pass on a machine
# whose cache is already warm.
$(BIN): $(BUILD_DIR)/libgit2/.build-stamp
	CGO_ENABLED=1 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $@ .

# tests3-lease — the test-only process that owns the one S3-compatible
# container used by make test. The test binaries receive only its loopback
# endpoint; direct `go test` still starts its normal per-process suite.
$(TEST_S3_LEASE): go.mod go.sum $(TEST_S3_LEASE_SOURCES)
	$(GO) build -trimpath -o $@ ./internal/tests3/lease

build: $(BIN)

# test — every Go test in the repository, against the real libgit2 boundary
# and a real SeaweedFS container, under the race detector, reporting coverage.
# The test-only lease starts one S3 service and injects it into every package
# test binary; Docker and the pinned libgit2 remain prerequisites, so no run
# can silently omit the storage protocol.
#
# Packages run concurrently. The timeout is per package, and measurement on a
# four-core runner puts the slowest package near 20 s either way, so bounding
# package parallelism bought no headroom and cost roughly 3x wall clock.
#
# Coverage uses -coverpkg=./... because the black-box scenario suite exercises
# packages other than its own; per-package coverage would understate it badly.
#
# Do not weaken -race, -count, or -timeout.
test: $(BIN) $(TEST_S3_LEASE)
	@lease_dir=$$(mktemp -d "$(abspath $(BUILD_DIR))/tests3.XXXXXX"); \
	 ready_file="$$lease_dir/endpoint"; \
	 "$(TEST_S3_LEASE)" --ready-file "$$ready_file" & lease_pid=$$!; \
	 cleanup() { \
	   kill -INT "$$lease_pid" 2>/dev/null || true; \
	   wait "$$lease_pid" 2>/dev/null || true; \
	   rm -f "$$ready_file"; \
	   rmdir "$$lease_dir" 2>/dev/null || true; \
	 }; \
	 trap cleanup EXIT; \
	 trap 'exit 130' HUP INT TERM; \
	 SLIVINGDOC_TESTS3_ENDPOINT_FILE="$$ready_file" $(GO) test -race -count=3 -timeout=30s -coverpkg=./... -coverprofile=$(COVER_PROFILE) ./...
	@total=$$($(GO) tool cover -func=$(COVER_PROFILE) | tail -1 | awk '{print $$3}'); \
	 echo "== coverage: $$total (floor $(COVER_FLOOR)%) =="; \
	 awk -v got="$${total%\%}" -v floor=$(COVER_FLOOR) 'BEGIN { \
	   if (got + 0 < floor) { printf "coverage %s%% is below the %d%% floor\n", got, floor; exit 1 } }'

# cover — open the coverage profile from the last `make test` in a browser.
cover: $(COVER_PROFILE)
	$(GO) tool cover -html=$(COVER_PROFILE)

# npm-test — the zero-dependency launcher suite. Requires Node.js; no
# package installation is needed.
npm-test:
	npm test --prefix npm/slivingdoc

# lint — formatting and static analysis. These are not tests; they are the
# other half of the local gate.
lint: $(BUILD_DIR)/libgit2/.build-stamp
	@echo "== gofumpt =="
	@files="$$($(GOFUMPT) -l .)"; test -z "$$files" || { echo "gofumpt: unformatted files:"; echo "$$files"; exit 1; }
	@echo "== go vet =="
	$(GO) vet ./...
	@echo "== staticcheck =="
	$(STATICCHECK) ./...
	@echo "== go fix =="
	@out="$$($(GO) fix -diff ./...)"; test -z "$$out" || { echo "go fix: fixes needed:"; echo "$$out"; exit 1; }

# fmt — apply gofumpt to the tree.
fmt:
	$(GOFUMPT) -w -l .

# build — release-style native binary with libgit2 linked in and the version
# injected through the linker. The release smoke checks of this wiring live
# in release_test.go and run as part of `make test`; the shared $(BIN)
# target above also warms the compile cache for that in-suite build.

# release — cut a tagged release interactively. Prints the recent
# releases, prompts for the new version and the tag description, bumps
# npm/slivingdoc/package.json, commits the bump, annotates the v<version>
# tag, and pushes branch and tag; the tag push runs the release workflow
# (native builds, GitHub release, npm publish).
release:
	./scripts/release.go

# qa — lint plus both test suites.
qa: lint test npm-test

clean:
	rm -rf $(BUILD_DIR)
