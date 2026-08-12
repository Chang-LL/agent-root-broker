VERSION ?= dev
GOCACHE ?= /tmp/hostctl-go-cache
GOPATH ?= /tmp/hostctl-go-path
GO_ENV = GOCACHE=$(GOCACHE) GOPATH=$(GOPATH)
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: all build test test-race vet integration snapshot clean

all: build

build:
	$(GO_ENV) go build -trimpath -ldflags '$(LDFLAGS)' -o bin/hostctl ./cmd/hostctl

test:
	$(GO_ENV) go test ./...

test-race:
	$(GO_ENV) go test -race ./...

vet:
	$(GO_ENV) go vet ./...

integration:
	$(GO_ENV) ./tests/integration_linux.sh

snapshot:
	mkdir -p dist
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/hostctl-linux-amd64 ./cmd/hostctl
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/hostctl-linux-arm64 ./cmd/hostctl

clean:
	rm -rf -- bin dist
