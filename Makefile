# Tool versions are pinned so a local `make check` and CI agree.
GOFUMPT_VERSION   := v0.11.0
GOLANGCI_VERSION  := v2.12.2
DEADCODE_VERSION  := v0.49.0

# Section 4 of CLAUDE.md: files up to 400 lines. funlen, lll and nestif cover
# functions, line length and nesting; nothing covers file length, so we do.
MAX_FILE_LINES := 400

GO := go

# The release artifacts. VERSION reaches the binary through -ldflags, so
# `redfirst version` answers with the tag it was built from; the asset name
# carries the same version without its leading v, which is what
# `redfirst init --ci` writes into the workflow it generates.
VERSION      ?= $(shell git describe --tags --always --dirty)
ASSET_VERSION := $(patsubst v%,%,$(VERSION))
DIST          := dist
PLATFORMS     := linux/amd64 linux/arm64 darwin/arm64
# Section 10 of the spec: one static binary, 15 MB at the most.
MAX_BINARY_BYTES := 15728640
# The timestamp every archive member carries. Any fixed value does; the epoch
# says plainly that it stands for nothing.
ARCHIVE_MTIME := 197001010000.00
LDFLAGS := -s -w -X github.com/timhartmann7/redfirst/internal/domain.Version=$(ASSET_VERSION)

.PHONY: check fmt filelen vet build lint test deadcode tidy-check bench dist clean

check: fmt filelen vet build lint test deadcode tidy-check

fmt:
	@out=$$($(GO) run mvdan.cc/gofumpt@$(GOFUMPT_VERSION) -l .); \
	if [ -n "$$out" ]; then echo "gofumpt: not formatted:"; echo "$$out"; exit 1; fi

filelen:
	@bad=$$(find . -name '*.go' -not -path './.git/*' \
	  -exec awk 'END { if (FNR > $(MAX_FILE_LINES)) print FILENAME ": " FNR " lines" }' {} \;); \
	if [ -n "$$bad" ]; then echo "over $(MAX_FILE_LINES) lines:"; echo "$$bad"; exit 1; fi

vet:
	$(GO) vet ./...

build:
	CGO_ENABLED=0 $(GO) build ./...

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./...

test:
	$(GO) test -race ./...

deadcode:
	@out=$$($(GO) run golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION) -test ./...); \
	if [ -n "$$out" ]; then echo "deadcode:"; echo "$$out"; exit 1; fi

tidy-check:
	$(GO) mod tidy
	@git diff --exit-code -- go.mod go.sum

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./...

# dist cross-compiles the three targets from section 10 of the spec.
#
# The artifacts are reproducible, and every part of that is deliberate. CGO
# stays off and -trimpath keeps the binary out of the builder's filesystem. The
# archive is what a release publishes a checksum of, so it gets the same
# treatment: touch pins the member's timestamp, --owner and --group drop the
# builder's identity, and gzip -n keeps the timestamp out of the compression
# header. Without those three the checksum moves on every build while the binary
# inside stays identical, and a checksum nobody can reproduce pins nothing.
#
# shasum rather than sha256sum, and the GNU spelling of the ownership flags,
# which bsdtar accepts too: this target has to run on the platforms it builds
# for as well as on the release runner.
dist: clean
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		stage=$(DIST)/redfirst_$(ASSET_VERSION)_$${os}_$${arch}; \
		echo "building $$stage"; \
		mkdir -p $$stage; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath \
			-ldflags '$(LDFLAGS)' -o $$stage/redfirst ./cmd/redfirst || exit 1; \
		bytes=$$(wc -c < $$stage/redfirst); \
		if [ $$bytes -gt $(MAX_BINARY_BYTES) ]; then \
			echo "$$stage/redfirst is $$bytes bytes, over the $(MAX_BINARY_BYTES) budget"; \
			exit 1; \
		fi; \
		TZ=UTC touch -t $(ARCHIVE_MTIME) $$stage/redfirst; \
		tar --create --file - --owner=0 --group=0 --numeric-owner \
			-C $$stage redfirst | gzip -n -9 > $$stage.tar.gz || exit 1; \
		rm -rf $$stage; \
	done
	@cd $(DIST) && shasum -a 256 *.tar.gz > checksums.txt
	@cat $(DIST)/checksums.txt

clean:
	rm -rf $(DIST)
