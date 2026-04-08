MODULE  := github.com/jaltamir/spotlight
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%d)

LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) \
           -X $(MODULE)/internal/version.Commit=$(COMMIT) \
           -X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: build test clean

build:
	go build -ldflags "$(LDFLAGS)" -o spotlight ./cmd/spotlight

test:
	go test ./... -cover

clean:
	rm -f spotlight
