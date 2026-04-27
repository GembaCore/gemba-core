// /api/openapi.json — self-describing OpenAPI 3.1 schema for the
// gemba HTTP surface (gm-e4.2). The source of truth lives at
// docs/api/openapi.json and is embedded here so a running server is
// always self-describing without depending on the working directory.
//
// Codegen: `make codegen` runs `pnpm openapi-typescript` against the
// embedded copy (extracted via `gemba-spec-dump` in dev or the `/api/
// openapi.json` route at runtime) to regenerate web/src/api/gen/types.ts.
//
// MVP scope (gm-e4.2): the document covers the read endpoints the SPA
// depends on day-to-day; the long tail of `/api/*` (consults, walks,
// planner, bootstrap) appears as summary-only stubs and grows as
// follow-up beads land detailed schemas. The handler validates the
// embedded JSON parses as a minimal shape on first request so a
// malformed spec fails loudly rather than serving garbage.

package server

import (
	_ "embed"
	"encoding/json"
	"net/http"
)

// openapiSpecJSON is the raw bytes of the committed OpenAPI document.
// Embedded so an externally-deployed binary (no docs/ on disk) still
// serves a self-describing schema.
//
//go:embed openapi/openapi.json
var openapiSpecJSON []byte

// OpenAPISpec returns the embedded OpenAPI document bytes. Exported so
// cmd/gen-core-types and offline tooling can pull the same source the
// runtime serves without spinning up the server.
func OpenAPISpec() []byte { return openapiSpecJSON }

// openapiSpec is the lazily-validated parse of openapiSpecJSON. The
// handler unmarshals once into a minimal shape — just enough to catch
// "I committed broken JSON" mistakes — and reuses the raw bytes for
// the response. Full validation (Spectral-grade) runs as a separate
// `make` target, not at request time.
var openapiSpec struct {
	parsed bool
	err    error
	doc    minimalOpenAPIDoc
}

// minimalOpenAPIDoc captures only the top-level fields the handler
// asserts before serving. Avoids a full openapi3 dependency for an
// MVP: we just need `openapi` + `info.title` + `paths` to be present.
type minimalOpenAPIDoc struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Paths map[string]json.RawMessage `json:"paths"`
}

// ensureSpecValid parses openapiSpecJSON on first call and caches the
// outcome. Concurrent first-callers may parse twice — that's fine,
// the inputs are deterministic and json.Unmarshal is allocation-only.
func ensureSpecValid() error {
	if openapiSpec.parsed {
		return openapiSpec.err
	}
	var doc minimalOpenAPIDoc
	if err := json.Unmarshal(openapiSpecJSON, &doc); err != nil {
		openapiSpec.err = err
	} else if doc.OpenAPI == "" {
		openapiSpec.err = errMissingField("openapi")
	} else if doc.Info.Title == "" {
		openapiSpec.err = errMissingField("info.title")
	} else if len(doc.Paths) == 0 {
		openapiSpec.err = errMissingField("paths")
	} else {
		openapiSpec.doc = doc
	}
	openapiSpec.parsed = true
	return openapiSpec.err
}

func errMissingField(name string) error { return &openapiInvalidError{field: name} }

type openapiInvalidError struct{ field string }

func (e *openapiInvalidError) Error() string { return "openapi spec missing required field: " + e.field }

// openapiSpecHandler serves the embedded OpenAPI document. Cache-
// Control: a fresh build embeds a fresh spec, so no cache while the
// generator + spec live on the same Git revision (we'd burn one
// round-trip per page load only when the SPA opens devtools).
func (r *Router) openapiSpecHandler(w http.ResponseWriter, _ *http.Request) {
	if err := ensureSpecValid(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "openapi_invalid",
			"message": err.Error(),
		})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(openapiSpecJSON)
}
