test-db:
	docker compose --profile test up -d --wait testdb
	IHY_TEST_DSN=postgres://ihy:ihy@127.0.0.1:5433/postgres?sslmode=disable IHY_FIXTURE_DIR=$(CURDIR)/examples go test ./internal/db/ ./internal/dbtest/ ./internal/scanner/ -count=1
	docker compose --profile test down -v

test:
	go test ./... -count=1

build:
	CGO_ENABLED=0 go build -o ihy ./cmd/ihy

run: build
	./ihy

.PHONY: test-db test build run
