// Package codeanalysis provides four prompt-ctx providers that
// adapt a [codeanalysis.Provider] (the typed knowledge-graph
// surface from gm-1ak) into context-provider Result fragments
// the persona prompt envelope splices in (gm-bro).
//
// Each provider is a thin renderer over a single backend method:
//
//   - [SummaryProvider]  → ListRepos + Modules + Health (the
//     PM-default "module inventory + health top-line").
//   - [SymbolContextProvider] → on-demand symbol context. The
//     persona names the symbol via the "symbol" Option; the
//     provider returns callers / callees / enclosing module by
//     reusing [Provider.Impact] in the "both" direction. (Code
//     Analysis backends that surface a richer Context method are
//     adapted in their own providers; this seed deliberately
//     keeps the call surface narrow.)
//   - [ImpactAnalysisProvider] → cascade-of-change for a symbol
//     (Architect, Code Reviewer default).
//   - [HealthReportProvider] → cycles, untested modules,
//     god-classes, undocumented APIs (QA, Documentarian default).
//
// All four take a [codeanalysis.Provider] and a
// [codeanalysis.RepoRef] via constructor injection; the
// dispatcher resolves these at registration time. Tests use a
// stub backend so the providers exercise without GitNexus
// (gm-56z).
//
// Each Refresh emits BOTH:
//   - Content — prose for the prompt envelope.
//   - Payload — JSON-friendly map carrying the typed shape so
//     the SPA / audit log can render without re-parsing prose.
package codeanalysis
