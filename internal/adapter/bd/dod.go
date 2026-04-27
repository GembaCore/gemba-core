package bd

import (
	"strings"

	"github.com/MikeBengtson/gemba/core"
)

// gm-e6.4 DoD pass-through.
//
// Beads has no native acceptance-criteria column on the WorkItem-shaped
// surface this adaptor reads (bd's --acceptance flag writes to a separate
// `notes`-adjacent field that does NOT round-trip through `bd show
// --json`). To give core.DefinitionOfDone a stable home without growing
// the bd schema, the adaptor encodes acceptance_criteria + notes inside
// a delimited section of the bead's description and parses it back on
// read. Anything outside the delimiters is preserved verbatim, so beads
// users who edit the description by hand never lose their DoD.
//
// Encoding (inside the delimiters):
//
//	<!--gemba:dod-->
//	acceptance:
//	- first criterion
//	- second criterion
//	notes:
//	free-form notes here, can span
//	multiple lines
//	<!--/gemba:dod-->
//
// The "informational-only" rule (gm-root DD-3) is preserved: the adaptor
// reads/writes the field but never gates a state transition on it.

const (
	dodOpenDelim  = "<!--gemba:dod-->"
	dodCloseDelim = "<!--/gemba:dod-->"
)

// extractDoD splits desc into (descriptionWithoutBlock, dod).
//
// When desc has no <!--gemba:dod--> block it returns desc unchanged and
// a nil DoD pointer. When the block is present but empty (no acceptance,
// no notes) it still returns a non-nil DoD with empty slices/strings —
// the SPA can then render a "DoD declared, no items yet" affordance.
//
// The function is conservative: a malformed block (open delim with no
// close delim) is treated as "no block" and surfaces verbatim in
// description, so a half-edited bead doesn't silently lose content.
func extractDoD(desc string) (string, *core.DefinitionOfDone) {
	openIdx := strings.Index(desc, dodOpenDelim)
	if openIdx < 0 {
		return desc, nil
	}
	rest := desc[openIdx+len(dodOpenDelim):]
	closeIdx := strings.Index(rest, dodCloseDelim)
	if closeIdx < 0 {
		// Malformed — open delim with no close delim. Leave the desc
		// untouched so the operator can fix it; don't half-strip.
		return desc, nil
	}
	inner := rest[:closeIdx]
	tail := rest[closeIdx+len(dodCloseDelim):]
	dod := parseDoDInner(inner)

	// Reassemble description without the block. Trim a single newline
	// pair around the delimiters so a description that wrapped the
	// block in blank lines doesn't end up with a double-newline scar
	// after extraction. We do this carefully: only consume the
	// adjacent whitespace, never further.
	prefix := desc[:openIdx]
	prefix = strings.TrimRight(prefix, "\n")
	tail = strings.TrimLeft(tail, "\n")
	cleaned := prefix
	if cleaned != "" && tail != "" {
		cleaned += "\n\n"
	}
	cleaned += tail
	return cleaned, dod
}

// parseDoDInner decodes the body between the delimiters into a
// DefinitionOfDone. The grammar is line-oriented:
//
//   - "acceptance:" header — every following line that begins with "- "
//     becomes one acceptance criterion (the leading "- " is stripped).
//   - "notes:" header — every following line until the next header /
//     end-of-block becomes part of Notes (joined with "\n", trimmed).
//
// Lines outside any header (e.g. a stray comment between sections) are
// silently dropped. We don't enforce ordering; "notes:" before
// "acceptance:" works the same.
func parseDoDInner(s string) *core.DefinitionOfDone {
	dod := &core.DefinitionOfDone{}
	lines := strings.Split(s, "\n")
	section := "" // "acceptance" | "notes" | ""
	var notesBuf []string
	for _, raw := range lines {
		line := raw
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "acceptance:":
			section = "acceptance"
			continue
		case "notes:":
			section = "notes"
			continue
		}
		switch section {
		case "acceptance":
			if strings.HasPrefix(trimmed, "- ") {
				dod.AcceptanceCriteria = append(dod.AcceptanceCriteria,
					strings.TrimSpace(trimmed[2:]))
			} else if trimmed == "-" {
				// Bare "-" is an empty bullet; record it as an empty
				// criterion so a round-trip preserves the slot.
				dod.AcceptanceCriteria = append(dod.AcceptanceCriteria, "")
			}
			// Non-bullet lines under acceptance are dropped on purpose.
		case "notes":
			notesBuf = append(notesBuf, line)
		}
	}
	if len(notesBuf) > 0 {
		dod.Notes = strings.TrimRight(strings.Join(notesBuf, "\n"), "\n")
		// Also trim leading blank lines so a "notes:\n\nfoo" round-trips
		// to just "foo".
		dod.Notes = strings.TrimLeft(dod.Notes, "\n")
	}
	return dod
}

// embedDoD returns desc with any existing <!--gemba:dod--> block
// replaced by the serialized form of dod. When dod is nil OR has
// nothing to serialize (no acceptance criteria AND empty notes), any
// existing block is removed and the surrounding description is left as
// the cleaned-up form (so passing a nil DoD on update is the way to
// clear the section).
//
// Round-trip property: extractDoD(embedDoD(d, dod)) == (d, dod) when
// d has no pre-existing block; existing blocks are replaced (not
// duplicated).
func embedDoD(desc string, dod *core.DefinitionOfDone) string {
	// Strip any existing block first so we always emit at most one.
	cleaned, _ := extractDoD(desc)

	if dod == nil || (len(dod.AcceptanceCriteria) == 0 && strings.TrimSpace(dod.Notes) == "") {
		return cleaned
	}

	var b strings.Builder
	b.WriteString(dodOpenDelim)
	b.WriteString("\n")
	if len(dod.AcceptanceCriteria) > 0 {
		b.WriteString("acceptance:\n")
		for _, c := range dod.AcceptanceCriteria {
			b.WriteString("- ")
			b.WriteString(c)
			b.WriteString("\n")
		}
	}
	if strings.TrimSpace(dod.Notes) != "" {
		b.WriteString("notes:\n")
		b.WriteString(dod.Notes)
		// Ensure the notes section ends with exactly one newline before
		// the close delim so the closing delim sits on its own line.
		if !strings.HasSuffix(dod.Notes, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString(dodCloseDelim)

	if cleaned == "" {
		return b.String()
	}
	return cleaned + "\n\n" + b.String()
}
