# Variables
APP_NAME 	:= kubectl-syncpod
OUTPUT   	:= $(APP_NAME)
COV_REPORT 	:= coverage.txt
TEST_FLAGS 	:= -v -race -timeout 30s
INSTALL_DIR := /usr/local/bin

ifeq ($(OS),Windows_NT)
	OUTPUT := $(APP_NAME).exe
endif

.PHONY: lint
lint:
	golangci-lint run --output.tab.path=stdout

.PHONY: gen
gen:
	go generate ./...

.PHONY: build
build: gen
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/$(OUTPUT) main.go

.PHONY: install
install: build
	@echo "Installing bin/$(OUTPUT) to $(INSTALL_DIR)..."
	@install -m 0755 bin/$(OUTPUT) $(INSTALL_DIR)

.PHONY: test
test:
	go test -v -cover ./...

.PHONY: snapshot
snapshot:
	goreleaser release --skip sign --skip publish --snapshot --clean

.PHONY: test-cov
test-cov:
	go test -coverprofile=$(COV_REPORT) ./...
	go tool cover -html=$(COV_REPORT)
