.PHONY: build-ui build-hub build-agent build-all dev-hub dev-agent dev-ui clean release-agent checksums

VERSION ?= dev
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

## Build targets

build-ui:
	cd ui && pnpm build

build-hub: build-ui
	go build $(LDFLAGS) -o bin/nexwatch-hub ./cmd/hub

build-agent:
	go build $(LDFLAGS) -o bin/nexwatch-agent ./cmd/agent

build-all: build-ui build-hub build-agent

## Release targets

release-agent:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/nexwatch-agent ./cmd/agent
	tar -czf dist/nexwatch-agent_$(VERSION)_linux_amd64.tar.gz -C dist nexwatch-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/nexwatch-agent ./cmd/agent
	tar -czf dist/nexwatch-agent_$(VERSION)_linux_arm64.tar.gz -C dist nexwatch-agent
	rm dist/nexwatch-agent

checksums: release-agent
	cd dist && shasum -a 256 nexwatch-agent_$(VERSION)_linux_*.tar.gz > nexwatch-agent-checksums.txt

## Development targets

dev-hub:
	go run ./cmd/hub --http=0.0.0.0:8090

dev-agent:
	go run ./cmd/agent --hub=ws://localhost:8090/ws/agent --token=dev-token

dev-ui:
	cd ui && pnpm dev

## Utility targets

clean:
	rm -rf bin/ ui/dist/ dist/

tidy:
	go mod tidy

fmt:
	go fmt ./...
	cd ui && pnpm exec prettier --write src/
