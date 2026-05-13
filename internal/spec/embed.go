// Package spec hosts the ASDD interception layer: templates, constitution
// parsing, hook handlers, and the project scaffolding that ships with
// `gemba init --asdd` and `gemba spec new --kit`.
package spec

import _ "embed"

//go:embed templates/commands/tasks.md
var TasksCommandTemplate string

//go:embed templates/commands/implement.md
var ImplementCommandTemplate string

//go:embed templates/hooks.json
var HooksJSONTemplate string

//go:embed templates/CLAUDE.md.tmpl
var ClaudeMDTemplate string

//go:embed templates/constitution.md.tmpl
var ConstitutionTemplate string

//go:embed schema/constitution.schema.json
var ConstitutionSchema string

//go:embed templates/spec.md.tmpl
var SpecMDTemplate string
