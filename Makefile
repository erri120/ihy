# Unit tests: no external dependencies. Postgres-backed tests skip
# when no test database is configured.
test:
	go test ./... -count=1

# E2E tests: real Postgres (dev compose) + fixture library.
test-e2e:
	docker compose -f docker-compose.dev.yml up -d --wait testdb
	IHY_TEST_DSN=postgres://ihy:ihy@127.0.0.1:5433/postgres?sslmode=disable IHY_FIXTURE_DIR=$(CURDIR)/examples go test ./internal/db/ ./internal/dbtest/ ./internal/scanner/ -count=1
	docker compose -f docker-compose.dev.yml down

# Run ihy in a dev environment: ephemeral Postgres, fixtures as
# library. Override IHY_LIBRARIES for any read-only path.
IHY_LIBRARIES ?= $(CURDIR)/examples
dev:
	docker compose -f docker-compose.dev.yml up -d --wait testdb
	DB_HOST=127.0.0.1 DB_PORT=5433 DB_USER=ihy DB_PASSWORD=ihy DB_DATABASE=postgres IHY_LIBRARIES=$(IHY_LIBRARIES) go run ./cmd/ihy

build:
	CGO_ENABLED=0 go build -o ihy ./cmd/ihy

.PHONY: test test-e2e dev build
