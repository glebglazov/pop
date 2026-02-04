PREFIX ?= ~/.local

build:
	go build -o pop ./

install: build
	cp -f pop $(PREFIX)/bin/pop

test:
	go test ./...

.PHONY: build install test
