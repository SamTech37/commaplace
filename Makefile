DB      ?= ./dev.db
PORT    ?= 8080
BINARY  := ./commonplace

.PHONY: run dev dev-full dev-oauth watch build test clean

## build: compile the server binary
build:
	go build -o $(BINARY) ./cmd/server

## run: run with no special flags (uses commonplace.db)
run: build
	$(BINARY)

## dev: local dev mode — DEBUG=1, dev login at /_dev/login?as=<handle>
dev:
	DEBUG=1 DB_PATH=$(DB) ADDR=:$(PORT) go run ./cmd/server

## dev-full: dev mode + multi-user seed data (alice, bob, carol, dave)
# go to /_dev/login?as=alice 
dev-full:
	DEBUG=1 DB_PATH=$(DB) ADDR=:$(PORT) SEED_DEV=1 go run ./cmd/server

## dev-oauth: dev mode with Google OAuth (set GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET first)
dev-oauth:
	DEBUG=1 DB_PATH=$(DB) ADDR=:$(PORT) SEED_DEV=1 \
	GOOGLE_CLIENT_ID=$(GOOGLE_CLIENT_ID) \
	GOOGLE_CLIENT_SECRET=$(GOOGLE_CLIENT_SECRET) \
	go run ./cmd/server

## watch: live-reload dev server (requires: go install github.com/air-verse/air@latest)
watch:
	air

## test: run all tests
test:
	go test ./...

## clean: remove binary and dev database
clean:
	rm -f $(BINARY) $(DB)
