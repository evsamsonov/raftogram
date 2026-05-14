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
	go build ./cmd/main.go

doc: ## Run doc server using docker
	@echo "Doc server runs on http://127.0.0.1:6060"
	docker run --rm \
        -p 127.0.0.1:6060:6060 \
        -v ${PWD}:/go/src/github.com/evsamsonov/stab-quotes-exporter \
        -w /go/src/github.com/evsamsonov/stab-quotes-exporter \
        golang:latest \
        bash -c "go install golang.org/x/tools/cmd/godoc@latest && /go/bin/godoc -http=:6060"

