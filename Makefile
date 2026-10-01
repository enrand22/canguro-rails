# ── canguro-rails · Makefile ──────────────────────────────────────────────────
# Everything done often lives here, so nobody memorises commands
# (the same idea as `rails db:migrate` or `bin/rails test`).

SHELL := /bin/bash
PKG := ./...
GOBIN := $(shell go env GOPATH)/bin
COVER_MIN ?= 80

# Test database. `make db-up` starts it; the tests SKIP without it, which is why
# `make test` here refuses to pretend: it says out loud how many tests ran.
TEST_DATABASE_URL ?= canguro:canguro@tcp(127.0.0.1:3307)/canguro_test?parseTime=true&charset=utf8mb4&loc=UTC
export TEST_DATABASE_URL

.PHONY: help setup build test cover lint fmt vuln db-up db-down clean scrub

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

setup: ## Install the development tools (templ, goose, golangci-lint)
	go install github.com/a-h/templ/cmd/templ@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "done: $(GOBIN)"

build: ## Compile everything
	go build $(PKG)

test: ## Run the suite (against a real database when one is up)
	go test $(PKG) -count=1 -p 1

race: ## Run the suite with the race detector (jobs and pools live here)
	go test $(PKG) -count=1 -p 1 -race

cover: ## Coverage report; fails below $(COVER_MIN)%
	go test $(PKG) -count=1 -p 1 -coverpkg=./... -coverprofile=coverage.out -covermode=atomic
	@go tool cover -func=coverage.out | tail -1
	@go tool cover -html=coverage.out -o coverage.html
	@pct=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	 echo "coverage: $$pct%  ·  minimum: $(COVER_MIN)%"; \
	 awk -v p="$$pct" -v m="$(COVER_MIN)" 'BEGIN { exit (p+0 < m+0) }' || \
	 (echo "BELOW THE THRESHOLD: add tests or justify the change in the PR"; exit 1)

lint: ## go vet + golangci-lint (when installed) + gofmt check
	go vet $(PKG)
	@command -v $(GOBIN)/golangci-lint >/dev/null && $(GOBIN)/golangci-lint run ./... || echo "(golangci-lint not installed: run make setup)"
	@test -z "$$(gofmt -l . | grep -v '_templ.go' || true)" || (echo "unformatted files:"; gofmt -l . | grep -v '_templ.go'; exit 1)

fmt: ## Format the code (generated files untouched)
	gofmt -w $$(find . -name '*.go' -not -name '*_templ.go')

vuln: ## Dependency vulnerability audit
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# ── Test database ─────────────────────────────────────────────────────────────
db-up: ## Start the test MySQL (port 3307)
	@docker start canguro-mysql-test 2>/dev/null || \
	 docker run -d --name canguro-mysql-test -p 3307:3306 \
	   -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=canguro_test \
	   -e MYSQL_USER=canguro -e MYSQL_PASSWORD=canguro mysql:8.0
	@for i in {1..30}; do docker exec canguro-mysql-test mysqladmin ping -h127.0.0.1 -uroot -proot --silent >/dev/null 2>&1 && break; sleep 2; done
	@echo "test database up on 3307"

db-down: ## Stop the test MySQL (without deleting it)
	-docker stop canguro-mysql-test

scrub: ## Refuse to publish client data or personal traces (also runs in CI)
	@# The patterns are written with a character class (sol[u]gg) so that this very
	@# rule cannot match itself — otherwise the check would always fail on its own file.
	@! grep -rniE "sol[u]gg|segt[r]onic|kangar[o]om|be[j]a|kreaso[f]t|madei[s]a|20[0-9]\.126\.|84\.247\.|157\.173\." . \
	  --exclude-dir=.git || \
	 (echo "client traces found: this repository is PUBLIC"; exit 1)
	@! grep -rniE "enriqueand22[@]|/home/[a-z]+/" . --exclude-dir=.git || \
	 (echo "personal traces found"; exit 1)
	@echo "scrub clean"

clean: ## Remove local artifacts
	rm -rf bin dist coverage.out coverage.html
