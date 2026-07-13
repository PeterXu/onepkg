
OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)

LDFLAGS := -s -w

DATE_PREFIX := $(shell date +%y%m%d)
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo 'dev')
VERSION ?= $(DATE_PREFIX).$(GIT_VERSION)
LDFLAGS += -X main.Version=$(VERSION)


build: win-wrapper
	cd cmd/onepkg && env CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags "$(LDFLAGS)" -o onepkg .
	@echo

win-wrapper:
	@echo
	cd cmd/onepkg/installer/stub-src && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-H windowsgui" -o ../stubs/wrapper-stub-amd64.exe .
	@echo
	cd cmd/onepkg/installer/stub-src && GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-H windowsgui" -o ../stubs/wrapper-stub-arm64.exe .
	@echo

test: win-wrapper
	cd cmd/onepkg && go test ./...
	@echo
	cd cmd/onepkg/installer/stub-src && go test
	@echo

install: build
	@mkdir -p $(HOME)/.local/bin
	cp -f cmd/onepkg/onepkg $(HOME)/.local/bin
	@echo
