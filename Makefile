DATABASE_URL ?= postgres://commaplace:commaplace@localhost:5432/commaplace?sslmode=disable
# Separate DB: the test harness TRUNCATEs on every run, so it must NEVER point at
# the dev (or any real) database. Keep this distinct from DATABASE_URL.
TEST_DATABASE_URL ?= postgres://commaplace:commaplace@localhost:5432/commaplace_test?sslmode=disable
PORT     ?= 8080
DEV_USER ?= alice
DEV_URL  ?= http://localhost:$(PORT)/_dev/login?as=$(DEV_USER)

OPEN    := $(shell which xdg-open 2>/dev/null || which open 2>/dev/null || echo echo)
GO_ENV  := DEBUG=1 DATABASE_URL=$(DATABASE_URL) ADDR=:$(PORT) SEED_DEV=1
OPEN_BG := ( sleep 2 && $(OPEN) "$(DEV_URL)" ) &

.PHONY: dev dev-full dev-oauth watch test clean db-up db-down


## db-up: start local Postgres via Docker Compose
db-up:
	docker compose up -d postgres
	docker compose exec postgres sh -c 'until pg_isready -U commaplace; do sleep 1; done'

## db-down: stop local Postgres
db-down:
	docker compose down

## dev: local dev mode — DEBUG=1, dev login at http://localhost:$(PORT)/_dev/login?as=$(DEV_USER)
dev: db-up
	$(GO_ENV) go run ./cmd/server

## dev-full: dev + seed data (alice, bob, carol, dave), auto-opens browser
dev-full: db-up
	@$(OPEN_BG)
	$(GO_ENV) go run ./cmd/server

## dev-oauth: dev mode with Google OAuth (set GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET first)
dev-oauth: db-up
	$(GO_ENV) GOOGLE_CLIENT_ID=$(GOOGLE_CLIENT_ID) GOOGLE_CLIENT_SECRET=$(GOOGLE_CLIENT_SECRET) go run ./cmd/server

## watch: live-reload dev server (requires: go install github.com/air-verse/air@latest)
watch: db-up
	@$(OPEN_BG)
	$(GO_ENV) air

## test: run all tests (against the dedicated test DB, never dev data)
test: db-up
	@docker compose exec -T postgres psql -U commaplace -d commaplace -c 'CREATE DATABASE commaplace_test' >/dev/null 2>&1 || true
	TEST_DATABASE_URL=$(TEST_DATABASE_URL) go test ./...

## clean: remove binary
clean:
	rm -f ./commonplace
