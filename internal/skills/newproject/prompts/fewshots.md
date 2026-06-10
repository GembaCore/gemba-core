# Few-shot examples

The four examples below span the project shapes the Onboarder is
most likely to see in practice: a web app, a library, an ops
tooling project, and a research/exploration project. They are
illustrative -- do NOT copy them verbatim into a real conversation.
The schemas they emit are exhaustive (every field populated) so a
model that pattern-matches against them produces complete states.

---

## Example 1 -- Web app (small, opinionated)

**Operator turn 1:**

> I want to build a personal pomodoro timer. I work at a desk and
> mostly use Chrome. I want to track sessions over weeks.

**Onboarder reply (turn 1) -- proposes a first slice:**

```json
{
  "state": {
    "ProjectName": "tomato",
    "Description": "Personal pomodoro timer that runs in the browser and persists session history locally so the operator can review weekly throughput.",
    "TechStack": ["typescript", "react", "vite", "indexeddb"],
    "Architecture": "Single-page React app, no backend in v1. IndexedDB for session persistence. Optional CSV export.",
    "Milestones": [
      {
        "Title": "M1 -- Working timer in the browser",
        "Description": "A usable timer that runs a 25/5 cycle and persists completed sessions locally.",
        "Acceptance": "Operator can start a 25-minute focus block, hear an audible alert at end, and see the completed session in a history list after refresh.",
        "Labels": ["area:timer"],
        "Priority": 0,
        "Estimate": 480,
        "Skills": ["typescript", "react"],
        "DesignNotes": "Keep state in a single Zustand store; no global event bus. IndexedDB writes go through a thin repository so a future sync layer can swap in.",
        "Notes": "Audible alert in v1 may be a single static MP3; voice synthesis is out of scope.",
        "Epics": [
          {
            "Title": "E1.1 -- Core timer UI",
            "Description": "Render the running countdown, start/pause controls, and current cycle indicator.",
            "Acceptance": "User can start a 25-minute block, pause it, resume it, and the countdown matches wall-clock to within one second.",
            "Labels": [],
            "Priority": 0,
            "Estimate": 240,
            "Skills": ["typescript", "react"],
            "DesignNotes": "useEffect tied to setInterval with drift-correction against Date.now -- raw setInterval lags under tab-throttling.",
            "Notes": "",
            "Beads": [
              {
                "Title": "Render countdown",
                "Description": "Component that displays mm:ss for the current focus block, updating every 250ms.",
                "Type": "feature",
                "Acceptance": "Countdown renders, decreases monotonically, and clamps at 00:00.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 60,
                "Skills": ["typescript", "react"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": [],
                "BlocksRefs": []
              },
              {
                "Title": "Start / pause / resume controls",
                "Description": "Three buttons wired to the timer store. Pause preserves remaining time; resume continues from there.",
                "Type": "feature",
                "Acceptance": "Operator can start, pause, resume; the displayed countdown matches the underlying state across all three actions.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 60,
                "Skills": ["typescript", "react"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:0/bead:0"],
                "BlocksRefs": []
              },
              {
                "Title": "Drift correction",
                "Description": "Replace plain setInterval with a Date.now-anchored loop so the countdown stays accurate when Chrome throttles background tabs.",
                "Type": "task",
                "Acceptance": "After 30 minutes in a backgrounded tab, the countdown is within 1 second of the wall clock.",
                "Labels": [],
                "Priority": 1,
                "Estimate": 60,
                "Skills": ["typescript"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:0/bead:0"],
                "BlocksRefs": []
              }
            ]
          },
          {
            "Title": "E1.2 -- Session persistence",
            "Description": "Persist completed sessions to IndexedDB and render the history list.",
            "Acceptance": "Sessions completed before a refresh are visible in the history list after refresh.",
            "Labels": [],
            "Priority": 0,
            "Estimate": 240,
            "Skills": ["typescript"],
            "DesignNotes": "Repository pattern -- ui code never touches indexedDB directly.",
            "Notes": "",
            "Beads": [
              {
                "Title": "Session repository",
                "Description": "Repository module with create() / list() / clear() backed by IndexedDB.",
                "Type": "feature",
                "Acceptance": "Unit tests in jsdom verify round-trip persistence of 50 sessions.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 120,
                "Skills": ["typescript"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": [],
                "BlocksRefs": []
              },
              {
                "Title": "History list view",
                "Description": "Reverse-chronological list of completed sessions with date, duration, and label.",
                "Type": "feature",
                "Acceptance": "List renders 50 sessions without layout shift. Clicking a row expands its notes.",
                "Labels": [],
                "Priority": 1,
                "Estimate": 120,
                "Skills": ["typescript", "react"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:1/bead:0"],
                "BlocksRefs": []
              }
            ]
          }
        ]
      }
    ],
    "DraftProjectMD": "# tomato\n\nPersonal pomodoro timer. Single-page React app with IndexedDB persistence. v1 ships a working 25/5 timer plus a history list; no backend, no sync, no team features.\n\n## Why\n\nThe operator wants weekly throughput visibility without sharing their data with a hosted service.\n\n## Milestones (v1)\n\n- **M1 -- Working timer in the browser** -- 25/5 cycle, audible alert, persistent history.\n",
    "Turn": 1,
    "LastChange": {
      "path": "",
      "kind": "added",
      "summary": "Drafted milestone 1 (working timer) with two epics and five beads."
    }
  },
  "reply": "I sketched milestone 1 -- the bare-bones working timer with persistent session history. Two epics: the timer UI itself and the IndexedDB-backed history. I treated 'weekly review' as an implicit feature of the history list rather than a separate milestone; want me to break it out as M2 or fold it into M1? Also: any preference on how alerts should sound, or should I park that as a v1.1 polish bead?"
}
```

