.PHONY: build-ui build-hub build-agent build-agent-windows build-all dev-hub dev-agent dev-ui clean release-agent checksums test-go lint-go fmt-go test-ui lint-ui fmt-ui test lint fmt

VERSION ?= dev
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

## Build targets

build-ui:
	cd ui && pnpm build

build-hub: build-ui
	go build $(LDFLAGS) -o bin/nexwatch-hub ./cmd/hub

build-agent:
	go build $(LDFLAGS) -o bin/nexwatch-agent ./cmd/agent

build-agent-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/nexwatch-agent.exe ./cmd/agent

build-all: build-ui build-hub build-agent

## Release targets

release-agent:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/nexwatch-agent ./cmd/agent
	tar -czf dist/nexwatch-agent_$(VERSION)_linux_amd64.tar.gz -C dist nexwatch-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/nexwatch-agent ./cmd/agent
	tar -czf dist/nexwatch-agent_$(VERSION)_linux_arm64.tar.gz -C dist nexwatch-agent
	rm dist/nexwatch-agent
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/nexwatch-agent.exe ./cmd/agent
	cd dist && zip -q nexwatch-agent_$(VERSION)_windows_amd64.zip nexwatch-agent.exe
	rm dist/nexwatch-agent.exe

checksums: release-agent
	cd dist && shasum -a 256 nexwatch-agent_$(VERSION)_linux_*.tar.gz nexwatch-agent_$(VERSION)_windows_*.zip > nexwatch-agent-checksums.txt

## Development targets

dev-hub:
	go run ./cmd/hub --http=0.0.0.0:8090

dev-agent:
	go run ./cmd/agent --hub=ws://localhost:8090/ws/agent --token=dev-token

dev-ui:
	cd ui && pnpm dev

## Test and lint targets

test-go:
	go test -race -cover -timeout 30m ./...

lint-go:
	golangci-lint run ./...

fmt-go:
	go fmt ./...

test-ui:
	cd ui && pnpm test

lint-ui:
	cd ui && pnpm lint

fmt-ui:
	cd ui && pnpm format

test: test-go test-ui

lint: lint-go lint-ui

## Utility targets

clean:
	rm -rf bin/ ui/dist/ dist/

tidy:
	go mod tidy

fmt: fmt-go fmt-ui
