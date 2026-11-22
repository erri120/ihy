module ihy

go 1.27

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/a-h/templ v0.3.1118
	github.com/getkin/kin-openapi v0.153.0
	github.com/golang-migrate/migrate/v4 v4.20.1
	github.com/jackc/pgx/v5 v5.13.0
	github.com/nicksnyder/go-i18n/v2 v2.7.0
	github.com/oapi-codegen/runtime v1.9.1
	go.senan.xyz/taglib v0.0.0-00010101000000-000000000000
	golang.org/x/crypto v0.59.0
	golang.org/x/text v0.44.0
)

require (
	dario.cat/mergo v1.0.2 // indirect
	github.com/a-h/parse v0.0.42 // indirect
	github.com/air-verse/air v1.67.4 // indirect
	github.com/andybalholm/brotli v1.3.0 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/bep/godartsass/v2 v2.5.0 // indirect
	github.com/bep/golibsass v1.2.0 // indirect
	github.com/bits-and-blooms/bitset v1.24.5 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cli/browser v1.3.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/gohugoio/hashstructure v0.6.0 // indirect
	github.com/gohugoio/hugo v0.164.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgerrcode v0.0.0-20220416144525-469b46aa5efa // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/mattn/go-colorable v0.1.16 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/natefinch/atomic v1.0.1 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/tdewolff/parse/v2 v2.8.12 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	go.yaml.in/yaml/v4 v4.0.0 // indirect
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace go.senan.xyz/taglib => github.com/deluan/go-taglib v0.0.0-20260913142955-d55e0c9353cb

tool (
	github.com/a-h/templ/cmd/templ
	github.com/air-verse/air
)
