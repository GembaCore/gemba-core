// Package selection implements the WP2.0 Layer 5 Selection step
// (gm-v5z2.7, work-planning.md §4 Layer 5).
//
// Single function, Select(Inputs) → []Result, that composes every
// score component (Affinity, Leverage, EpicAffinity) and runs the
// six gates from §4 Layer 5.2:
//
//  1. dispatch_status filter (hard)
//  2. owner-claim filter (hard)
//  3. conflict filter (hard)
//  4. runway gate (soft) — bead size > runway → ×0.5
//  5. intent gate (soft) — out-of-intent → ×demotion_factor
//  6. fairness boost (soft) — age in ready queue → +boost
//
// Pure function: same inputs always produce the same sorted list.
// The Justification slice on each Result is one line per gate fire
// + every score sub-component contribution; coach mode renders it
// verbatim and auto-dispatch stamps it on the dispatch event so
// the retrospective can grade WHY each pick happened.
package selection
