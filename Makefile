.PHONY: build-ui build-hub build-agent build-all dev-hub dev-agent dev-ui clean

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

## Development targets

dev-hub:
	go run ./cmd/hub --http=0.0.0.0:8090

dev-agent:
	go run ./cmd/agent --hub=ws://localhost:8090/ws/agent --token=dev-token

dev-ui:
	cd ui && pnpm dev

## Utility targets

clean:
	rm -rf bin/ ui/dist/

tidy:
	go mod tidy

fmt:
	go fmt ./...
	cd ui && pnpm exec prettier --write src/
