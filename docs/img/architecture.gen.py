#!/usr/bin/env python3
"""
Generate docs/img/architecture.png — the canonical system-architecture
diagram for the README.

Aesthetic: official technical diagram (AWS / Stripe / Datadog docs).
Off-white canvas, thin neutral borders, charcoal type, generous
whitespace. Subtle tie-in to the brand banner via stripe colors only —
sky / emerald / rose / amber map to SPA / core / WorkPlane / Orchestration
in lockstep with `branding/banner/banner-arch-gradient.png` so the two
read as a deliberate pair without the diagram looking like marketing.

Run: `python3 docs/img/architecture.gen.py`
"""

import os
from PIL import Image, ImageDraw, ImageFont, ImageFilter

W, H = 1280, 1010
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "architecture.png")

# ── Fonts ──
HELV_NEUE = "/System/Library/Fonts/HelveticaNeue.ttc"
HELV = "/System/Library/Fonts/Helvetica.ttc"
MENLO = "/System/Library/Fonts/Menlo.ttc"


def f(path, size, idx=0):
    return ImageFont.truetype(path, size, index=idx)


def tw(d, t, fnt):
    bb = d.textbbox((0, 0), t, font=fnt)
    return bb[2] - bb[0]


# ── Palette (technical-doc neutrals; banner-matched accents) ──
INK = (24, 26, 32)
DIM = (110, 114, 124)
SOFT = (160, 162, 170)
BORDER = (216, 216, 213)
HEADER = (245, 245, 243)
CARD = (255, 255, 255)
PAGE = (250, 250, 249)
SHADOW = (0, 0, 0)

ACCENT = {
    "spa": (14, 165, 233),     # sky
    "core": (16, 185, 129),    # emerald
    "wp": (244, 63, 94),       # rose — REQUIRED
    "orch": (245, 158, 11),    # amber — OPTIONAL
    "analysis": (124, 58, 237), # violet — FOUNDATION (Source Code Analysis Store)
}


def shadowed_round_rect(img, x, y, w, h, *, radius=10, fill=CARD, border=BORDER):
    """Draw a card with a soft shadow behind it. The shadow is a blurred
    rounded rectangle at low alpha — gives the diagram an elevated,
    schematic feel without leaning on hard outlines."""
    sh = Image.new("RGBA", img.size, (0, 0, 0, 0))
    sd = ImageDraw.Draw(sh)
    sd.rounded_rectangle((x + 0, y + 4, x + w + 0, y + h + 4),
                         radius=radius, fill=(*SHADOW, 36))
    sh = sh.filter(ImageFilter.GaussianBlur(radius=4))
    img.alpha_composite(sh)
    d = ImageDraw.Draw(img)
    d.rounded_rectangle((x, y, x + w, y + h),
                        radius=radius, fill=(*fill, 255),
                        outline=(*border, 255), width=1)


def stripe(d, x, y, w, h, color, *, side="left", thick=4, radius=10):
    """Colored stripe along the left edge of a card. Banner tie-in: the
    palette mirrors the gradient banner's tile-stripe assignments."""
    if side == "left":
        d.rectangle((x, y, x + thick, y + h), fill=(*color, 255))
    elif side == "top":
        d.rectangle((x, y, x + w, y + thick), fill=(*color, 255))
    # Re-trim the corner radius so the stripe doesn't bleed past it.
    # (Visual approximation — PIL has no clipping mask out of the box.)


def chip(d, x, y, label, color, *, fg=(255, 255, 255)):
    text_f = f(HELV_NEUE, 11, idx=1)
    pad_x, pad_y = 8, 4
    bw = tw(d, label, text_f) + pad_x * 2
    bh = 18
    d.rounded_rectangle((x, y, x + bw, y + bh),
                        radius=4, fill=(*color, 255))
    d.text((x + pad_x, y + 3), label, font=text_f, fill=(*fg, 255))
    return bw + 8


