.PHONY: help pre-push lint test doc build 
.DEFAULT_GOAL := help

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

pre-push: lint test ## Run golang lint and test

lint: ## Run golang lint using docker
	go mod download
	docker run --rm \
		-v $(shell go env GOPATH)/pkg/mod:/go/pkg/mod \
 		-v ${PWD}:/app \
 		-w /app \
	    golangci/golangci-lint:v2.11 \
	    golangci-lint run --fix -v --modules-download-mode=readonly --timeout=5m

test: ## Run tests
	go test ./...

build: ## Build service
	go build -o raftogram ./cmd/raftogram