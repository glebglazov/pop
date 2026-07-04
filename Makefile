PREFIX ?= ~/.local

# CalVer version string (ADR 0014): the latest vYYYY.M.N tag, plus
# commits-since and short SHA between releases, "-dirty" with uncommitted
# changes. Falls back to the bare SHA before the first tag exists.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)

LDFLAGS := -X github.com/glebglazov/pop/cmd.version=$(VERSION)
ifdef DEBUG
	LDFLAGS += -X github.com/glebglazov/pop/debug.defaultLogPath=$(HOME)/.local/share/pop/debug.log
endif

build:
	go build -ldflags "$(LDFLAGS)" -o pop ./

install: build
	cp -f pop $(PREFIX)/bin/pop
	command -v codesign >/dev/null 2>&1 && codesign --force --sign - $(PREFIX)/bin/pop || true
	$(PREFIX)/bin/pop integrate --update-existing || true

install-dev:
	$(MAKE) install DEBUG=1

test:
	go test ./...

live-agent-smoke:
	@if [ -z "$(AGENTS)" ]; then \
		echo 'usage: make live-agent-smoke AGENTS="codex claude"'; \
		exit 64; \
	fi
	scripts/live-agent-smoke.sh $(AGENTS)

.PHONY: build install install-dev test live-agent-smoke
