APP := pve
VERSION ?= v$(shell date +%Y%m%d)-$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"
GOENV := CGO_ENABLED=0
SHA256SUM := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo "shasum -a 256")

.PHONY: all build dist install uninstall fmt check test test-race docs docs-check completions completions-check validate clean help

all: fmt check test build docs-check

build:
	$(GOENV) go build $(LDFLAGS) -o bin/$(APP) ./main.go

dist:
	@mkdir -p dist
	@set -eu; for goos in linux darwin windows; do \
		for goarch in amd64 arm64; do \
			output="dist/$(APP)-$(VERSION)-$${goos}-$${goarch}"; \
			if [ "$$goos" = "windows" ]; then output="$$output.exe"; fi; \
			echo "Building $$output"; \
			$(GOENV) GOOS=$$goos GOARCH=$$goarch go build $(LDFLAGS) -o "$$output" ./main.go; \
			$(SHA256SUM) "$$output" > "$$output.sha256"; \
		done; \
	done

install: build
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 "bin/$(APP)" "$(DESTDIR)$(BINDIR)/$(APP)"

uninstall:
	rm -f "$(DESTDIR)$(BINDIR)/$(APP)"

fmt:
	go fmt ./...

check:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

clean:
	rm -rf bin dist coverage.out

help:
	@echo "make build  - build binary"
	@echo "make dist   - build release binaries"
	@echo "make install - install binary"
	@echo "make uninstall - remove installed binary"
	@echo "make fmt    - format code"
	@echo "make check  - run go vet"
	@echo "make test   - run tests"
	@echo "make test-race - run tests with the race detector"
	@echo "make docs   - regenerate command and schema references"
	@echo "make docs-check - verify generated references"
	@echo "make completions - generate shell scripts"
	@echo "make validate - run the release gate"
	@echo "make clean  - clean generated artifacts"

docs:
	go run ./scripts/docgen

docs-check:
	go run ./scripts/docgen -check

completions: build
	@mkdir -p dist/completions
	@for shell in bash zsh fish; do bin/pve completion $$shell > dist/completions/pve.$$shell; done

validate:
	$(MAKE) fmt check test test-race build docs-check

completions-check: completions
	bash -n dist/completions/pve.bash
	zsh -n dist/completions/pve.zsh
	fish --no-execute dist/completions/pve.fish
