BINARY   := codetap
GOROOT   := /opt/go
GO       := $(GOROOT)/bin/go
GOFLAGS  := CGO_ENABLED=0 GOROOT=$(GOROOT)
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)

.PHONY: help build test clean build-all fmt vet lint check
.DEFAULT_GOAL := help

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Build:"
	@echo "  build        Build binary for current platform"
	@echo "  build-all    Cross-compile linux/amd64 and linux/arm64"
	@echo "  clean        Remove built binaries"
	@echo ""
	@echo "Test & Quality:"
	@echo "  test         Run all tests"
	@echo "  fmt          Format Go source files"
	@echo "  vet          Run go vet static analysis"
	@echo "  lint         Run vet + golangci-lint"
	@echo "  check        CI gate: vet + assert gofmt-clean"

build:
	$(GOFLAGS) $(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/codetap

test:
	$(GOFLAGS) $(GO) test ./...

clean:
	rm -f $(BINARY) $(BINARY)-linux-*

build-all: build-linux-amd64 build-linux-arm64

fmt:
	$(GOFLAGS) $(GO) fmt ./...

vet:
	$(GOFLAGS) $(GO) vet ./...

lint: vet
	@if type -p golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found, skipping (install: https://golangci-lint.run/welcome/install/)"; \
	fi

check: vet
	@UNFORMATTED=$$(gofmt -l .); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "gofmt check failed. Unformatted files:"; \
		echo "$$UNFORMATTED"; \
		exit 1; \
	fi

build-linux-amd64:
	$(GOFLAGS) GOOS=linux GOARCH=amd64 $(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY)-linux-amd64 ./cmd/codetap

build-linux-arm64:
	$(GOFLAGS) GOOS=linux GOARCH=arm64 $(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY)-linux-arm64 ./cmd/codetap
