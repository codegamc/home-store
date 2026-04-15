BIN       := bin/home-store
MODULE    := github.com/codegamc/home-store
GOFLAGS   := -trimpath
LDFLAGS   := -s -w

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
	docker build -t home-store:dev -f docker/Dockerfile .

.PHONY: fmt
fmt:
	gofmt -w -s .

.PHONY: clean
clean:
	rm -rf bin/
