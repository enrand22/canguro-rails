# ── canguro-rails · Makefile ──────────────────────────────────────────────────
# Everything done often lives here, so nobody memorises commands
# (the same idea as `rails db:migrate` or `bin/rails test`).

SHELL := /bin/bash
PKG := ./...
GOBIN := $(shell go env GOPATH)/bin
COVER_MIN ?= 80

# Test database. `make db-up` starts it. REQUIRE_DB=1 is what makes `make test`
# mean something: without it the database tests skip silently and a green run
# proves nothing (that is how 36 tests went unnoticed once).
TEST_DATABASE_URL ?= canguro:canguro@tcp(127.0.0.1:3307)/canguro_test?parseTime=true&charset=utf8mb4&loc=UTC
REQUIRE_DB ?= 1
export TEST_DATABASE_URL
export REQUIRE_DB

# Makes the CLI test compile a scaffolded project against THIS checkout (it builds,
# runs templ and runs the new project's tests). Without it that test skips, and the
# template could rot unnoticed until the next app is created from it.
CANGURO_KIT_PATH ?= $(shell pwd)
export CANGURO_KIT_PATH

.PHONY: help setup build test cover lint fmt vuln db-up db-down clean scrub templates-tracked

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
	@# Count only what a human wrote: templ's generated code is not ours to test.
	@grep -vE '_templ\.go' coverage.out > coverage.filtered.out
	@go tool cover -func=coverage.filtered.out | tail -1
	@go tool cover -html=coverage.filtered.out -o coverage.html
	@pct=$$(go tool cover -func=coverage.filtered.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	 echo "coverage (without generated code): $$pct%  ·  minimum: $(COVER_MIN)%"; \
	 awk -v p="$$pct" -v m="$(COVER_MIN)" 'BEGIN { exit (p+0 < m+0) }' || \
	 (echo "BELOW THE THRESHOLD: add tests or justify the change in the PR"; exit 1)

lint: ## go vet + golangci-lint (when installed) + gofmt check
	go vet $(PKG)
	@command -v $(GOBIN)/golangci-lint >/dev/null && $(GOBIN)/golangci-lint run ./... || echo "(golangci-lint not installed: run make setup)"
	@test -z "$$(gofmt -l . | grep -v '_templ.go' || true)" || (echo "unformatted files:"; gofmt -l . | grep -v '_templ.go'; exit 1)
	@$(MAKE) --no-print-directory templates-tracked

templates-tracked: ## Every template file must be committable (git-ignored templates never ship)
	@# A template ignored by a .gitignore inside the template tree is a file that
	@# exists on the machine that wrote it and nowhere else: the CLI would not
	@# embed it, CI would not see it, and generated projects would quietly miss it.
	@for f in $$(find cmd/canguro/templates -type f | sort); do \
	  git ls-files --error-unmatch "$$f" >/dev/null 2>&1 || { \
	    echo "::error::template file not tracked by git (check .gitignore): $$f"; exit 1; }; \
	done
	@echo "all template files are tracked"

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
