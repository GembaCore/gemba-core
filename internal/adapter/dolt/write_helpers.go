package dolt

import (
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

const (
	milestoneLabel = "type:milestone"
	stagedLabel    = "staged:true"

	labelAgentRolePrefix   = "agent:role:"
	labelAgentParentPrefix = "agent:parent:"

	dodOpenDelim  = "<!--gemba:dod-->"
	dodCloseDelim = "<!--/gemba:dod-->"
)

func doltStatusForCategory(cat core.StateCategory) (string, bool) {
	switch cat {
	case core.StateBacklog:
		return "deferred", true
	case core.StateUnstarted, core.StateStaged:
		return "open", true
	case core.StateStarted:
		return "in_progress", true
	case core.StateCompleted:
		return "closed", true
	}
	return "", false
}

func hasDoltLabel(labels []string, needle string) bool {
	for _, l := range labels {
		if l == needle {
			return true
		}
	}
	return false
}

func setDoltStagedLabel(labels []string, staged bool) []string {
	out := make([]string, 0, len(labels)+1)
	for _, l := range labels {
		if l == stagedLabel {
			continue
		}
		out = append(out, l)
	}
	if staged {
		out = append(out, stagedLabel)
	}
	return out
}

func doltAgentLabels(ref *core.AgentRef) []string {
	if ref == nil {
		return nil
	}
	var out []string
	if ref.Role != "" {
		out = append(out, labelAgentRolePrefix+ref.Role)
	}
	if ref.ParentID != nil && *ref.ParentID != "" {
		out = append(out, labelAgentParentPrefix+string(*ref.ParentID))
	}
	return out
}

func stripDoltAgentLabels(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, l := range in {
		if strings.HasPrefix(l, labelAgentRolePrefix) ||
			strings.HasPrefix(l, labelAgentParentPrefix) {
			continue
		}
		out = append(out, l)
	}
	return out
}

func parentOnDoltCreate(rels []core.Relationship) core.WorkItemID {
	for _, r := range rels {
		if r.Kind == core.RelParentChild && r.From != "" && r.To == "" {
			return r.From
		}
	}
	return ""
}

func extractDoltDoD(desc string) (string, *core.DefinitionOfDone) {
	openIdx := strings.Index(desc, dodOpenDelim)
	if openIdx < 0 {
		return desc, nil
	}
	rest := desc[openIdx+len(dodOpenDelim):]
	closeIdx := strings.Index(rest, dodCloseDelim)
	if closeIdx < 0 {
		return desc, nil
	}
	inner := rest[:closeIdx]
	tail := rest[closeIdx+len(dodCloseDelim):]
	dod := parseDoltDoDInner(inner)
	prefix := strings.TrimRight(desc[:openIdx], "\n")
	tail = strings.TrimLeft(tail, "\n")
	cleaned := prefix
	if cleaned != "" && tail != "" {
		cleaned += "\n\n"
	}
	cleaned += tail
	return cleaned, dod
}

func parseDoltDoDInner(s string) *core.DefinitionOfDone {
	dod := &core.DefinitionOfDone{}
	section := ""
	var notes []string
	for _, raw := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(raw)
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
				dod.AcceptanceCriteria = append(dod.AcceptanceCriteria, strings.TrimSpace(trimmed[2:]))
			} else if trimmed == "-" {
				dod.AcceptanceCriteria = append(dod.AcceptanceCriteria, "")
			}
		case "notes":
			notes = append(notes, raw)
		}
	}
	if len(notes) > 0 {
		dod.Notes = strings.TrimLeft(strings.TrimRight(strings.Join(notes, "\n"), "\n"), "\n")
	}
	return dod
}

func embedDoltDoD(desc string, dod *core.DefinitionOfDone) string {
	cleaned, _ := extractDoltDoD(desc)
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
