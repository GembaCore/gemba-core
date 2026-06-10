---
name: tasks
description: Materialize the spec's tasks as beads.
---
You are in an ASDD-mode project. Tasks in this project live in `bd`, NOT in `tasks.md`.

Read `specs/{{slug}}/spec.md`. For each `### T-NN <title>` heading in the Tasks section, ensure a corresponding bead exists labeled `spec:{{slug}}`. Use `gemba spec reconcile {{slug}} --apply` — it computes the create/update/orphan diff and applies it atomically.

Do NOT write `tasks.md`. A pre-tool-use hook will reject the write and the project's constitution prohibits it.