---

## Example 2 -- Library (Go module)

**Operator turn 1:**

> I want a tiny Go library that takes a struct and emits a CLI flag
> set automatically. Like cobra-but-simpler.

**Onboarder reply (turn 1):**

```json
{
  "state": {
    "ProjectName": "structflag",
    "Description": "A tiny Go library that derives a CLI flag set from a tagged struct using reflection. Aimed at single-binary tools that find cobra too heavy.",
    "TechStack": ["go", "reflect"],
    "Architecture": "Reflection over struct tags -> *flag.FlagSet binding. No external deps; standard library only. Public API is a single function: Bind(*FlagSet, *T).",
    "Milestones": [
      {
        "Title": "M1 -- Stable v1 API + scalar types",
        "Description": "Ship a v1 of Bind that handles the scalar types most CLIs need: string, int, bool, time.Duration.",
        "Acceptance": "go test passes for round-trip Bind on a struct with all four types; godoc renders the public surface; a README example compiles via go run.",
        "Labels": ["area:api"],
        "Priority": 0,
        "Estimate": 360,
        "Skills": ["go"],
        "DesignNotes": "Tag name is `flag:\"name,help=...\"`. Reject ambiguous names at Bind time, not run time. v1 freezes the public surface so semver works.",
        "Notes": "Slice types and nested structs are explicit non-goals for v1.",
        "Epics": [
          {
            "Title": "E1.1 -- Public API + tag parser",
            "Description": "Define Bind, the tag parser, and the error types. Document them in godoc.",
            "Acceptance": "go doc github.com/.../structflag renders cleanly; the package has zero exported helper types beyond what Bind needs.",
            "Labels": [],
            "Priority": 0,
            "Estimate": 180,
            "Skills": ["go"],
            "DesignNotes": "Errors implement error and have a single category enum (TagError, BindError) so callers can switch.",
            "Notes": "",
            "Beads": [
              {
                "Title": "Define Bind signature + scalar dispatch",
                "Description": "Bind(*flag.FlagSet, *T) error iterating the struct's fields and dispatching by Kind.",
                "Type": "feature",
                "Acceptance": "Unit tests cover string, int, bool, time.Duration round-trips with table-driven cases.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 120,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": [],
                "BlocksRefs": []
              },
              {
                "Title": "Tag parser",
                "Description": "Parse `flag:\"name,help=...,default=...\"` into a typed parsed-tag struct.",
                "Type": "feature",
                "Acceptance": "Tag-parser unit tests cover happy path, missing name (rejected), unknown key (rejected), embedded comma in help.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 60,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": [],
                "BlocksRefs": []
              }
            ]
          },
          {
            "Title": "E1.2 -- Examples + README",
            "Description": "A runnable example and a README that compiles via go test ./examples/...",
            "Acceptance": "go test ./examples/... runs successfully; the example output matches an embedded golden.",
            "Labels": [],
            "Priority": 1,
            "Estimate": 180,
            "Skills": ["go"],
            "DesignNotes": "",
            "Notes": "",
            "Beads": [
              {
                "Title": "Quickstart example",
                "Description": "examples/quickstart/main.go with a 30-line struct + Bind + flag.Parse.",
                "Type": "feature",
                "Acceptance": "go run ./examples/quickstart -- --help prints the expected flags.",
                "Labels": [],
                "Priority": 1,
                "Estimate": 60,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:0/bead:0", "milestone:0/epic:0/bead:1"],
                "BlocksRefs": []
              },
              {
                "Title": "README + godoc cross-links",
                "Description": "Top-level README with the quickstart code embedded via go run -- and pkg.go.dev cross-links.",
                "Type": "chore",
                "Acceptance": "README renders on GitHub; the embedded example matches examples/quickstart byte-for-byte.",
                "Labels": [],
                "Priority": 2,
                "Estimate": 120,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:1/bead:0"],
                "BlocksRefs": []
              }
            ]
          }
        ]
      }
    ],
    "DraftProjectMD": "# structflag\n\nA tiny Go library: `Bind(*flag.FlagSet, *T) error`. Reflection over struct tags. Stdlib only.\n\n## Non-goals (v1)\n\n- Slice types\n- Nested structs\n- Subcommands\n",
    "Turn": 1,
    "LastChange": {
      "path": "",
      "kind": "added",
      "summary": "Drafted milestone 1 (stable v1 + scalar types) with two epics and four beads."
    }
  },
  "reply": "Drafted v1 around the tightest possible API: Bind, scalars, two examples, no slices or nested structs. I'm treating 'cobra-but-simpler' as a hard constraint -- if you want subcommands the design changes substantially, so call that out now. Anything else you want pinned as a non-goal?"
}
```

---

## Example 3 -- Ops tooling (CLI + cron)

**Operator turn 1:**

> Help me build a tool that watches a directory of CSVs, validates
> them against a schema, and emits Slack notifications for failures.

**Onboarder reply (turn 1):**

```json
{
  "state": {
    "ProjectName": "csv-sentry",
    "Description": "An ops daemon that watches a directory of incoming CSVs, validates each file against a registered schema, and notifies Slack on validation failures so the upstream owner sees a problem before downstream pipelines do.",
    "TechStack": ["go", "fsnotify", "slack-webhook"],
    "Architecture": "Single binary daemon. fsnotify-driven file watcher -> per-file validator -> Slack webhook on failure. Schema registry is a TOML file the operator edits; no DB.",
    "Milestones": [
      {
        "Title": "M1 -- Local validation pipeline",
        "Description": "End-to-end on a single host: drop a CSV, get a Slack ping if it fails the registered schema.",
        "Acceptance": "Dropping a malformed CSV in the watch dir triggers a Slack message in the configured channel within 5 seconds.",
        "Labels": ["area:pipeline"],
        "Priority": 0,
        "Estimate": 720,
        "Skills": ["go", "infra"],
        "DesignNotes": "fsnotify on the watch dir; debounce 200ms to coalesce editor save bursts. Slack webhook URL from env (SLACK_WEBHOOK_URL).",
        "Notes": "Multi-host support and a real metrics endpoint move to M2.",
        "Epics": [
          {
            "Title": "E1.1 -- File watcher + dispatch",
            "Description": "fsnotify-driven loop that fires a validation job per stable file write.",
            "Acceptance": "Touching a .csv in the watch dir emits a single 'received' log line within 250ms; stable-write debouncing prevents partial-file false positives.",
            "Labels": [],
            "Priority": 0,
            "Estimate": 240,
            "Skills": ["go"],
            "DesignNotes": "",
            "Notes": "",
            "Beads": [
              {
                "Title": "fsnotify wrapper",
                "Description": "Module that wraps fsnotify with a debounce and emits stable-file events on a channel.",
                "Type": "feature",
                "Acceptance": "Integration test: 10 rapid writes to the same file produce exactly one event after the debounce window.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 120,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": [],
                "BlocksRefs": []
              },
              {
                "Title": "Dispatcher loop",
                "Description": "Pulls events off the channel and hands each to the validator.",
                "Type": "feature",
                "Acceptance": "Loop exits cleanly on SIGTERM; in-flight validations are awaited before exit.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 120,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:0/bead:0"],
                "BlocksRefs": []
              }
            ]
          },
          {
            "Title": "E1.2 -- Schema registry + validator",
            "Description": "TOML-driven schema registry; per-row column-type and presence checks.",
            "Acceptance": "Sample CSVs in fixtures/ with deliberate violations all produce the expected human-readable error message.",
            "Labels": [],
            "Priority": 0,
            "Estimate": 240,
            "Skills": ["go"],
            "DesignNotes": "",
            "Notes": "",
            "Beads": [
              {
                "Title": "Schema TOML loader",
                "Description": "Parse schemas.toml at startup; reload on SIGHUP.",
                "Type": "feature",
                "Acceptance": "Bad TOML at startup fails the binary fast with a line/column error; SIGHUP reloads without dropping in-flight events.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 120,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": [],
                "BlocksRefs": []
              },
              {
                "Title": "Row validator",
                "Description": "Apply column-type + presence rules to each row; collect all errors per file rather than failing fast.",
                "Type": "feature",
                "Acceptance": "Test fixtures with N seeded violations produce N reported errors.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 120,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:1/bead:0"],
                "BlocksRefs": []
              }
            ]
          },
          {
            "Title": "E1.3 -- Slack notifier",
            "Description": "Webhook-based notifier with rate limiting + redaction.",
            "Acceptance": "Validation failures land in the configured channel within 5 seconds; runs of 100 failures collapse into a single summary message.",
            "Labels": [],
            "Priority": 1,
            "Estimate": 240,
            "Skills": ["go"],
            "DesignNotes": "",
            "Notes": "",
            "Beads": [
              {
                "Title": "Webhook poster",
                "Description": "Tiny http.Client wrapper that POSTs the formatted message to SLACK_WEBHOOK_URL.",
                "Type": "feature",
                "Acceptance": "Integration test against a stubbed webhook server confirms the JSON body shape.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 60,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:1/bead:1"],
                "BlocksRefs": []
              },
              {
                "Title": "Rate limit + redaction",
                "Description": "Coalesce >10 failures/minute into a single summary; redact obvious PII columns by name from messages.",
                "Type": "feature",
                "Acceptance": "Burst test with 100 failures emits one message; redacted columns from the schema's `redact` list are replaced with [REDACTED].",
                "Labels": [],
                "Priority": 1,
                "Estimate": 180,
                "Skills": ["go"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:2/bead:0"],
                "BlocksRefs": []
              }
            ]
          }
        ]
      }
    ],
    "DraftProjectMD": "# csv-sentry\n\nDirectory-watcher that catches malformed CSVs before they break the downstream pipeline. Notifies Slack so the upstream owner sees the failure first.\n\n## Components\n\n- fsnotify watcher with stable-write debounce.\n- TOML schema registry; SIGHUP-reload.\n- Slack webhook notifier with rate-limit + PII redaction.\n",
    "Turn": 1,
    "LastChange": {
      "path": "",
      "kind": "added",
      "summary": "Drafted milestone 1 (local validation pipeline) with three epics covering watcher, validator, and notifier."
    }
  },
  "reply": "I split this into watcher / validator / notifier; that's the smallest skeleton that makes the end-to-end Slack ping demonstrable. I treated multi-host (a fleet that all share one schema registry) as M2 since you didn't mention it -- raise it now if the deploy target is more than one box. Schema language: I assumed presence + column-type checks; do you also need cross-row checks (e.g. 'order_id is unique within a file')?"
}
```

---

## Example 4 -- Research project (exploratory)

**Operator turn 1:**

> I want to explore whether a small transformer can learn to
> compress my git commit history into one-paragraph weekly summaries
> that read like the ones I write by hand.

**Onboarder reply (turn 1):**

```json
{
  "state": {
    "ProjectName": "weekly-summarizer",
    "Description": "An exploratory research project: can a small (sub-1B) transformer fine-tuned on the operator's hand-written weekly summaries reproduce that style well enough to draft the next one from raw git logs?",
    "TechStack": ["python", "pytorch", "huggingface", "tokenizers"],
    "Architecture": "Three-stage: dataset pipeline (git log -> instance pairs) -> fine-tune small base (Phi-class) on those pairs -> evaluation harness comparing model output to held-out hand-written summaries.",
    "Milestones": [
      {
        "Title": "M1 -- Reproducible dataset",
        "Description": "Decide -- before any training -- whether a useful (input, output) dataset exists at all. If the operator hasn't written enough summaries, this milestone surfaces that and the project pivots.",
        "Acceptance": "A reproducible script emits N (git_log_window, hand_written_summary) pairs from the operator's repos; N is reported and N >= 20 for a viable continuation.",
        "Labels": ["area:data"],
        "Priority": 0,
        "Estimate": 480,
        "Skills": ["python", "git"],
        "DesignNotes": "Pair-extraction reads weekly markdown files from a known directory and aligns each to the git-log window it covers. Deterministic given an input commit range.",
        "Notes": "If N < 20, do NOT proceed to M2 -- milestone-2 fine-tunes assume a dataset of at least the rough order of magnitude that will support a tiny model.",
        "Epics": [
          {
            "Title": "E1.1 -- Hand-summary corpus",
            "Description": "Locate, parse, and normalise the operator's hand-written summaries.",
            "Acceptance": "All summaries from the operator's known weekly directory parse without error; their date ranges are exposed as a sorted CSV.",
            "Labels": [],
            "Priority": 0,
            "Estimate": 240,
            "Skills": ["python"],
            "DesignNotes": "",
            "Notes": "",
            "Beads": [
              {
                "Title": "Summary parser",
                "Description": "Parse weekly markdown files into (start, end, body) tuples.",
                "Type": "feature",
                "Acceptance": "Unit tests on five real markdown files produce the expected tuples.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 120,
                "Skills": ["python"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": [],
                "BlocksRefs": []
              },
              {
                "Title": "Date range normaliser",
                "Description": "Map each summary's date range to a canonical UTC ISO interval.",
                "Type": "task",
                "Acceptance": "Round-trip property test: parse -> normalise -> serialise reproduces the original interval.",
                "Labels": [],
                "Priority": 1,
                "Estimate": 120,
                "Skills": ["python"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:0/bead:0"],
                "BlocksRefs": []
              }
            ]
          },
          {
            "Title": "E1.2 -- Git-log windowing + pair assembly",
            "Description": "For each summary, gather the git-log slice that covers the same window, then emit (input, output) pairs.",
            "Acceptance": "The pipeline emits a JSONL file of pairs; pair count is logged and matches manually counted summaries.",
            "Labels": [],
            "Priority": 0,
            "Estimate": 240,
            "Skills": ["python", "git"],
            "DesignNotes": "",
            "Notes": "",
            "Beads": [
              {
                "Title": "Git-log windower",
                "Description": "Run `git log` with --since/--until matching the summary interval; emit a uniform string representation.",
                "Type": "feature",
                "Acceptance": "Unit tests on a fixture repo produce stable output for a known date range.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 120,
                "Skills": ["python", "git"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": [],
                "BlocksRefs": []
              },
              {
                "Title": "Pair emitter",
                "Description": "Combine windowed git-log + hand-summary into a JSONL pair file.",
                "Type": "feature",
                "Acceptance": "End-to-end run on the operator's main repo emits a JSONL file; a smoke pass over five rows confirms the input includes the expected commit subjects.",
                "Labels": [],
                "Priority": 0,
                "Estimate": 120,
                "Skills": ["python"],
                "DesignNotes": "",
                "Notes": "",
                "DependsOnRefs": ["milestone:0/epic:0/bead:0", "milestone:0/epic:1/bead:0"],
                "BlocksRefs": []
              }
            ]
          }
        ]
      }
    ],
    "DraftProjectMD": "# weekly-summarizer\n\nExploratory: can a small transformer learn to draft one-paragraph weekly summaries that read like the operator's hand-written ones?\n\n## Stance\n\nThis is a research project, not a product. Milestone 1 is a kill-switch: if the operator has fewer than ~20 hand-written summaries, the project pivots before any training runs.\n\n## Plan\n\n1. Build a reproducible (git_log, hand_summary) dataset.\n2. Decide whether to proceed to fine-tuning based on dataset size.\n3. (M2, deferred) Fine-tune a sub-1B base.\n4. (M3, deferred) Evaluation harness.\n",
    "Turn": 1,
    "LastChange": {
      "path": "",
      "kind": "added",
      "summary": "Drafted milestone 1 (reproducible dataset) with two epics covering corpus parsing and pair assembly."
    }
  },
  "reply": "I framed M1 as a kill-switch: before any training, the dataset pipeline tells you whether you have enough hand-written summaries to make fine-tuning credible. M2 (fine-tune) and M3 (eval) are deferred until M1 reports its row count -- I'd rather you pivot early than spend a week on training a model that doesn't have a corpus. Tell me roughly how many weekly summaries you've written in the past year so I can sanity-check the threshold."
}
```

---

# Mid-conversation edit pattern

When the operator asks you to revise an existing tree, the
`LastChange` field signals what changed:

- Single-item rename: `path="milestone:1"`, `kind="renamed"`,
  `summary="Renamed milestone 2 to 'OSS-ready'"`.
- Drop an epic: `path="milestone:1/epic:2"`, `kind="removed"`,
  `summary="Dropped epic 1.3 (sync)"`. After dropping, walk the
  remaining beads' `DependsOnRefs` and remove any that pointed at
  the deleted epic; surface that downstream cleanup in the reply.
- Add a bead: `path="milestone:1/epic:0/bead:3"`,
  `kind="added"`, `summary="Added bead under epic 2.1 about
  telemetry"`.
- Tree-wide regeneration: empty `path`, `kind="edited"`,
  `summary="Regenerated milestones 2-3 to fold in the new tech-stack constraint"`.

The application uses LastChange to highlight the affected nodes in
the SPA's plan-preview pane. Make the summary specific enough that
the operator can confirm the right thing changed without
re-reading the tree.
