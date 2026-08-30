VERSION ?= dev
GOCACHE ?= /tmp/rootbroker-go-cache
GOPATH ?= /tmp/rootbroker-go-path
GO_ENV = GOCACHE=$(GOCACHE) GOPATH=$(GOPATH)
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: all build test test-race vet lint deadcode integration system-test snapshot clean

all: build

build:
	$(GO_ENV) go build -trimpath -ldflags '$(LDFLAGS)' -o bin/rootbroker ./cmd/rootbroker

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
	shellcheck -x install.sh uninstall.sh migrate-private-prealpha.sh setup-apt-repository.sh packaging/debian/preinst packaging/debian/postinst packaging/debian/prerm packaging/debian/rootbroker-setup profiles/*/profile.sh profiles/grok/bin/grok-agent-launch profiles/grok/bin/grok-safe.in tests/*.sh
	actionlint
	$(MAKE) deadcode

deadcode:
	@output="$$( $(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 deadcode ./... )" || exit $$?; \
	if [ -n "$$output" ]; then printf '%s\n' "$$output"; exit 1; fi

integration:
	$(GO_ENV) ./tests/integration_linux.sh

system-test:
	ROOTBROKER_SYSTEM_TEST_ALLOW_MUTATION=1 \
	ROOTBROKER_TEST_APPROVER_USER="$${SUDO_USER:-$${USER:-}}" \
	./tests/install_system_linux.sh

snapshot:
	mkdir -p dist
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/rootbroker-linux-amd64 ./cmd/rootbroker
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/rootbroker-linux-arm64 ./cmd/rootbroker

clean:
	rm -rf -- bin dist
