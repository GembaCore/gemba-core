module github.com/MikeBengtson/gemba

// Real repo should set go 1.23+; using 1.22 here to match this sandbox.
// The `slog` package requires 1.21+ and everything else works on 1.22+.
go 1.25.0

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/fsnotify/fsnotify v1.9.0
	github.com/go-chi/chi/v5 v5.1.0
	github.com/go-sql-driver/mysql v1.9.3
	github.com/modelcontextprotocol/go-sdk v1.5.0
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/client_model v0.6.2
	github.com/spf13/cobra v1.8.1
	golang.org/x/crypto v0.35.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
)

replace (
	gopkg.in/check.v1 => github.com/go-check/check v0.0.0-20161208181325-20d25e280405
	gopkg.in/yaml.v3 => github.com/go-yaml/yaml v3.0.1+incompatible
)
