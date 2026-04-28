#!/usr/bin/env python3
"""
Generate five 1280x640 GitHub social-preview PNGs for gemba.

Each variation captures the spirit: human-ordered workflow orchestration
dispatched to AI agents. Aesthetic references are pulled from the most
heavily-trafficked agentic-AI repos (Anthropic, langchain/langgraph,
microsoft/autogen, openai-cookbook, charmbracelet, vercel/ai).

Outputs: social-1-{name}.png ... social-5-{name}.png at 1280x640 RGBA.
PNGs preserve their alpha channel; for variations with a strong solid
background the alpha channel is fully opaque (transparency preserved
in the file format, not as a literal see-through canvas — most social-
preview consumers composite onto white anyway).

Run: `python3 branding/social/generate.py`.
"""

import os
from PIL import Image, ImageDraw, ImageFont, ImageFilter

W, H = 1280, 640
OUT_DIR = os.path.dirname(os.path.abspath(__file__))

HELV = "/System/Library/Fonts/Helvetica.ttc"
HELV_NEUE = "/System/Library/Fonts/HelveticaNeue.ttc"
MENLO = "/System/Library/Fonts/Menlo.ttc"
GEORGIA = "/System/Library/Fonts/Supplemental/Georgia.ttf"
GEORGIA_BOLD = "/System/Library/Fonts/Supplemental/Georgia Bold.ttf"


def font(path, size, index=0):
    return ImageFont.truetype(path, size, index=index)


def text_size(draw, text, fnt):
    bbox = draw.textbbox((0, 0), text, font=fnt)
    return bbox[2] - bbox[0], bbox[3] - bbox[1]


# Priority palette borrowed from the Topbar/EpicCard color story so the
# visual identity stays coherent with the running app.
PRI = {
    "p0": (244, 63, 94),     # rose-500
    "p1": (249, 115, 22),    # orange-500
    "p2": (245, 158, 11),    # amber-500
    "p3": (14, 165, 233),    # sky-500
    "ink": (15, 18, 24),     # near-black
    "off": (250, 250, 249),  # warm white
    "violet": (139, 92, 246),
    "emerald": (16, 185, 129),
}


def save(img, name):
    out = os.path.join(OUT_DIR, name)
    img.save(out, "PNG", optimize=True)
    print(f"wrote {out} ({os.path.getsize(out) // 1024} KB)")


