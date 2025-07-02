.PHONY: all build deps generate help test version

CHECK_FILES ?= $$(go list ./... | grep -v /vendor/)

help: ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {sub("\\\\n",sprintf("\n%22c"," "), $$2);printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: deps generate build test ## Run all steps

build: ## Build all
	go build ./...

dev: ## Run the development server
	go run ./cmd/server/main.go

deps: ## Download dependencies.
	go mod tidy

generate: ## Run code generation
	go generate ./...

test: ## Run tests
	go test -v $(CHECK_FILES)

version: ## Run the goversion tool. Usage: make version bump="patch" [files="-file=README.md"]
	@# Check that the 'bump' variable is provided.
	@if [ -z "$(bump)" ]; then \
		echo "Error: You must provide a bump argument, e.g. bump=\"patch\"" >&2; \
		exit 1; \
	fi
	@echo "Running goversion with bump: $(bump)"
	go run main.go $(bump)