def main():
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    img = Image.new("RGBA", (W, H), (*PAGE, 255))
    d = ImageDraw.Draw(img)

    # ── Doc title strip ──────────────────────────────────────────────
    d.text((72, 36), "gemba",
           font=f(HELV_NEUE, 22, idx=1), fill=(*INK, 255))
    d.text((144, 41), "·  system architecture",
           font=f(HELV_NEUE, 14, idx=0), fill=(*DIM, 255))
    # Right-side meta: "core types  ·  capability-driven UI".
    meta = "single-binary  ·  capability-driven UI"
    meta_f = f(MENLO, 11, idx=0)
    mw = tw(d, meta, meta_f)
    d.text((W - mw - 72, 44), meta, font=meta_f, fill=(*DIM, 255))

    # Subtle horizontal divider under the title.
    d.line([(72, 78), (W - 72, 78)], fill=(*BORDER, 255), width=1)

    # ── Tier 1: SPA ──────────────────────────────────────────────────
    t1 = (72, 110, W - 72, 200)  # x0,y0,x1,y1
    shadowed_round_rect(img, t1[0], t1[1], t1[2] - t1[0], t1[3] - t1[1])
    d = ImageDraw.Draw(img)
    stripe(d, t1[0], t1[1], t1[2] - t1[0], t1[3] - t1[1], ACCENT["spa"])
    # Header strip (light tint for header area).
    d.rectangle((t1[0] + 4, t1[1] + 1, t1[2] - 1, t1[1] + 36),
                fill=(*HEADER, 255))
    d.line([(t1[0] + 4, t1[1] + 36), (t1[2] - 1, t1[1] + 36)],
           fill=(*BORDER, 255), width=1)
    # Header text.
    d.text((t1[0] + 18, t1[1] + 9), "Gemba SPA",
           font=f(HELV_NEUE, 16, idx=1), fill=(*INK, 255))
    d.text((t1[0] + 130, t1[1] + 11), "React  /  TypeScript",
           font=f(HELV_NEUE, 13, idx=0), fill=(*DIM, 255))
    # Right-aligned chip.
    chip_x = t1[2] - 12 - tw(d, "PRESENTATION", f(HELV_NEUE, 11, idx=1)) - 16
    chip(d, chip_x, t1[1] + 9, "PRESENTATION", ACCENT["spa"])
    # Body bullets.
    d.text((t1[0] + 18, t1[1] + 56),
           "no role names  ·  no pack vocabulary  ·  capability-driven",
           font=f(MENLO, 13, idx=0), fill=(*INK, 255))
    d.text((t1[0] + 18, t1[1] + 80),
           "renders only what the bound adaptor's manifest declares",
           font=f(MENLO, 12, idx=0), fill=(*DIM, 255))

    # ── Connector 1 → 2: HTTP / SSE ─────────────────────────────────
    cx = W // 2
    cy_top = t1[3]  # 200
    cy_bot = 240
    d.line([(cx, cy_top), (cx, cy_bot)], fill=(*INK, 255), width=2)
    # Arrowheads at both ends — bidirectional.
    d.polygon([(cx - 6, cy_top + 8), (cx + 6, cy_top + 8), (cx, cy_top)],
              fill=(*INK, 255))
    d.polygon([(cx - 6, cy_bot - 8), (cx + 6, cy_bot - 8), (cx, cy_bot)],
              fill=(*INK, 255))
    # Annotation.
    annot = "HTTP  ·  SSE  —  capability-negotiated"
    annot_f = f(MENLO, 12, idx=0)
    aw = tw(d, annot, annot_f)
    # Background rect so the annotation sits cleanly over any
    # potential connector overlap (and because schematic diagrams
    # always pad their inline labels).
    d.rectangle((cx + 16, 211, cx + 16 + aw + 16, 232), fill=(*PAGE, 255))
    d.text((cx + 24, 214), annot, font=annot_f, fill=(*INK, 255))

    # ── Tier 2: Core ─────────────────────────────────────────────────
    t2 = (72, 250, W - 72, 380)
    shadowed_round_rect(img, t2[0], t2[1], t2[2] - t2[0], t2[3] - t2[1])
    d = ImageDraw.Draw(img)
    stripe(d, t2[0], t2[1], t2[2] - t2[0], t2[3] - t2[1], ACCENT["core"])
    d.rectangle((t2[0] + 4, t2[1] + 1, t2[2] - 1, t2[1] + 36),
                fill=(*HEADER, 255))
    d.line([(t2[0] + 4, t2[1] + 36), (t2[2] - 1, t2[1] + 36)],
           fill=(*BORDER, 255), width=1)
    d.text((t2[0] + 18, t2[1] + 9), "Gemba core",
           font=f(HELV_NEUE, 16, idx=1), fill=(*INK, 255))
    d.text((t2[0] + 130, t2[1] + 11), "Go binary  ·  embeds the SPA",
           font=f(HELV_NEUE, 13, idx=0), fill=(*DIM, 255))
    chip_x = t2[2] - 12 - tw(d, "TYPES + RUNTIME", f(HELV_NEUE, 11, idx=1)) - 16
    chip(d, chip_x, t2[1] + 9, "TYPES + RUNTIME", ACCENT["core"])
    # Type list — two rows with emphasis on the canonical primitives.
    d.text((t2[0] + 18, t2[1] + 54), "types:",
           font=f(MENLO, 13, idx=1), fill=(*DIM, 255))
    d.text((t2[0] + 80, t2[1] + 54),
           "WorkItem  ·  AgentRef  ·  Relationship  ·  Evidence  ·  DoD",
           font=f(MENLO, 13, idx=1), fill=(*INK, 255))
    d.text((t2[0] + 80, t2[1] + 76),
           "Sprint  ·  TokenBudget  ·  CostMeter  ·  EscalationRequest",
           font=f(MENLO, 13, idx=1), fill=(*INK, 255))
    d.text((t2[0] + 18, t2[1] + 102),
           "registers exactly one WorkPlaneAdaptor and at most one OrchestrationPlaneAdaptor",
           font=f(MENLO, 12, idx=0), fill=(*DIM, 255))

    # ── Connector 2 → 3: branching to two adaptor columns ────────────
    # All adaptor metadata lives inside the Tier 3 cards themselves;
    # the connector region stays clean (lines + arrowheads only) so
    # the diagram reads as schematic, not annotated.
    bus_top = t2[3]   # 380
    bus_y = 412
    bus_bot = 460
    d.line([(cx, bus_top), (cx, bus_y)], fill=(*INK, 255), width=2)
    left_cx = 72 + (W - 144) // 4
    right_cx = W - 72 - (W - 144) // 4
    d.line([(left_cx, bus_y), (right_cx, bus_y)],
           fill=(*INK, 255), width=2)
    d.line([(left_cx, bus_y), (left_cx, bus_bot)],
           fill=(*INK, 255), width=2)
    d.line([(right_cx, bus_y), (right_cx, bus_bot)],
           fill=(*INK, 255), width=2)
    d.polygon([(left_cx - 6, bus_bot - 8),
               (left_cx + 6, bus_bot - 8),
               (left_cx, bus_bot)], fill=(*INK, 255))
    d.polygon([(right_cx - 6, bus_bot - 8),
               (right_cx + 6, bus_bot - 8),
               (right_cx, bus_bot)], fill=(*INK, 255))

    # Edge label hanging off the vertical from core: makes the binding
    # type explicit without overcrowding the connector region.
    edge_label = "registers WorkPlaneAdaptor (1)  +  OrchestrationPlaneAdaptor (0..1)"
    el_f = f(MENLO, 11, idx=0)
    elw = tw(d, edge_label, el_f)
    d.rectangle((cx - elw // 2 - 12, 388, cx + elw // 2 + 12, 406),
                fill=(*PAGE, 255))
    d.text((cx - elw // 2, 391), edge_label, font=el_f, fill=(*DIM, 255))

    # ── Tier 3: two adaptor cards side by side ───────────────────────
    col_w = (W - 144 - 32) // 2
    t3_y = 484
    t3_h = 296

    def adaptor_card(x, y, w, h, color, name, role, examples_groups):
        shadowed_round_rect(img, x, y, w, h)
        d = ImageDraw.Draw(img)
        stripe(d, x, y, w, h, color)
        # Two-line header: name + role chip on row 1, transport on row 2.
        d.rectangle((x + 4, y + 1, x + w - 1, y + 56),
                    fill=(*HEADER, 255))
        d.line([(x + 4, y + 56), (x + w - 1, y + 56)],
               fill=(*BORDER, 255), width=1)
        d.text((x + 18, y + 11), name,
               font=f(HELV_NEUE, 16, idx=1), fill=(*INK, 255))
        chip_x = x + w - 12 - tw(d, role, f(HELV_NEUE, 11, idx=1)) - 16
        chip(d, chip_x, y + 11, role, color)
        d.text((x + 18, y + 36),
               "transport:  api  ·  jsonl  ·  mcp   ─   wire-format negotiated at startup",
               font=f(MENLO, 11, idx=0), fill=(*DIM, 255))

        # Body — labelled groups of examples.
        cy = y + 76
        for group_label, items in examples_groups:
            d.text((x + 18, cy), group_label,
                   font=f(HELV_NEUE, 11, idx=1), fill=(*DIM, 255))
            cy += 18
            for itm in items:
                d.text((x + 18, cy), itm,
                       font=f(MENLO, 13, idx=1), fill=(*INK, 255))
                cy += 22
            cy += 6

    adaptor_card(72, t3_y, col_w, t3_h, ACCENT["wp"],
                 "WorkPlane", "REQUIRED",
                 examples_groups=[
                     ("OUT-OF-THE-BOX", ["Beads"]),
                     ("FORCING FUNCTION", ["Jira"]),
                     ("FUTURE", ["Linear  ·  GitHub Projects  ·  …"]),
                 ])
    adaptor_card(72 + col_w + 32, t3_y, col_w, t3_h, ACCENT["orch"],
                 "OrchestrationPlane", "OPTIONAL",
                 examples_groups=[
                     ("OUT-OF-THE-BOX", ["Native  (terminal multiplexer)"]),
                     ("OPTIONAL", ["Gas Town  ·  LangGraph  ·  Gas City",
                                   "OpenHands  ·  CrewAI  ·  …"]),
                 ])

    # ── Connector 3 → 4: foundation underpins both adaptor columns ──
    # Visual semantics: the analysis store *underpins* both planes
    # rather than being consumed by them. We draw two thin vertical
    # lines from each adaptor's bottom-center down to the foundation
    # card's top — without arrowheads — so the foundation reads as a
    # bedrock layer rather than a downstream consumer.
    f_y = 824
    f_h = 130
    f_top = f_y
    adaptor_bottom = t3_y + t3_h  # 780
    d.line([(left_cx, adaptor_bottom), (left_cx, f_top)],
           fill=(*SOFT, 255), width=1)
    d.line([(right_cx, adaptor_bottom), (right_cx, f_top)],
           fill=(*SOFT, 255), width=1)

    # ── Tier 4: Source Code Analysis Store (foundation) ─────────────
    # Distinct from the upper tiers in two ways: (a) top-stripe
    # instead of left-stripe so the bar reads as a base/floor rather
    # than a sidebar, (b) labelled FOUNDATION with the violet accent
    # so the legend tells the operator this is its own concept tier.
    t4 = (72, f_y, W - 72, f_y + f_h)
    shadowed_round_rect(img, t4[0], t4[1], t4[2] - t4[0], t4[3] - t4[1])
    d = ImageDraw.Draw(img)
    stripe(d, t4[0], t4[1], t4[2] - t4[0], t4[3] - t4[1],
           ACCENT["analysis"], side="top")
    d.rectangle((t4[0] + 1, t4[1] + 4, t4[2] - 1, t4[1] + 40),
                fill=(*HEADER, 255))
    d.line([(t4[0] + 1, t4[1] + 40), (t4[2] - 1, t4[1] + 40)],
           fill=(*BORDER, 255), width=1)
    d.text((t4[0] + 18, t4[1] + 13), "Source Code Analysis Store",
           font=f(HELV_NEUE, 16, idx=1), fill=(*INK, 255))
    d.text((t4[0] + 282, t4[1] + 15), "e.g.  GitNexus",
           font=f(HELV_NEUE, 13, idx=0), fill=(*DIM, 255))
    chip_x = t4[2] - 12 - tw(d, "FOUNDATION", f(HELV_NEUE, 11, idx=1)) - 16
    chip(d, chip_x, t4[1] + 13, "FOUNDATION", ACCENT["analysis"])
    # Body — what the foundation provides.
    d.text((t4[0] + 18, t4[1] + 58),
           "indexes the repository as a graph: symbols  ·  call edges  ·  imports  "
           "·  references  ·  execution flows",
           font=f(MENLO, 12, idx=1), fill=(*INK, 255))
    d.text((t4[0] + 18, t4[1] + 82),
           "powers efficient agentic software development — impact analysis "
           "before edits, 360° symbol context, safe refactor / rename",
           font=f(MENLO, 12, idx=0), fill=(*DIM, 255))
    d.text((t4[0] + 18, t4[1] + 104),
           "consumed via MCP by every spawned agent; queried by the SPA's "
           "Graph + drift surfaces; underpins both planes",
           font=f(MENLO, 12, idx=0), fill=(*DIM, 255))

    # ── Footer / legend ──────────────────────────────────────────────
    foot_y = H - 56
    d.line([(72, foot_y - 6), (W - 72, foot_y - 6)],
           fill=(*BORDER, 255), width=1)
    legend_f = f(MENLO, 11, idx=0)
    legend_items = [
        ("SPA", ACCENT["spa"]),
        ("core", ACCENT["core"]),
        ("WorkPlane (REQUIRED)", ACCENT["wp"]),
        ("OrchestrationPlane (OPTIONAL)", ACCENT["orch"]),
        ("Analysis Store (FOUNDATION)", ACCENT["analysis"]),
    ]
    x = 72
    for label, color in legend_items:
        d.rectangle((x, foot_y + 8, x + 12, foot_y + 20),
                    fill=(*color, 255))
        d.text((x + 18, foot_y + 6), label,
               font=legend_f, fill=(*INK, 255))
        x += 18 + tw(d, label, legend_f) + 28
    foot_text = "github.com/MikeBengtson/gemba"
    fw = tw(d, foot_text, legend_f)
    d.text((W - 72 - fw, foot_y + 6), foot_text,
           font=legend_f, fill=(*DIM, 255))

    img.save(OUT, "PNG", optimize=True)
    print(f"wrote {OUT} ({os.path.getsize(OUT) // 1024} KB)")


if __name__ == "__main__":
    main()
