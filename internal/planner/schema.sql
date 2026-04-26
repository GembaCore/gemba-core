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
