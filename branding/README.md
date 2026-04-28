# Brand assets

Ten paired image sets for gemba's GitHub presence. Each pair shares
palette, typography, and visual motif — pick the pair that best sells
the angle your audience already feels.

| # | Theme | Social preview (1280×640) | README banner (1280×320) | Differentiator emphasized |
| - | ----- | ------------------------- | ------------------------ | ------------------------- |
| 01 | Dark kanban → agents | `social/social-01-dark-kanban.png` | `banner/banner-01-dark-kanban.png` | Operator-ordered dispatch |
| 02 | Light minimalist serif | `social/social-02-light-serif.png` | `banner/banner-02-light-serif.png` | Typographic identity |
| 03 | Brutalist ASCII flow | `social/social-03-brutalist-mono.png` | `banner/banner-03-brutalist-mono.png` | Declarative 1 → N model |
| 04 | Terminal CLI | `social/social-04-terminal.png` | `banner/banner-04-terminal.png` | Single-binary, runnable |
| 05 | Gradient dispatch tiles | `social/social-05-gradient-dispatch.png` | `banner/banner-05-gradient-dispatch.png` | Curated bead → agent |
| 06 | Blueprint schematic | `social/social-06-blueprint.png` | `banner/banner-06-blueprint.png` | Adaptor architecture |
| 07 | Adaptor matrix grid | `social/social-07-adaptor-matrix.png` | `banner/banner-07-adaptor-matrix.png` | Any tracker × any runtime |
| 08 | Capability flags | `social/social-08-capability-flags.png` | `banner/banner-08-capability-flags.png` | Capability-aware UI |
| 09 | Persona constellation | `social/social-09-persona-constellation.png` | `banner/banner-09-persona-constellation.png` | Typed persona system |
| 10 | Three-pane app | `social/social-10-three-pane.png` | `banner/banner-10-three-pane.png` | Backlog · dispatch · sessions |

## Usage

**Social preview** — repo Settings → Social preview → Upload an image.
GitHub composites onto white for embeds (X, LinkedIn, Slack, Discord);
the canvas fill in each variation accounts for that.

**README banner** — embed at the top of `README.md`:

```markdown
[![gemba](branding/banner/banner-01-dark-kanban.png)](https://github.com/MikeBengtson/gemba)
```

The banners pair with their social card so the GitHub project page
and embedded shares read as a coherent set.

## Regenerate

```bash
python3 branding/generate.py
```

Outputs land under `branding/social/` and `branding/banner/` (the
`.png` files are committed; regenerating overwrites in place).
Pillow 10+ required; fonts are pulled from `/System/Library/Fonts/`
(Helvetica, Helvetica Neue, Menlo, Georgia Bold).

## Notes on transparency

PNGs carry an alpha channel. Variations with a strong solid background
(01, 03, 04, 06, 08, 09) are fully opaque end-to-end; variations with
gradients or layered overlays (05) use semi-transparent layers
internally but the final canvas is fully painted. Most social-preview
consumers composite onto white without honouring per-pixel alpha, so
the visual identity stays stable across platforms.
