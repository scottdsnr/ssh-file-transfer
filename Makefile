# croc-go build tasks. Everything here is plain `go`; the Makefile just names
# the common combinations.

BIN := bin/croc
PREFIX ?= $(HOME)/.local

.PHONY: all build test vet fmt smoke install clean

all: build test

build:
	go build -o $(BIN) ./cmd/croc

test: vet
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

# smoke runs a real transfer between two processes with no relay involved.
smoke: build
	./scripts/smoke.sh

install:
	./scripts/install.sh --prefix $(PREFIX)

clean:
	rm -rf bin
