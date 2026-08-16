VERSION ?= dev
GOCACHE ?= /tmp/hostctl-go-cache
GOPATH ?= /tmp/hostctl-go-path
GO_ENV = GOCACHE=$(GOCACHE) GOPATH=$(GOPATH)
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: all build test test-race vet lint deadcode integration system-test snapshot clean

all: build

build:
	$(GO_ENV) go build -trimpath -ldflags '$(LDFLAGS)' -o bin/hostctl ./cmd/hostctl

test:
	$(GO_ENV) go test ./...

test-race:
	$(GO_ENV) go test -race ./...

vet:
	$(GO_ENV) go vet ./...

lint:
	test -z "$$(gofmt -l cmd internal tests)"
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 staticcheck ./...
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 errcheck -exclude .errcheck-excludes ./...
	$(GO_ENV) govulncheck ./...
	shellcheck -x install.sh uninstall.sh profiles/*/profile.sh profiles/grok/bin/grok-agent-launch profiles/grok/bin/grok-safe.in tests/*.sh
	actionlint
	$(MAKE) deadcode

deadcode:
	@output="$$( $(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 deadcode ./... )" || exit $$?; \
	if [ -n "$$output" ]; then printf '%s\n' "$$output"; exit 1; fi

integration:
	$(GO_ENV) ./tests/integration_linux.sh

system-test:
	HOSTCTL_SYSTEM_TEST_ALLOW_MUTATION=1 \
	HOSTCTL_TEST_APPROVER_USER="$${SUDO_USER:-$${USER:-}}" \
	./tests/install_system_linux.sh

snapshot:
	mkdir -p dist
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/hostctl-linux-amd64 ./cmd/hostctl
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/hostctl-linux-arm64 ./cmd/hostctl

clean:
	rm -rf -- bin dist
