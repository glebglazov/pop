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

install-dev:
	$(MAKE) install DEBUG=1

test:
	go test ./...

live-agent-smoke:
	@if [ -z "$(AGENTS)" ]; then \
		echo 'usage: make live-agent-smoke AGENTS="codex claude"'; \
		exit 64; \
	fi
	scripts/live-workload-agent-smoke.sh $(AGENTS)

.PHONY: build install install-dev test live-agent-smoke
