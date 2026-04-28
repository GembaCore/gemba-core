# Social previews

Five 1280×640 PNG variations for GitHub's social-preview slot
(Settings → Social preview → Upload an image). Each captures
gemba's identity — operator-ordered work dispatched to AI agents —
with a different aesthetic register.

| File | Direction | Reference vibe |
| --- | --- | --- |
| `social-1-dark-kanban.png` | Dark + neon, cards → arrows → agents | vercel/ai, anthropic SDKs, openai-python |
| `social-2-light-serif.png` | Light minimalist, serif wordmark | Linear, Stripe docs, anthropic.com |
| `social-3-brutalist-mono.png` | Brutalist mono, ASCII flow diagram | charmbracelet/bubbletea, langgraph, autogen |
| `social-4-terminal.png` | Terminal window with command + output | charm.sh, ollama, gh CLI |
| `social-5-gradient-dispatch.png` | Marketing gradient, dispatch tile stack | openai-cookbook, anthropic quickstarts, e2b |

## Regenerate

```bash
python3 branding/social/generate.py
```

Outputs land next to the script. Pillow 10+ is required (system Python's
PIL is fine on macOS); fonts are pulled from `/System/Library/Fonts/`
(Helvetica, Helvetica Neue, Menlo, Georgia).

## Notes on transparency

PNGs are saved with an alpha channel. Backgrounds that *look* solid
(variations 1, 3, 4) carry a fully opaque dark fill; the alpha bytes
are present in the file but every pixel is ⍺=255. Variations 2 and 5
use semi-transparent overlays internally for layered effects, but the
final canvas is fully painted.

This matters because most social-preview consumers (X, LinkedIn,
Slack, Discord, Open Graph crawlers) composite onto white or context
without honouring per-pixel alpha. A literally-transparent canvas
would render unpredictably outside GitHub's own card. The choice
above keeps the visual identity stable everywhere.
