# Tool versions are pinned so a local `make check` and CI agree.
GOFUMPT_VERSION   := v0.11.0
GOLANGCI_VERSION  := v2.12.2
DEADCODE_VERSION  := v0.49.0

# Section 4 of CLAUDE.md: files up to 400 lines. funlen, lll and nestif cover
# functions, line length and nesting; nothing covers file length, so we do.
MAX_FILE_LINES := 400

GO := go

.PHONY: check fmt filelen vet build lint test deadcode tidy-check bench clean

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

clean:
	rm -rf dist
