//go:build ignore
// +build ignore

// preprocess_spec.go converts a small set of OpenAPI 3.1 idioms into
// 3.0-compatible equivalents so oapi-codegen v2.x (which still flags
// 3.1 as "best-effort") can ingest the gemba spec cleanly.
//
// Only one transformation is applied for now:
//
//	"type": ["X", "null"]   ==>   "type": "X", "nullable": true
//
// (oapi-codegen handles `nullable: true` correctly; it does NOT handle
// the 3.1 type-array form for primitive+null and aborts.)
//
// The OpenAPI version field is also downgraded from "3.1.0" to "3.0.3"
// so the generator's compatibility check stays quiet — the rest of the
// spec is already 3.0-compatible.
//
// Usage (invoked by `go generate` in this package):
//
//	go run preprocess_spec.go <src> <dst>
//
// gm-o9t8.1.1.4
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: preprocess_spec.go <src> <dst>")
		os.Exit(2)
	}
	src, dst := os.Args[1], os.Args[2]
	raw, err := os.ReadFile(src)
	if err != nil {
		die(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		die(fmt.Errorf("parse %s: %w", src, err))
	}
	walk(doc)
	if m, ok := doc.(map[string]any); ok {
		if v, ok := m["openapi"].(string); ok && v == "3.1.0" {
			m["openapi"] = "3.0.3"
		}
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		die(err)
	}
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		die(err)
	}
}

// walk recursively rewrites `"type": ["X","null"]` into `"type":"X"` +
// `"nullable": true` on any object it encounters.
func walk(v any) {
	switch x := v.(type) {
	case map[string]any:
		if t, ok := x["type"]; ok {
			if arr, ok := t.([]any); ok && len(arr) == 2 {
				a, aok := arr[0].(string)
				b, bok := arr[1].(string)
				if aok && bok {
					switch {
					case b == "null":
						x["type"] = a
						x["nullable"] = true
					case a == "null":
						x["type"] = b
						x["nullable"] = true
					}
				}
			}
		}
		for _, vv := range x {
			walk(vv)
		}
	case []any:
		for _, vv := range x {
			walk(vv)
		}
	}
}

func die(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
