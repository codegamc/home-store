BIN       := bin/home-store
MODULE    := github.com/codegamc/home-store
GOFLAGS   := -trimpath
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS   := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: all
all: build

.PHONY: build
build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/home-store

.PHONY: test
test:
	go test ./internal/...

.PHONY: test-verbose
test-verbose:
	go test -v -count=1 ./internal/...

.PHONY:  integration-test
integration-test: build
	cd test/integration && HOME_STORE_BIN=../../$(BIN) go test -v -count=1 -timeout 120s ./...

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: docker
docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t home-store:dev -f docker/Dockerfile .

.PHONY: cross-build
cross-build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/home-store-linux-amd64 ./cmd/home-store
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/home-store-linux-arm64 ./cmd/home-store
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/home-store-linux-armv7 ./cmd/home-store

.PHONY: synology-spk
synology-spk: cross-build
	./synology/build-spk.sh x86_64 $(VERSION) bin/home-store-linux-amd64
	./synology/build-spk.sh armv8 $(VERSION) bin/home-store-linux-arm64

.PHONY: fmt
fmt:
	gofmt -w -s .

.PHONY: clean
clean:
	rm -rf bin/
