-- session_profiles (gm-s47n.2.1)
--
-- The planner's per-session warm-context store. One row per
-- core.Session, lifetime tied to the session: the writer hooks
-- (gm-s47n.2.3) insert on bead-claim, update on bead-completion,
-- and let the row outlive the session for retrospective queries.
--
-- The conceptual model is a join: this row + core.Session +
-- core.Workspace + core.AgentRef form an OperationalContext (see
-- internal/planner/profile.go). Joins happen in the read API
-- (gm-s47n.2.5), not in the table — the columns here are the bits
-- the join can't derive.
--
-- Schema notes:
--   - session_id is PK + foreign-key-by-convention to core.Session.id.
--     Dolt is content-versioned and we don't enforce FKs here; the
--     write hooks own that invariant.
--   - assignment_id + agent_id are denormalised so the scoring loop
--     answers "which agent / bead is this profile for?" in one read
--     without a session lookup.
--   - concepts / files are JSON columns — dolt encodes them as JSON
--     types so SQL filters can probe specific tags
--     (`JSON_EXTRACT(concepts, '$.auth')`).
--   - last_beads is a JSON array (newest last); the writer caps it
--     at LastBeadsRingSize from profile.go.
--   - context_pct is precomputed and stored so the SPA + planner
--     read the same number; computed on write from
--     tokens_used / context_window_max.
--   - last_activity_at is distinct from core.Session.last_heartbeat:
--     this column updates only on bead-event boundaries, not on
--     every health ping.

CREATE TABLE IF NOT EXISTS session_profiles (
  session_id          VARCHAR(255) NOT NULL,
  assignment_id       VARCHAR(255) NOT NULL,
  agent_id            VARCHAR(255) NOT NULL,

  concepts            JSON,
  files               JSON,

  tokens_used         INT          NOT NULL DEFAULT 0,
  context_window_max  INT          NOT NULL DEFAULT 0,
  context_pct         DOUBLE       NOT NULL DEFAULT 0.0,

  last_beads          JSON,
  last_activity_at    DATETIME(6)  NOT NULL,

  created_at          DATETIME(6)  NOT NULL,
  updated_at          DATETIME(6)  NOT NULL,

  PRIMARY KEY (session_id),
  KEY idx_session_profiles_agent (agent_id),
  KEY idx_session_profiles_assignment (assignment_id)
);

-- agent_profiles (gm-v5z2.2)
--
-- Persistent agent profile — sister to session_profiles, keyed on
-- AgentRef.ID. Survives `gt handoff`: a fresh session inherits the
-- agent's profile as a warm starting point. Decay is per-day
-- (default half-life 14d) — distinct from session_profiles'
-- per-event decay.
--
-- The retrospective hook writes both profiles on bead completion:
-- session row gets full weight, agent row gets contribution scaled
-- by 1 / lifetime_bead_count so a single bead doesn't dominate.
--
-- last_activity_at is the wall-clock of the most-recent completion;
-- the writer uses (now - last_activity_at) as the decay input for
-- AgeByDays so an idle profile fades correctly even when no fresh
-- read has happened since the last bump.

CREATE TABLE IF NOT EXISTS agent_profiles (
  agent_id              VARCHAR(255) NOT NULL,

  concepts              JSON,
  files                 JSON,

  lifetime_bead_count   BIGINT       NOT NULL DEFAULT 0,
  last_activity_at      DATETIME(6)  NOT NULL,

  created_at            DATETIME(6)  NOT NULL,
  updated_at            DATETIME(6)  NOT NULL,

  PRIMARY KEY (agent_id)
);

