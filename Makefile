VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO ?= go

.PHONY: build run check test release image

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/mcpanel ./cmd/mcpanel

run: build
	./bin/mcpanel -data ./data

test:
	$(GO) test ./...

check:
	gofmt -l . | (! grep .)
	$(GO) vet ./...
	$(GO) test ./...

release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/mcpanel-linux-amd64 ./cmd/mcpanel
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/mcpanel-linux-arm64 ./cmd/mcpanel

image:
	docker build --build-arg VERSION=$(VERSION) -t mcpanel:$(VERSION) -t mcpanel:latest .