# ---------------------------------------------------------------------------
# Variation 1: dark+neon "kanban -> agents"
# Reference vibe: vercel/ai, anthropic/anthropic-sdk-typescript, openai-python.
# ---------------------------------------------------------------------------
def variation_1_dark_kanban():
    img = Image.new("RGBA", (W, H), (10, 11, 16, 255))
    d = ImageDraw.Draw(img)

    # Subtle grid backdrop — barely-there 1px lines, evokes "structured
    # workflow space" without competing with the headline.
    grid = (28, 30, 38, 255)
    for x in range(0, W, 64):
        d.line([(x, 0), (x, H)], fill=grid, width=1)
    for y in range(0, H, 64):
        d.line([(0, y), (W, y)], fill=grid, width=1)

    # Headline.
    title_f = font(HELV, 156, index=1)
    d.text((72, 96), "gemba", font=title_f, fill=(250, 250, 249, 255))

    # Tagline — monospaced, dim, sits under the wordmark.
    tag_f = font(MENLO, 26, index=0)
    d.text(
        (78, 268),
        "human-ordered workflow  →  AI agents",
        font=tag_f,
        fill=(168, 172, 184, 255),
    )

    # Right side: 3 kanban-card rectangles with priority stripes -> arrow ->
    # 3 agent dots arranged vertically. Cards drawn with a tiny offset
    # stack to suggest "queue".
    card_x0 = 720
    card_w, card_h = 240, 90
    gap = 18
    card_top = 200
    stripe_w = 6
    card_fill = (24, 26, 33, 255)
    card_border = (52, 55, 65, 255)
    stripes = [PRI["p0"], PRI["p1"], PRI["p2"]]
    titles = ["GM-E4.2  P0", "GM-PD2  P1", "GM-LQ1  P2"]
    sub = ["openapi spec", "epic anatomy", "output policy"]
    for i in range(3):
        x = card_x0
        y = card_top + i * (card_h + gap)
        # Card background.
        d.rounded_rectangle(
            [(x, y), (x + card_w, y + card_h)],
            radius=10,
            fill=card_fill,
            outline=card_border,
            width=1,
        )
        # Priority stripe along left edge.
        d.rectangle(
            [(x, y), (x + stripe_w, y + card_h)],
            fill=(*stripes[i], 255),
        )
        # Card text.
        d.text(
            (x + 18, y + 18),
            titles[i],
            font=font(MENLO, 18, index=1),
            fill=(228, 230, 240, 255),
        )
        d.text(
            (x + 18, y + 50),
            sub[i],
            font=font(MENLO, 16, index=0),
            fill=(140, 145, 158, 255),
        )

    # Branching arrow — bus from cards down/across to each agent so the
    # "1 → N dispatch" reading is unambiguous.
    arr_x = card_x0 + card_w + 24
    arr_color = (140, 145, 158, 255)
    bus_x = arr_x + 16
    cards_mid = card_top + (card_h * 3 + gap * 2) // 2
    # Vertical bus line.
    d.line(
        [(bus_x, card_top + card_h // 2), (bus_x, card_top + 2 * (card_h + gap) + card_h // 2)],
        fill=arr_color,
        width=2,
    )
    # One short horizontal stub + arrowhead per agent row.
    for i in range(3):
        ay = card_top + i * (card_h + gap) + card_h // 2
        d.line([(arr_x, ay), (bus_x, ay)], fill=arr_color, width=2)
        d.line([(bus_x, ay), (bus_x + 60, ay)], fill=arr_color, width=2)
        d.polygon(
            [(bus_x + 60, ay - 6), (bus_x + 72, ay), (bus_x + 60, ay + 6)],
            fill=arr_color,
        )

    # Three agent dots — concentric for a soft glow.
    agent_x = bus_x + 130
    glow_colors = [PRI["emerald"], PRI["violet"], PRI["p3"]]
    for i, color in enumerate(glow_colors):
        cy = card_top + i * (card_h + gap) + card_h // 2
        # Halo on its own RGBA layer so alpha composites cleanly over the grid.
        halo = Image.new("RGBA", (W, H), (0, 0, 0, 0))
        ImageDraw.Draw(halo).ellipse(
            [(agent_x - 30, cy - 30), (agent_x + 30, cy + 30)], fill=(*color, 60)
        )
        img.alpha_composite(halo)
        d.ellipse([(agent_x - 18, cy - 18), (agent_x + 18, cy + 18)], fill=(*color, 255))
        d.ellipse([(agent_x - 6, cy - 8), (agent_x + 4, cy + 0)], fill=(255, 255, 255, 180))

    # Footer: repo handle + tagline. Right-side text width-checked so it
    # never clips the edge.
    foot_f = font(MENLO, 18, index=0)
    d.text((72, H - 60), "MikeBengtson/gemba", font=foot_f, fill=(120, 124, 138, 255))
    foot_right = "single-binary · adaptor-agnostic"
    fw, _ = text_size(d, foot_right, foot_f)
    d.text((W - fw - 72, H - 60), foot_right, font=foot_f, fill=(120, 124, 138, 255))

    return img


# ---------------------------------------------------------------------------
# Variation 2: light minimalist serif-forward
# Reference vibe: linear, stripe docs, anthropic.com. Whitespace + serif
# wordmark + a single narrative sentence.
# ---------------------------------------------------------------------------
def variation_2_light_serif():
    img = Image.new("RGBA", (W, H), (250, 250, 249, 255))
    d = ImageDraw.Draw(img)

    # Wordmark.
    title_f = font(GEORGIA_BOLD, 200)
    tw, th = text_size(d, "gemba", title_f)
    d.text(((W - tw) // 2, 130), "gemba", font=title_f, fill=PRI["ink"] + (255,))

    # Sub-tagline in small caps via spacing trick (Pillow has no
    # smallcaps; use uppercase with letter-spacing).
    sub_text = "O P E R A T O R - O R D E R E D    ·    A G E N T - D I S P A T C H E D"
    sub_f = font(HELV_NEUE, 22, index=1)
    sw, sh = text_size(d, sub_text, sub_f)
    d.text(((W - sw) // 2, 360), sub_text, font=sub_f, fill=(80, 84, 96, 255))

    # Three small dots representing a workflow pipeline.
    cx = W // 2
    dot_y = 460
    dot_r = 6
    spacing = 70
    line_color = (180, 184, 196, 255)
    for i, color in enumerate([PRI["p0"], PRI["ink"], PRI["emerald"]]):
        x = cx - spacing + i * spacing
        # Connecting line.
        if i < 2:
            d.line([(x + dot_r + 8, dot_y), (x + spacing - dot_r - 8, dot_y)], fill=line_color, width=2)
        d.ellipse([(x - dot_r, dot_y - dot_r), (x + dot_r, dot_y + dot_r)], fill=(*color, 255))

    # Footer in mono.
    foot_f = font(MENLO, 18, index=0)
    foot_text = "github.com/MikeBengtson/gemba"
    fw, _ = text_size(d, foot_text, foot_f)
    d.text(((W - fw) // 2, H - 64), foot_text, font=foot_f, fill=(140, 144, 156, 255))

    return img


# ---------------------------------------------------------------------------
# Variation 3: brutalist mono ASCII
# Reference vibe: charmbracelet/bubbletea, langgraph, autogen. ASCII
# diagram doing the talking; everything mono.
# ---------------------------------------------------------------------------
def variation_3_brutalist_mono():
    img = Image.new("RGBA", (W, H), (15, 18, 24, 255))
    d = ImageDraw.Draw(img)

    # Heavy mono wordmark.
    head_f = font(MENLO, 132, index=1)
    d.text((72, 60), "GEMBA", font=head_f, fill=(250, 250, 249, 255))

    # ASCII flow diagram.
    diag_f = font(MENLO, 26, index=0)
    diag = [
        "[ operator ]  ──→  [ orchestrator ]  ──┬─→  [ agent  · pm   ]",
        "                                       ├─→  [ agent  · ops  ]",
        "                                       ├─→  [ agent  · qa   ]",
        "                                       └─→  [ agent  · docs ]",
    ]
    y = 240
    for line in diag:
        d.text((72, y), line, font=diag_f, fill=(220, 224, 232, 255))
        y += 38

    # Tagline.
    tag_f = font(MENLO, 28, index=1)
    d.text((72, H - 130), "you order.  they execute.", font=tag_f, fill=(*PRI["p1"], 255))

    # Footer.
    foot_f = font(MENLO, 18, index=0)
    d.text(
        (72, H - 60),
        "MikeBengtson/gemba   ·   single-binary · workplane × orchestrationplane",
        font=foot_f,
        fill=(120, 124, 138, 255),
    )

    return img


# ---------------------------------------------------------------------------
# Variation 4: terminal/CLI window
# Reference vibe: charmbracelet, gh CLI, textual, ollama. A faux-terminal
# with a real-feeling command + output excerpt.
# ---------------------------------------------------------------------------
def variation_4_terminal():
    img = Image.new("RGBA", (W, H), (8, 10, 14, 255))
    d = ImageDraw.Draw(img)

    # Terminal window chrome.
    pad = 64
    win = (pad, pad, W - pad, H - pad)
    d.rounded_rectangle(win, radius=14, fill=(20, 22, 28, 255), outline=(48, 52, 62, 255), width=1)

    # Title bar.
    bar_h = 36
    d.rounded_rectangle(
        (win[0], win[1], win[2], win[1] + bar_h),
        radius=14,
        fill=(28, 30, 38, 255),
    )
    # Square off the bottom of the title bar by overpainting.
    d.rectangle(
        (win[0], win[1] + bar_h - 14, win[2], win[1] + bar_h),
        fill=(28, 30, 38, 255),
    )
    # Traffic-light dots.
    for i, c in enumerate([(255, 95, 86), (255, 189, 46), (39, 201, 63)]):
        cx = win[0] + 22 + i * 22
        cy = win[1] + bar_h // 2
        d.ellipse([(cx - 7, cy - 7), (cx + 7, cy + 7)], fill=(*c, 255))
    # Title.
    title_f = font(MENLO, 16, index=0)
    title_text = "gemba serve  ·  /Users/you/your-rig"
    tw, _ = text_size(d, title_text, title_f)
    d.text(((W - tw) // 2, win[1] + bar_h // 2 - 10), title_text, font=title_f, fill=(170, 174, 186, 255))

    # Body text.
    body_f = font(MENLO, 22, index=0)
    bold_f = font(MENLO, 22, index=1)
    x = win[0] + 28
    y = win[1] + bar_h + 24
    line_h = 30

    def line(text, color=(220, 224, 232, 255), fnt=body_f):
        nonlocal y
        d.text((x, y), text, font=fnt, fill=color)
        y += line_h

    line("$ gemba serve --beads-dir .", color=(*PRI["emerald"], 255), fnt=bold_f)
    line("→ workplane:    bd  (44 epics, 312 work items)", color=(190, 194, 206, 255))
    line("→ orchestrator: native  (4 agents available)", color=(190, 194, 206, 255))
    line("→ ui:           http://127.0.0.1:7666/", color=(190, 194, 206, 255))
    y += 8
    line("$ gemba dispatch gm-e4.2 --to deployment-engineer", color=(*PRI["emerald"], 255), fnt=bold_f)
    line("✓ slung gm-e4.2 → session a91415c1  ·  watching for handoff", color=(*PRI["emerald"], 255))
    y += 8
    line("$ _", color=(250, 250, 249, 255), fnt=bold_f)
    # Blinking cursor block — drawn as a solid rect after the underscore.
    cursor_x = x + text_size(d, "$ _", bold_f)[0] + 2
    cursor_y = y - line_h
    d.rectangle(
        (cursor_x, cursor_y + 4, cursor_x + 12, cursor_y + 26),
        fill=(250, 250, 249, 255),
    )

    return img


# ---------------------------------------------------------------------------
# Variation 5: gradient hero + isometric-ish dispatch blocks
# Reference vibe: openai-cookbook, anthropic quickstarts, e2b, all-hands AI.
# Modern marketing aesthetic.
# ---------------------------------------------------------------------------
def variation_5_gradient_dispatch():
    img = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    # Vertical gradient deep navy → violet → rose.
    top = (15, 18, 48)
    mid = (76, 36, 136)
    bot = (200, 60, 110)
    for y in range(H):
        if y < H // 2:
            t = y / (H // 2)
            r = int(top[0] + (mid[0] - top[0]) * t)
            g = int(top[1] + (mid[1] - top[1]) * t)
            b = int(top[2] + (mid[2] - top[2]) * t)
        else:
            t = (y - H // 2) / (H // 2)
            r = int(mid[0] + (bot[0] - mid[0]) * t)
            g = int(mid[1] + (bot[1] - mid[1]) * t)
            b = int(mid[2] + (bot[2] - mid[2]) * t)
        ImageDraw.Draw(img).line([(0, y), (W, y)], fill=(r, g, b, 255))
    d = ImageDraw.Draw(img)

    # Headline.
    title_f = font(HELV_NEUE, 168, index=1)
    d.text((72, 88), "gemba", font=title_f, fill=(255, 255, 255, 255))

    # Subhead — wrap to two lines so it never collides with the right-
    # side tile column.
    sub_f = font(HELV_NEUE, 28, index=0)
    d.text(
        (78, 280),
        "operator-ordered work,",
        font=sub_f,
        fill=(255, 255, 255, 230),
    )
    d.text(
        (78, 318),
        "dispatched to the right agent.",
        font=sub_f,
        fill=(255, 255, 255, 230),
    )

    # Right-side stack of "card → agent" tiles. Drawn on a separate
    # transparent layer + alpha-composited so semi-transparent fills
    # actually blend over the gradient (PIL's draw replaces, doesn't
    # composite, when the canvas already has pixels).
    base_x = 720
    base_y = 100
    tile_w, tile_h = 488, 88
    rows = [
        ("EPIC", "gm-e4.2", PRI["p0"], "deployment-engineer"),
        ("FEAT", "gm-pd2",  PRI["p1"], "ux-pm"),
        ("REFR", "gm-6av",  PRI["p2"], "core-platform"),
        ("DOCS", "gm-77u",  PRI["p3"], "documentarian"),
    ]
    overlay = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    od = ImageDraw.Draw(overlay)
    for i, (kind, bead_id, color, agent) in enumerate(rows):
        y = base_y + i * (tile_h + 12)
        od.rounded_rectangle(
            (base_x, y, base_x + tile_w, y + tile_h),
            radius=12,
            fill=(255, 255, 255, 36),
            outline=(255, 255, 255, 110),
            width=1,
        )
        od.rectangle(
            (base_x, y, base_x + 6, y + tile_h),
            fill=(*color, 255),
        )
    img.alpha_composite(overlay)
    # Re-bind draw context to the composited image so subsequent text
    # lands on top of the tiles.
    d = ImageDraw.Draw(img)
    for i, (kind, bead_id, color, agent) in enumerate(rows):
        y = base_y + i * (tile_h + 12)
        d.text(
            (base_x + 22, y + 14),
            kind,
            font=font(MENLO, 14, index=1),
            fill=(255, 255, 255, 230),
        )
        d.text(
            (base_x + 22, y + 36),
            bead_id,
            font=font(MENLO, 22, index=1),
            fill=(255, 255, 255, 255),
        )
        d.text(
            (base_x + 200, y + 36),
            "→",
            font=font(MENLO, 22, index=1),
            fill=(255, 255, 255, 220),
        )
        d.text(
            (base_x + 240, y + 36),
            agent,
            font=font(MENLO, 20, index=0),
            fill=(255, 255, 255, 240),
        )

    # Footer.
    foot_f = font(MENLO, 18, index=0)
    d.text(
        (72, H - 60),
        "github.com/MikeBengtson/gemba",
        font=foot_f,
        fill=(255, 255, 255, 200),
    )
    return img


def main():
    variations = [
        ("social-1-dark-kanban.png", variation_1_dark_kanban),
        ("social-2-light-serif.png", variation_2_light_serif),
        ("social-3-brutalist-mono.png", variation_3_brutalist_mono),
        ("social-4-terminal.png", variation_4_terminal),
        ("social-5-gradient-dispatch.png", variation_5_gradient_dispatch),
    ]
    for name, fn in variations:
        save(fn(), name)


if __name__ == "__main__":
    main()
