
OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)

LDFLAGS := -s -w

DATE_PREFIX := $(shell date +%y%m%d)
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo 'dev')
VERSION ?= $(DATE_PREFIX).$(GIT_VERSION)
LDFLAGS += -X main.Version=$(VERSION)


build:
	cd cmd/onepkg && env CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags "$(LDFLAGS)" -o onepkg .
	@echo

install: build
	@mkdir -p $(HOME)/.local/bin
	cp -f cmd/onepkg/onepkg $(HOME)/.local/bin
	@echo
