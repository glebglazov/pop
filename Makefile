PREFIX ?= ~/.local

LDFLAGS :=
ifdef DEBUG
	LDFLAGS += -X github.com/glebglazov/pop/debug.defaultLogPath=$(HOME)/.local/share/pop/debug.log
endif

build:
	go build -ldflags "$(LDFLAGS)" -o pop ./

install: build
	cp -f pop $(PREFIX)/bin/pop
	codesign --force --sign - $(PREFIX)/bin/pop

test:
	go test ./...

.PHONY: build install test
