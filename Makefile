.PHONY: fmt test build ui build-compat install-rootfs

GO ?= go
NPM ?= npm
CC_ARMHF ?= arm-linux-gnueabihf-gcc

fmt:
	$(GO) fmt ./...
	$(NPM) --prefix web run format

test:
	$(GO) test ./...
	$(NPM) --prefix web run build

build:
	$(GO) build ./cmd/dmonitor-improved ./cmd/dmonitor-install

ui:
	$(NPM) --prefix web install
	$(NPM) --prefix web run build

build-compat:
	mkdir -p runtime/rootfs/usr/lib
	$(CC_ARMHF) -shared -fPIC -O2 -Wall -Wextra -o runtime/rootfs/usr/lib/dmonitor-compat.so compat/dmonitor_compat.c -ldl

install-rootfs:
	$(GO) run ./cmd/dmonitor-install