-- session_intents (gm-v5z2.3)
--
-- One row per session carrying the operator-pinned focus directive
-- (work-planning.md §4 Layer 1.3). Three orthogonal restrictors —
-- epic_id / label / bead_id_regex — AND together; at least one MUST
-- be set for a row to exist (an empty intent is the "cleared"
-- state, represented by the row's absence rather than NULL columns).
--
-- Selection (gm-v5z2.7) reads ctx.intent and demotes out-of-intent
-- candidates by demotion_factor (default 0.4 — set as the column
-- default so a hand-rolled INSERT picks up the canonical value).
--
-- session_id is PK + FK-by-convention to core.Session.id. The
-- companion lifecycle hook clears the row on session end so a
-- recycled session id can never inherit a stale intent.

CREATE TABLE IF NOT EXISTS session_intents (
  session_id          VARCHAR(255) NOT NULL,

  epic_id             VARCHAR(255) NOT NULL DEFAULT '',
  label               VARCHAR(255) NOT NULL DEFAULT '',
  bead_id_regex       VARCHAR(512) NOT NULL DEFAULT '',
  rationale           TEXT,

  demotion_factor     DOUBLE       NOT NULL DEFAULT 0.4,

  created_at          DATETIME(6)  NOT NULL,
  updated_at          DATETIME(6)  NOT NULL,

  PRIMARY KEY (session_id)
);

-- session_intent_audit (gm-v5z2.3)
--
-- Append-only history of every intent change. Every set/clear via
-- the CLI or SPA writes a row so the retrospective can attribute
-- "the session was focused on gm-e3 when this bead landed" without
-- re-reading the live row (which may have been cleared by then).
--
-- prior_json / next_json carry the full Intent struct as JSON so
-- the retrospective doesn't have to reconstruct the shape from
-- denormalised columns. Schema additions to Intent ride this without
-- a migration.

CREATE TABLE IF NOT EXISTS session_intent_audit (
  id                  BIGINT       NOT NULL AUTO_INCREMENT,
  session_id          VARCHAR(255) NOT NULL,
  action              VARCHAR(16)  NOT NULL,
  prior_json          JSON,
  next_json           JSON,
  actor               VARCHAR(255) NOT NULL DEFAULT '',
  at                  DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_session_intent_audit_session (session_id),
  KEY idx_session_intent_audit_at (at)
);

-- scorer_grades (gm-s47n.8.2)
--
-- One row per (bead, retrospective run) capturing the comparator
-- output from internal/planner/retro.Compare. The retrospective hook
-- (gm-s47n.8.3) writes after the bead's merge commits land and the
-- queryable view (gm-s47n.8.4) reads.
--
-- Schema notes:
--   - Primary key is (bead_id, closed_at). A bead retro may be re-
--     run when the merge graph evolves (revert, fixup, late
--     backfill); each run lands its own row keyed on the close
--     timestamp so historical comparisons stay intact.
--   - session_id / agent_id record which session shipped the bead.
--     Denormalised so the query view ("which sessions over-declare?")
--     doesn't need a join through the assignment table — the row
--     answers it standalone.
--   - declared_targets / declared_concepts capture what the planner
--     thought the bead would touch at dispatch time. Stored even
--     though the bead row is the canonical source so retros remain
--     queryable after the bead is updated with actuals.
--   - actual_files / actual_concepts are the comparator's "actual"
--     side — the merge file list + inferred concepts.
--   - target_divergence / concept_divergence are the Jaccard
--     distances; stored as columns so the §7.4 review query
--     ("show me beads where divergence > 0.5") is a sargable scan.
--   - diff_json is the full retro.Diff payload — stored verbatim
--     so future scorer changes can re-derive metrics from history
--     without re-running source analysis.

CREATE TABLE IF NOT EXISTS scorer_grades (
  bead_id             VARCHAR(255) NOT NULL,
  closed_at           DATETIME(6)  NOT NULL,

  session_id          VARCHAR(255) NOT NULL DEFAULT '',
  agent_id            VARCHAR(255) NOT NULL DEFAULT '',

  declared_targets    JSON,
  declared_concepts   JSON,
  actual_files        JSON,
  actual_concepts     JSON,

  target_divergence   DOUBLE       NOT NULL DEFAULT 0.0,
  concept_divergence  DOUBLE       NOT NULL DEFAULT 0.0,

  diff_json           JSON,

  created_at          DATETIME(6)  NOT NULL,

  PRIMARY KEY (bead_id, closed_at),
  KEY idx_scorer_grades_session (session_id),
  KEY idx_scorer_grades_target_divergence (target_divergence),
  KEY idx_scorer_grades_concept_divergence (concept_divergence)
);

-- dispatch_decisions (gm-s47n.6.2)
--
-- One row per dispatch pick (coach or auto). The retrospective
-- (gm-s47n.8.x) joins these against scorer_grades on (bead_id) to
-- compare *predicted* affinity at dispatch time to *observed*
-- divergence after the bead lands.
--
-- Why a separate table from scorer_grades:
--   - Different lifecycle. A decision is born at dispatch and never
--     re-runs; a grade is born on bead-close and may re-run when the
--     merge graph evolves.
--   - Different identity. A bead may be dispatched, recycled, and
--     re-dispatched against a different session — each pick is its
--     own row keyed on (bead_id, decided_at). The grade is keyed
--     on (bead_id, closed_at) — only the merge-time event matters.
--
-- Schema notes:
--   - mode is 'coach' | 'auto'. Coach decisions carry decided_by
--     (the operator id); auto decisions leave it blank.
--   - affinity_combined is denormalised so the §7.4 review query
--     ("show me high-affinity picks that diverged > 0.5") is
--     sargable without JSON_EXTRACT.
--   - affinity_json carries the full AffinityScores breakdown so
--     historical analysis can still see which sub-score drove the
--     pick even after the weighting changes.
--   - conflicts_json captures the conflict edges the planner saw at
--     decision time. The retrospective wants this to ask "did the
--     coach pick into a known conflict?" — a value that's only
--     observable at decision time.
--   - ready_set_json is the set of alternatives the coach could have
--     picked. Without it the retrospective can't ask "of the ready
--     set, was the chosen bead actually the highest-affinity one?"

CREATE TABLE IF NOT EXISTS dispatch_decisions (
  id                  VARCHAR(64)  NOT NULL,
  bead_id             VARCHAR(255) NOT NULL,
  decided_at          DATETIME(6)  NOT NULL,

  session_id          VARCHAR(255) NOT NULL DEFAULT '',
  agent_id            VARCHAR(255) NOT NULL DEFAULT '',
  decided_by          VARCHAR(255) NOT NULL DEFAULT '',
  mode                VARCHAR(16)  NOT NULL DEFAULT 'coach',

  affinity_combined   DOUBLE       NOT NULL DEFAULT 0.0,
  affinity_json       JSON,
  conflicts_json      JSON,
  ready_set_json      JSON,

  created_at          DATETIME(6)  NOT NULL,

  PRIMARY KEY (id),
  KEY idx_dispatch_decisions_bead (bead_id, decided_at),
  KEY idx_dispatch_decisions_session (session_id),
  KEY idx_dispatch_decisions_mode (mode),
  KEY idx_dispatch_decisions_combined (affinity_combined)
);
