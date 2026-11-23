# Generate Go code from templ templates (committed generated files).
ui:
	go tool templ generate

# Generate the API server interface and models from api/openapi.yaml
# (committed generated files).
api:
	go tool oapi-codegen -config api/gen.yaml api/openapi.yaml
	go mod tidy

# Unit tests: no external dependencies. Postgres-backed tests skip
# when no test database is configured. gotestsum prints testdox-style
# names and a failure summary.
test:
	go tool gotestsum -- -count=1 ./...

# Race detector over the whole suite; slower, run when touching
# concurrent code (scanner, resolver, caches).
test-race:
	go tool gotestsum -- -race -count=1 ./...

vet:
	go vet ./...

# Format committed sources: Go and templ. Generated _templ.go files
# are produced by `make ui`.
fmt:
	go fmt ./...
	go tool templ fmt .

# E2E tests: real Postgres (dev compose) + fixture library.
test-e2e:
	docker compose -f docker-compose.dev.yml up -d --wait testdb
	IHY_TEST_DSN=postgres://ihy:ihy@127.0.0.1:5433/postgres?sslmode=disable IHY_FIXTURE_DIR=$(CURDIR)/examples go test ./internal/db/ ./internal/dbtest/ ./internal/scanner/ ./internal/resolver/ ./internal/auth/ ./internal/api/ ./internal/jobs/ -count=1
	docker compose -f docker-compose.dev.yml down

# Run ihy in a dev environment: ephemeral Postgres, fixtures as
# library. Override IHY_LIBRARIES for any read-only path.
IHY_LIBRARIES ?= $(CURDIR)/examples
DEV_ENV := DB_HOST=127.0.0.1 DB_PORT=5433 DB_USER=ihy DB_PASSWORD=ihy DB_DATABASE=postgres \
	IHY_LIBRARIES=$(IHY_LIBRARIES) IHY_MB_REPLAYS=$(CURDIR)/internal/musicbrainz/testdata/replays

dev: ui
	docker compose -f docker-compose.dev.yml up -d --wait testdb
	$(DEV_ENV) go run ./cmd/ihy

# UI development: ihy with hot reload. templ regenerates on .templ
# edits (air rebuilds and restarts the server on the generated Go);
# static assets serve from disk and hot-swap in the browser via SSE
# (design page: /ui/design).
dev-ui: ui
	docker compose -f docker-compose.dev.yml up -d --wait testdb
	go tool templ generate --watch & templpid=$$!; \
	trap 'kill $$templpid' EXIT; \
	$(DEV_ENV) IHY_UI_DEV=1 IHY_UI_STATIC_DIR=$(CURDIR)/internal/webui/static go tool air -c .air.toml

build: ui
	CGO_ENABLED=0 go build -o ihy ./cmd/ihy

# Open the DuckDB UI against the dev database. Starts the dev stack if
# it is not running; the app is then left running in the background
# (logs in /tmp/ihy-dev.log) so the UI can browse live data.
DB_UI_INIT := /tmp/ihy-duckdb-init.sql
db-ui:
	@docker compose -f docker-compose.dev.yml up -d --wait testdb >/dev/null 2>&1
	@# DuckDB UI workaround: the UI issues "SET schema ihy.main" when the
	@# ihy catalog is selected, but attached Postgres catalogs only have
	@# "public". Creating an empty "main" schema satisfies the UI's
	@# assumption; it holds nothing and the dev database is ephemeral.
	@docker compose -f docker-compose.dev.yml exec -T testdb \
		psql -U ihy -d postgres -c "CREATE SCHEMA IF NOT EXISTS main" >/dev/null
	@if ! curl -sf http://127.0.0.1:7731/healthz >/dev/null; then \
		$(MAKE) build >/dev/null; \
		nohup env DB_HOST=127.0.0.1 DB_PORT=5433 DB_USER=ihy DB_PASSWORD=ihy \
			DB_DATABASE=postgres IHY_LIBRARIES=$(IHY_LIBRARIES) \
			IHY_MB_REPLAYS=$(CURDIR)/internal/musicbrainz/testdata/replays \
			$(CURDIR)/ihy >/tmp/ihy-dev.log 2>&1 & \
	fi
	@until curl -sf http://127.0.0.1:7731/healthz >/dev/null; do sleep 0.5; done
	@printf "INSTALL postgres;\nLOAD postgres;\nINSTALL json;\nLOAD json;\nATTACH 'dbname=postgres host=127.0.0.1 port=5433 user=ihy password=ihy' AS ihy (TYPE postgres, READ_ONLY);\nUSE ihy.public;\n" > $(DB_UI_INIT)
	duckdb -init $(DB_UI_INIT) -ui

# Record MusicBrainz replays from live data (fixture-driven, ~1 req/s).
record-replays:
	IHY_MB_RECORD=1 IHY_FIXTURE_DIR=$(CURDIR)/examples go test ./internal/musicbrainz/ -run TestRecordReplays -count=1 -v

.PHONY: ui api test test-race vet fmt test-e2e dev dev-ui build db-ui record-replays
