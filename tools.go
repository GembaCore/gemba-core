//go:build tools
// +build tools

// Package tools pins build-time-only tools so `go mod tidy` keeps them
// in go.sum and a developer running `go install <tool>` lands a version
// matching what the repo expects.
//
// These imports are NEVER compiled into a runtime binary — the build
// tag `tools` is never set during normal builds. Each blank import is a
// dependency declaration only.
//
// gm-o9t8.1.1.4: oapi-codegen drives codegen for the typed Go client
// at internal/client/gen from internal/server/openapi/openapi.json.
package tools

import (
	_ "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
)
