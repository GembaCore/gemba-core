// Package evidence implements the shared evidence-synthesis library
// (gm-e11.6, resolves DD-13).
//
// Adaptors that need to reconstruct [core.Evidence] for a [core.WorkItemID]
// from external systems (git, GitHub PRs, CI) call into this package
// rather than reimplementing the discovery logic. Three default
// collectors are provided:
//
//   - [GitLogCollector]    — emits [core.EvidenceCommit] for every git
//     commit whose log message references the bead id.
//   - [GitHubPRCollector]  — emits [core.EvidenceURL] for every GitHub
//     pull request that mentions the bead id (title or body).
//   - [CICollector]        — emits [core.EvidenceTestResult] for every
//     GitHub Actions workflow run associated with a referencing PR or
//     commit.
//
// All synthesized evidence carries `Payload["synthesized"] = true` so
// downstream consumers can distinguish it from operator-attached
// records.
//
// # Concurrency
//
// [Synthesizer.Collect] runs every registered collector in parallel
// and merges the results in a stable order (collectors return in
// registration order, with each collector's items in the order it
// produced them). A failing collector logs and is skipped — the rest
// proceed.
//
// # Testability
//
// Every collector accepts an injectable [Runner] for shelling out, so
// tests do not actually invoke git or gh. The default runner uses
// os/exec, but tests substitute a table-driven fake.
//
// Contents:
//
//   - types.go     Collector interface, Source enum, Runner type
//   - gitlog.go    GitLogCollector
//   - githubpr.go  GitHubPRCollector
//   - ci.go        CICollector
//   - synth.go     Synthesizer facade (parallel + merge)
package evidence
