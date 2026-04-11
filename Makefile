PREFIX ?= ~/.local

LDFLAGS :=
ifdef DEBUG
	LDFLAGS += -X github.com/glebglazov/pop/debug.defaultLogPath=$(HOME)/.local/share/pop/debug.log
endif

build:
	go build -ldflags "$(LDFLAGS)" -o pop ./

install: build
	cp -f pop $(PREFIX)/bin/pop
	command -v codesign >/dev/null 2>&1 && codesign --force --sign - $(PREFIX)/bin/pop || true
	$(PREFIX)/bin/pop integrate --update-existing || true

test:
	go test ./...

.PHONY: build install test
