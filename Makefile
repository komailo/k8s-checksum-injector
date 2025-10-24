BINARY := k8s-checksum-injector
CMD_DIR := ./cmd/k8s-checksum-injector
BIN_DIR := ./bin
COVERAGE := coverage.out

.PHONY: build test lint release

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) $(CMD_DIR)

test:
	GOCACHE=$(CURDIR)/.cache/go-build go test ./... -coverprofile=$(COVERAGE) -covermode=atomic
	GOCACHE=$(CURDIR)/.cache/go-build go tool cover -func=$(COVERAGE)

lint:
	golangci-lint run

release:
	goreleaser release --clean --skip=publish --skip=validate
