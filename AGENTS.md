# ihy

Music library server: Go, Postgres, templ + htmx UI, oapi-codegen JSON API on one port.

- Entrypoint: ./cmd/ihy
- UI handlers: ./internal/webui/handlers.go; templates: ./internal/webui/*.templ
- JSON API: spec and generated code under ./api
- Design of record for the web UI: internal/webui/DESIGN.md (Dendera). Read it before UI changes.

## Commands

All commands live in ./Makefile.

- When changing server code: **Always** run `make vet` and `make test` to validate changes.
- Postgres-backed tests skip silently without a database. Run `make test-e2e` for the real-Postgres suite.
- Run `make fmt` before committing Go or templ changes.
- UI strings: active.en.toml is the source of truth; other locales may be partial (missing keys fall back to English). Never use MustLocalize in request paths.

## Documentation

Download the desired documentation to a temporary file and grep for headings first. **NEVER** read the entire contents; the files are too large.

- HTMX: https://raw.githubusercontent.com/bigskysoftware/htmx/refs/heads/master/www/content/docs.md
- templ: https://templ.guide/llms.md

## Dependencies

Go dependencies and development tools are managed through ./go.mod. Tools run via `go tool <name>`; add them with `go get -tool <module>@latest`.

## Schema changes

During development, fold schema changes into the latest migration file under migrations/. No incremental migrations until the first release has deployed databases to upgrade.
