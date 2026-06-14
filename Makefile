.DEFAULT_GOAL := help
.PHONY: help test race vet web build run docker tidy clean

help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

test: ## run the Go correctness suite
	cd cluster && go test ./...

race: ## run the Go suite under the race detector
	cd cluster && go test -race ./...

vet: ## go vet the backend
	cd cluster && go vet ./...

web: ## install + build the frontend into web/dist
	cd web && npm install && npm run build

build: web ## build the frontend and the single server binary into bin/
	cd cluster && go build -trimpath -o ../bin/quorum ./cmd/quorum

run: build ## build everything and run the server (UI + API on :8080)
	./bin/quorum -static ./web/dist

docker: ## build and run via docker compose
	docker compose up --build

tidy: ## tidy Go module dependencies
	cd cluster && go mod tidy

clean: ## remove build artifacts
	rm -rf bin web/dist
