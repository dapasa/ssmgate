# ssmgate — build & cross-compile
#
# Single static Go binary, no cgo, for every major platform
# (Windows, Linux, macOS on amd64 + arm64). `make dist` produces all of them under
# ./dist. Requires the session-manager-plugin at runtime (see README).

BINARY      := ssmgate
PKG         := github.com/dapasa/ssmgate
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
DIST        := dist

# GOOS/GOARCH matrix. darwin/arm64 = Apple Silicon; windows binaries get .exe.
PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64

.PHONY: all build install fmt vet test tidy clean dist $(PLATFORMS)

all: fmt vet build

## build: compile for the host platform into ./$(BINARY)
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) .

## install: install into $GOBIN / $GOPATH/bin
install:
	CGO_ENABLED=0 go install -trimpath -ldflags '$(LDFLAGS)' .

## fmt: format all sources
fmt:
	gofmt -w .

## vet: static analysis
vet:
	go vet ./...

## test: run unit tests
test:
	go test ./...

## tidy: sync go.mod/go.sum
tidy:
	go mod tidy

## clean: remove build artifacts
clean:
	rm -rf $(BINARY) $(DIST)

## dist: cross-compile every platform in the matrix into ./$(DIST)
dist: $(PLATFORMS)

$(PLATFORMS):
	@os=$(word 1,$(subst /, ,$@)); arch=$(word 2,$(subst /, ,$@)); \
	ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	out=$(DIST)/$(BINARY)-$$os-$$arch$$ext; \
	echo "building $$out"; \
	CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
		go build -trimpath -ldflags '$(LDFLAGS)' -o $$out .
