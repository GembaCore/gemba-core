// /api/personas filesystem enumeration (gm-s47n.16, spec §7).
//
// Distinct from /api/v1/personas (gm-8qr) which projects the persona
// dispatcher's in-memory registry. The editor needs the raw
// .gemba/personas/*.toml files so a freshly added persona shows up
// without a server restart, hence the filesystem-direct lookup.
//
// Returns 200 with an empty list when the directory doesn't exist —
// that's the default state on a workspace before any personas have
// been declared.

package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// PersonaFile is one row in the response.
type PersonaFile struct {
	ID        string   `json:"id"`         // filename stem
	Name      string   `json:"name"`       // [persona] name (top-level "name" key)
	AgentType string   `json:"agent_type"` // declared agent_type if present
	Skills    []string `json:"skills,omitempty"`
}

// AttachPersonasDir binds the directory the /api/personas handler
// scans. Empty / unset falls back to "<cwd>/.gemba/personas".
func (r *Router) AttachPersonasDir(dir string) { r.personasDir = dir }

func (r *Router) listPersonaFiles(w http.ResponseWriter, _ *http.Request) {
	dir := r.personasDir
	if dir == "" {
		cwd, _ := os.Getwd()
		dir = filepath.Join(cwd, ".gemba", "personas")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{
				"personas": []PersonaFile{},
				"dir":      dir,
				"missing":  true,
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "read_dir_failed",
			"message": err.Error(),
		})
		return
	}

	rows := []PersonaFile{}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".toml") {
			continue
		}
		full := filepath.Join(dir, name)
		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		row := parsePersonaFile(strings.TrimSuffix(name, ".toml"), body)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{
		"personas": rows,
		"dir":      dir,
	})
}

// parsePersonaFile pulls the operator-facing fields out of a persona
// TOML body. We don't validate the full schema here — the persona
// dispatcher does that on its own load path. This handler is meant to
// surface what the editor needs (id, display name, agent type) and
// gracefully tolerate any extra keys.
func parsePersonaFile(id string, body []byte) PersonaFile {
	row := PersonaFile{ID: id}
	var top struct {
		Name      string   `toml:"name"`
		AgentType string   `toml:"agent_type"`
		Skills    []string `toml:"skills"`
		Persona   struct {
			Name      string   `toml:"name"`
			AgentType string   `toml:"agent_type"`
			Skills    []string `toml:"skills"`
		} `toml:"persona"`
	}
	if _, err := toml.Decode(string(body), &top); err == nil {
		// Prefer a [persona] table when present (matches the
		// canonical schema corepersona.Persona uses). Fall back to
		// flat top-level keys for the looser editor-friendly shape.
		if top.Persona.Name != "" {
			row.Name = top.Persona.Name
		} else {
			row.Name = top.Name
		}
		if top.Persona.AgentType != "" {
			row.AgentType = top.Persona.AgentType
		} else {
			row.AgentType = top.AgentType
		}
		if len(top.Persona.Skills) > 0 {
			row.Skills = top.Persona.Skills
		} else {
			row.Skills = top.Skills
		}
	}
	if row.Name == "" {
		row.Name = id
	}
	return row
}
