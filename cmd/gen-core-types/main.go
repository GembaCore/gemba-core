// Command gen-core-types writes the TypeScript mirror of core.
//
// Invoked via `make gen` or `go run ./cmd/gen-core-types`. Output goes
// to web/src/types/core.gen.ts (overwrite). The source of truth is the
// CoreTypesTS constant in core.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MikeBengtson/gemba/core"
)

func main() {
	out := flag.String("out", "web/src/types/core.gen.ts", "output file")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "gen-core-types: mkdir %s: %v\n", *out, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(core.CoreTypesTS), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-core-types: write %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(core.CoreTypesTS))
}
