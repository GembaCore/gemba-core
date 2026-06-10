package epic_order

// outputSchema is the JSON Schema fed to Anthropic via the tools[].
// input_schema field. The model is instructed (via SkillPrompt) to
// emit a single emit_ordering call whose argument is an array
// matching this schema.
//
// Stored as a Go literal rather than an embedded .json file so the
// shape is readable next to the Go types it mirrors and a typo in
// the schema fails the build instead of fail at first runtime call.
// The /api/v1/skills/epic_order/output_schema.json route MAY render
// this map back to JSON for external consumers.
//
// Keep this in lockstep with the StrategyLine / RecommendationLine /
// WarningLine / DeferredLine / SummaryLine Go types. ValidateOutputLine
// is the runtime enforcer; this schema is what the model sees.
var outputSchema = map[string]any{
	"type":        "object",
	"description": "An ordered set of lines describing the recommended Epic staging order. The first MUST be a strategy line and the last MUST be a summary line.",
	"required":    []any{"lines"},
	"properties": map[string]any{
		"lines": map[string]any{
			"type":        "array",
			"description": "Ordered list of output lines. First element type=strategy; last element type=summary; recommendation lines (if any) come in rank order between them.",
			"minItems":    2,
			"items": map[string]any{
				"oneOf": []any{
					schemaStrategy,
					schemaRecommendation,
					schemaWarning,
					schemaDeferred,
					schemaSummary,
				},
			},
		},
	},
	"additionalProperties": false,
}

var schemaStrategy = map[string]any{
	"type": "object",
	"required": []any{
		"type", "workspace", "as_of", "model", "reasoning",
		"total_considered", "total_ranked",
	},
	"properties": map[string]any{
		"type":             map[string]any{"const": "strategy"},
		"workspace":        map[string]any{"type": "string"},
		"as_of":            map[string]any{"type": "string", "format": "date-time"},
		"model":            map[string]any{"type": "string"},
		"reasoning":        map[string]any{"type": "string", "minLength": 1},
		"total_considered": map[string]any{"type": "integer", "minimum": 0},
		"total_ranked":     map[string]any{"type": "integer", "minimum": 0},
		"filtered":         map[string]any{"type": "integer", "minimum": 0},
		"filter_reason":    map[string]any{"type": "string"},
	},
	"additionalProperties": false,
}

var schemaRecommendation = map[string]any{
	"type": "object",
	"required": []any{
		"type", "rank", "epic_id", "confidence", "rationale",
	},
	"properties": map[string]any{
		"type":             map[string]any{"const": "recommendation"},
		"rank":             map[string]any{"type": "integer", "minimum": 0},
		"epic_id":          map[string]any{"type": "string", "minLength": 1},
		"parallel_group":   map[string]any{"type": "string"},
		"confidence":       map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		"rationale":        map[string]any{"type": "string", "minLength": 1},
		"estimated_tokens": map[string]any{"type": "integer", "minimum": 0},
		"suggested_action": map[string]any{
			"type":     "object",
			"required": []any{"verb", "path"},
			"properties": map[string]any{
				"verb": map[string]any{"type": "string", "enum": []any{"GET", "POST", "PATCH", "PUT", "DELETE"}},
				"path": map[string]any{"type": "string", "minLength": 1},
				"body": map[string]any{},
			},
			"additionalProperties": false,
		},
	},
	"additionalProperties": false,
}

var schemaWarning = map[string]any{
	"type":     "object",
	"required": []any{"type", "kind", "detail"},
	"properties": map[string]any{
		"type": map[string]any{"const": "warning"},
		"kind": map[string]any{
			"type": "string",
			"enum": []any{
				"parallel_conflict",
				"dep_cycle",
				"budget_overrun",
				"missing_signal",
				"low_confidence",
			},
		},
		"epic_ids": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string", "minLength": 1},
		},
		"detail": map[string]any{"type": "string", "minLength": 1},
	},
	"additionalProperties": false,
}

var schemaDeferred = map[string]any{
	"type":     "object",
	"required": []any{"type", "epic_id", "reason"},
	"properties": map[string]any{
		"type":    map[string]any{"const": "deferred"},
		"epic_id": map[string]any{"type": "string", "minLength": 1},
		"reason":  map[string]any{"type": "string", "minLength": 1},
	},
	"additionalProperties": false,
}

var schemaSummary = map[string]any{
	"type": "object",
	"required": []any{
		"type", "ranked_count", "advisor_cost_dollars",
		"tokens_in", "tokens_out", "latency_ms", "model",
	},
	"properties": map[string]any{
		"type":                   map[string]any{"const": "summary"},
		"ranked_count":           map[string]any{"type": "integer", "minimum": 0},
		"filtered_count":         map[string]any{"type": "integer", "minimum": 0},
		"warnings_count":         map[string]any{"type": "integer", "minimum": 0},
		"deferred_count":         map[string]any{"type": "integer", "minimum": 0},
		"total_estimated_tokens": map[string]any{"type": "integer", "minimum": 0},
		"sprint_budget_headroom": map[string]any{"type": "number"},
		"advisor_cost_dollars":   map[string]any{"type": "number", "minimum": 0},
		"tokens_in":              map[string]any{"type": "integer", "minimum": 0},
		"tokens_out":             map[string]any{"type": "integer", "minimum": 0},
		"latency_ms":             map[string]any{"type": "integer", "minimum": 0},
		"model":                  map[string]any{"type": "string", "minLength": 1},
		"confidence_overall":     map[string]any{"type": "number", "minimum": 0, "maximum": 1},
	},
	"additionalProperties": false,
}
