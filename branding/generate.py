#!/usr/bin/env python3
"""
Generate paired GitHub social previews (1280x640) and README banners
(1280x320) for gemba.

Each of the 10 themes ships TWO images that share a palette, typography
choice, and visual motif so a project page reads as a coherent set:

  branding/social/social-NN-name.png   → repo Settings · Social preview
  branding/banner/banner-NN-name.png   → top of README.md (markdown img)

Why 10? gemba's identity has multiple readings — operator-ordered
dispatch, adaptor-agnostic, capability-aware, persona-driven, single-
binary — and each theme leans on one. Pick whichever one sells the
problem the audience already feels.

Run: `python3 branding/generate.py`
"""

import os
from PIL import Image, ImageDraw, ImageFont

W = 1280
H_SOCIAL = 640
H_BANNER = 320

ROOT = os.path.dirname(os.path.abspath(__file__))
OUT_SOCIAL = os.path.join(ROOT, "social")
OUT_BANNER = os.path.join(ROOT, "banner")

# ------ Fonts ----------------------------------------------------------
HELV = "/System/Library/Fonts/Helvetica.ttc"
HELV_NEUE = "/System/Library/Fonts/HelveticaNeue.ttc"
MENLO = "/System/Library/Fonts/Menlo.ttc"
GEORGIA_BOLD = "/System/Library/Fonts/Supplemental/Georgia Bold.ttf"


def font(path, size, index=0):
    return ImageFont.truetype(path, size, index=index)


def text_size(d, t, f):
    bbox = d.textbbox((0, 0), t, font=f)
    return bbox[2] - bbox[0], bbox[3] - bbox[1]


# ------ Shared palette -------------------------------------------------
PRI = {
    "p0": (244, 63, 94),
    "p1": (249, 115, 22),
    "p2": (245, 158, 11),
    "p3": (14, 165, 233),
    "ink": (15, 18, 24),
    "off": (250, 250, 249),
    "violet": (139, 92, 246),
    "emerald": (16, 185, 129),
    "cyan": (34, 211, 238),
    "rose": (244, 63, 94),
    "amber": (245, 158, 11),
    "sky": (14, 165, 233),
    "navy": (15, 23, 42),
    "navyDeep": (8, 12, 24),
    "indigo": (79, 70, 229),
    "fuchsia": (217, 70, 239),
}


def save(img, dest):
    os.makedirs(os.path.dirname(dest), exist_ok=True)
    img.save(dest, "PNG", optimize=True)
    print(f"wrote {dest} ({os.path.getsize(dest) // 1024} KB)")


def draw_check(d, cx, cy, size, color, width=3):
    """Render a checkmark as two strokes — works with any font (we don't
    use a glyph). cx/cy is the centre; size scales the strokes."""
    s = size
    d.line([(cx - int(s * 0.55), cy + int(s * 0.05)),
            (cx - int(s * 0.10), cy + int(s * 0.45))],
           fill=(*color, 255), width=width)
    d.line([(cx - int(s * 0.10), cy + int(s * 0.45)),
            (cx + int(s * 0.55), cy - int(s * 0.40))],
           fill=(*color, 255), width=width)


# =====================================================================
# Theme 01 — Dark kanban → agents
# =====================================================================
def _draw_kanban_motif(d, img, *, x0, y0, card_h, card_w, gap, agent_x_off, scale=1.0):
    """Three priority-stripe cards → branching arrow → three agent dots."""
    cards = [
        ("GM-E4.2  P0", "openapi spec", PRI["p0"]),
        ("GM-PD2  P1", "epic anatomy", PRI["p1"]),
        ("GM-LQ1  P2", "output policy", PRI["p2"]),
    ]
    title_size = int(18 * scale)
    sub_size = int(16 * scale)
    for i, (title, sub, color) in enumerate(cards):
        x = x0
        y = y0 + i * (card_h + gap)
        d.rounded_rectangle(
            [(x, y), (x + card_w, y + card_h)],
            radius=int(10 * scale),
            fill=(24, 26, 33, 255),
            outline=(52, 55, 65, 255),
            width=1,
        )
        d.rectangle([(x, y), (x + 6, y + card_h)], fill=(*color, 255))
        d.text((x + 18, y + int(card_h * 0.18)), title,
               font=font(MENLO, title_size, index=1),
               fill=(228, 230, 240, 255))
        d.text((x + 18, y + int(card_h * 0.55)), sub,
               font=font(MENLO, sub_size, index=0),
               fill=(140, 145, 158, 255))

    # Branching arrow.
    arr_x = x0 + card_w + int(24 * scale)
    bus_x = arr_x + int(16 * scale)
    arr_color = (140, 145, 158, 255)
    d.line([(bus_x, y0 + card_h // 2), (bus_x, y0 + 2 * (card_h + gap) + card_h // 2)],
           fill=arr_color, width=2)
    for i in range(3):
        ay = y0 + i * (card_h + gap) + card_h // 2
        d.line([(arr_x, ay), (bus_x, ay)], fill=arr_color, width=2)
        d.line([(bus_x, ay), (bus_x + agent_x_off - 12, ay)], fill=arr_color, width=2)
        d.polygon(
            [(bus_x + agent_x_off - 12, ay - 6),
             (bus_x + agent_x_off, ay),
             (bus_x + agent_x_off - 12, ay + 6)],
            fill=arr_color)

    # Agent dots.
    agents = [(PRI["emerald"]), (PRI["violet"]), (PRI["p3"])]
    agent_x = bus_x + agent_x_off + int(40 * scale)
    for i, color in enumerate(agents):
        cy = y0 + i * (card_h + gap) + card_h // 2
        halo = Image.new("RGBA", img.size, (0, 0, 0, 0))
        ImageDraw.Draw(halo).ellipse(
            [(agent_x - int(28 * scale), cy - int(28 * scale)),
             (agent_x + int(28 * scale), cy + int(28 * scale))],
            fill=(*color, 60))
        img.alpha_composite(halo)
        d.ellipse([(agent_x - int(18 * scale), cy - int(18 * scale)),
                   (agent_x + int(18 * scale), cy + int(18 * scale))],
                  fill=(*color, 255))
        d.ellipse([(agent_x - 5, cy - 7), (agent_x + 4, cy + 0)],
                  fill=(255, 255, 255, 180))


def _grid_bg(d, w, h, line_color, step=64):
    for x in range(0, w, step):
        d.line([(x, 0), (x, h)], fill=line_color, width=1)
    for y in range(0, h, step):
        d.line([(0, y), (w, y)], fill=line_color, width=1)


def social_01_dark_kanban():
    img = Image.new("RGBA", (W, H_SOCIAL), (10, 11, 16, 255))
    d = ImageDraw.Draw(img)
    _grid_bg(d, W, H_SOCIAL, (28, 30, 38, 255))
    d.text((72, 96), "gemba", font=font(HELV, 156, index=1), fill=(250, 250, 249, 255))
    d.text((78, 268), "human-ordered workflow  →  AI agents",
           font=font(MENLO, 26, index=0), fill=(168, 172, 184, 255))
    _draw_kanban_motif(d, img, x0=720, y0=200, card_h=90, card_w=240, gap=18, agent_x_off=72)
    foot_f = font(MENLO, 18, index=0)
    d.text((72, H_SOCIAL - 60), "GembaCore/gemba-core", font=foot_f, fill=(120, 124, 138, 255))
    fr = "single-binary · adaptor-agnostic"
    fw, _ = text_size(d, fr, foot_f)
    d.text((W - fw - 72, H_SOCIAL - 60), fr, font=foot_f, fill=(120, 124, 138, 255))
    return img


def banner_01_dark_kanban():
    img = Image.new("RGBA", (W, H_BANNER), (10, 11, 16, 255))
    d = ImageDraw.Draw(img)
    _grid_bg(d, W, H_BANNER, (28, 30, 38, 255), step=48)
    d.text((48, 56), "gemba", font=font(HELV, 96, index=1), fill=(250, 250, 249, 255))
    d.text((52, 168), "human-ordered workflow  →  AI agents",
           font=font(MENLO, 18, index=0), fill=(168, 172, 184, 255))
    d.text((52, 200), "GembaCore/gemba-core",
           font=font(MENLO, 16, index=0), fill=(120, 124, 138, 255))
    # Compact horizontal kanban → arrow → 3 stacked agent pips.
    cx = 600
    cy = 110
    cards = [(PRI["p0"], "P0"), (PRI["p1"], "P1"), (PRI["p2"], "P2")]
    for i, (color, label) in enumerate(cards):
        x = cx + i * 110
        d.rounded_rectangle([(x, cy), (x + 96, cy + 56)],
                            radius=8, fill=(24, 26, 33, 255),
                            outline=(52, 55, 65, 255), width=1)
        d.rectangle([(x, cy), (x + 5, cy + 56)], fill=(*color, 255))
        d.text((x + 14, cy + 16), label,
               font=font(MENLO, 18, index=1), fill=(228, 230, 240, 255))
    arr_x = cx + 3 * 110 - 8
    arr_color = (140, 145, 158, 255)
    d.line([(arr_x, cy + 28), (arr_x + 50, cy + 28)], fill=arr_color, width=2)
    d.polygon([(arr_x + 50, cy + 22), (arr_x + 62, cy + 28), (arr_x + 50, cy + 34)],
              fill=arr_color)
    agents_x = arr_x + 96
    for i, color in enumerate([PRI["emerald"], PRI["violet"], PRI["p3"]]):
        ax = agents_x + i * 50
        halo = Image.new("RGBA", img.size, (0, 0, 0, 0))
        ImageDraw.Draw(halo).ellipse([(ax - 22, cy + 8), (ax + 22, cy + 52)],
                                     fill=(*color, 60))
        img.alpha_composite(halo)
        d.ellipse([(ax - 14, cy + 14), (ax + 14, cy + 42)], fill=(*color, 255))
    return img


# =====================================================================
# Theme 02 — Light minimalist serif
# =====================================================================
def social_02_light_serif():
    img = Image.new("RGBA", (W, H_SOCIAL), (250, 250, 249, 255))
    d = ImageDraw.Draw(img)
    title_f = font(GEORGIA_BOLD, 200)
    tw, _ = text_size(d, "gemba", title_f)
    d.text(((W - tw) // 2, 130), "gemba", font=title_f, fill=PRI["ink"] + (255,))
    sub = "O P E R A T O R - O R D E R E D    ·    A G E N T - D I S P A T C H E D"
    sub_f = font(HELV_NEUE, 22, index=1)
    sw, _ = text_size(d, sub, sub_f)
    d.text(((W - sw) // 2, 360), sub, font=sub_f, fill=(80, 84, 96, 255))
    cx = W // 2
    dy = 460
    r = 6
    sp = 70
    for i, color in enumerate([PRI["p0"], PRI["ink"], PRI["emerald"]]):
        x = cx - sp + i * sp
        if i < 2:
            d.line([(x + r + 8, dy), (x + sp - r - 8, dy)], fill=(180, 184, 196, 255), width=2)
        d.ellipse([(x - r, dy - r), (x + r, dy + r)], fill=(*color, 255))
    foot = "github.com/GembaCore/gemba-core"
    foot_f = font(MENLO, 18, index=0)
    fw, _ = text_size(d, foot, foot_f)
    d.text(((W - fw) // 2, H_SOCIAL - 64), foot, font=foot_f, fill=(140, 144, 156, 255))
    return img


def banner_02_light_serif():
    img = Image.new("RGBA", (W, H_BANNER), (250, 250, 249, 255))
    d = ImageDraw.Draw(img)
    title_f = font(GEORGIA_BOLD, 132)
    d.text((68, 70), "gemba", font=title_f, fill=PRI["ink"] + (255,))
    sub = "O P E R A T O R - O R D E R E D    ·    A G E N T - D I S P A T C H E D"
    sub_f = font(HELV_NEUE, 17, index=1)
    d.text((72, 220), sub, font=sub_f, fill=(80, 84, 96, 255))
    # Right-side dot trio.
    dx, dy = 1010, 160
    sp = 50
    r = 5
    for i, color in enumerate([PRI["p0"], PRI["ink"], PRI["emerald"]]):
        x = dx + i * sp
        if i < 2:
            d.line([(x + r + 6, dy), (x + sp - r - 6, dy)],
                   fill=(180, 184, 196, 255), width=2)
        d.ellipse([(x - r, dy - r), (x + r, dy + r)], fill=(*color, 255))
    return img


# =====================================================================
# Theme 03 — Brutalist mono ASCII
# =====================================================================
def social_03_brutalist_mono():
    img = Image.new("RGBA", (W, H_SOCIAL), (15, 18, 24, 255))
    d = ImageDraw.Draw(img)
    d.text((72, 60), "GEMBA", font=font(MENLO, 132, index=1), fill=(250, 250, 249, 255))
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
    d.text((72, H_SOCIAL - 130), "you order.  they execute.",
           font=font(MENLO, 28, index=1), fill=(*PRI["p1"], 255))
    d.text((72, H_SOCIAL - 60),
           "GembaCore/gemba-core   ·   single-binary · workplane × orchestrationplane",
           font=font(MENLO, 18, index=0), fill=(120, 124, 138, 255))
    return img


def banner_03_brutalist_mono():
    img = Image.new("RGBA", (W, H_BANNER), (15, 18, 24, 255))
    d = ImageDraw.Draw(img)
    d.text((48, 36), "GEMBA", font=font(MENLO, 80, index=1), fill=(250, 250, 249, 255))
    diag_f = font(MENLO, 19, index=0)
    diag = [
        "[ operator ]  ──→  [ orchestrator ]  ──┬─→  [ agent · pm  ]",
        "                                       ├─→  [ agent · ops ]",
        "                                       └─→  [ agent · qa  ]",
    ]
    y = 144
    for line in diag:
        d.text((48, y), line, font=diag_f, fill=(220, 224, 232, 255))
        y += 26
    d.text((48, H_BANNER - 48), "you order.  they execute.",
           font=font(MENLO, 18, index=1), fill=(*PRI["p1"], 255))
    return img


# =====================================================================
# Theme 04 — Terminal / CLI window
# =====================================================================
def _terminal_window(d, img, *, win, with_cursor=True, lines):
    pad = win
    bar_h = 36
    d.rounded_rectangle(pad, radius=14, fill=(20, 22, 28, 255),
                        outline=(48, 52, 62, 255), width=1)
    d.rounded_rectangle((pad[0], pad[1], pad[2], pad[1] + bar_h),
                        radius=14, fill=(28, 30, 38, 255))
    d.rectangle((pad[0], pad[1] + bar_h - 14, pad[2], pad[1] + bar_h),
                fill=(28, 30, 38, 255))
    for i, c in enumerate([(255, 95, 86), (255, 189, 46), (39, 201, 63)]):
        cx = pad[0] + 22 + i * 22
        cy = pad[1] + bar_h // 2
        d.ellipse([(cx - 7, cy - 7), (cx + 7, cy + 7)], fill=(*c, 255))
    title_f = font(MENLO, 16, index=0)
    title = "gemba serve  ·  /Users/you/your-rig"
    tw, _ = text_size(d, title, title_f)
    d.text(((W - tw) // 2, pad[1] + bar_h // 2 - 10), title,
           font=title_f, fill=(170, 174, 186, 255))
    # Body lines.
    body_f = font(MENLO, 22, index=0)
    bold_f = font(MENLO, 22, index=1)
    x = pad[0] + 28
    y = pad[1] + bar_h + 24
    for text, kind in lines:
        f = bold_f if kind in ("cmd", "ok-bold") else body_f
        col = {
            "cmd": (*PRI["emerald"], 255),
            "ok": (*PRI["emerald"], 255),
            "ok-bold": (*PRI["emerald"], 255),
            "info": (190, 194, 206, 255),
            "white": (250, 250, 249, 255),
        }[kind]
        d.text((x, y), text, font=f, fill=col)
        y += 30
    if with_cursor:
        cursor_x = x + 22
        d.rectangle((cursor_x, y - 26, cursor_x + 12, y - 4),
                    fill=(250, 250, 249, 255))


def social_04_terminal():
    img = Image.new("RGBA", (W, H_SOCIAL), (8, 10, 14, 255))
    d = ImageDraw.Draw(img)
    win = (64, 64, W - 64, H_SOCIAL - 64)
    _terminal_window(d, img, win=win, with_cursor=True, lines=[
        ("$ gemba serve --project-dir .", "cmd"),
        ("→ workplane:    bd  (44 epics, 312 work items)", "info"),
        ("→ orchestrator: native  (4 agents available)", "info"),
        ("→ ui:           http://127.0.0.1:7666/", "info"),
        ("", "info"),
        ("$ gemba dispatch gm-e4.2 --to deployment-engineer", "cmd"),
        ("✓ slung gm-e4.2 → session a91415c1  ·  watching for handoff", "ok"),
        ("", "info"),
        ("$ ", "white"),
    ])
    return img


def banner_04_terminal():
    img = Image.new("RGBA", (W, H_BANNER), (8, 10, 14, 255))
    d = ImageDraw.Draw(img)
    win = (40, 32, W - 40, H_BANNER - 32)
    pad = win
    bar_h = 28
    d.rounded_rectangle(pad, radius=12, fill=(20, 22, 28, 255),
                        outline=(48, 52, 62, 255), width=1)
    d.rounded_rectangle((pad[0], pad[1], pad[2], pad[1] + bar_h),
                        radius=12, fill=(28, 30, 38, 255))
    d.rectangle((pad[0], pad[1] + bar_h - 12, pad[2], pad[1] + bar_h),
                fill=(28, 30, 38, 255))
    for i, c in enumerate([(255, 95, 86), (255, 189, 46), (39, 201, 63)]):
        cx = pad[0] + 18 + i * 18
        cy = pad[1] + bar_h // 2
        d.ellipse([(cx - 6, cy - 6), (cx + 6, cy + 6)], fill=(*c, 255))
    title_f = font(MENLO, 13, index=0)
    title = "gemba serve  ·  ~/your-rig"
    tw, _ = text_size(d, title, title_f)
    d.text(((W - tw) // 2, pad[1] + bar_h // 2 - 8), title,
           font=title_f, fill=(170, 174, 186, 255))
    body_f = font(MENLO, 18, index=0)
    bold_f = font(MENLO, 18, index=1)
    x = pad[0] + 22
    y = pad[1] + bar_h + 18
    lines = [
        ("$ gemba serve --project-dir .", PRI["emerald"], bold_f),
        ("→ workplane: bd · orchestrator: native · 4 agents", (190, 194, 206), body_f),
        ("$ gemba dispatch gm-e4.2 --to deployment-engineer", PRI["emerald"], bold_f),
        ("✓ slung gm-e4.2 → session a91415c1", PRI["emerald"], body_f),
    ]
    for text, color, f in lines:
        d.text((x, y), text, font=f, fill=(*color, 255))
        y += 26
    return img


# =====================================================================
# Theme 05 — Gradient + dispatch tiles
# =====================================================================
def _vertical_gradient(img, top, mid, bot):
    h = img.height
    d = ImageDraw.Draw(img)
    for y in range(h):
        if y < h // 2:
            t = y / (h // 2)
            r = int(top[0] + (mid[0] - top[0]) * t)
            g = int(top[1] + (mid[1] - top[1]) * t)
            b = int(top[2] + (mid[2] - top[2]) * t)
        else:
            t = (y - h // 2) / (h // 2)
            r = int(mid[0] + (bot[0] - mid[0]) * t)
            g = int(mid[1] + (bot[1] - mid[1]) * t)
            b = int(mid[2] + (bot[2] - mid[2]) * t)
        d.line([(0, y), (img.width, y)], fill=(r, g, b, 255))


def social_05_gradient_dispatch():
    img = Image.new("RGBA", (W, H_SOCIAL), (0, 0, 0, 0))
    _vertical_gradient(img, (15, 18, 48), (76, 36, 136), (200, 60, 110))
    d = ImageDraw.Draw(img)
    d.text((72, 88), "gemba", font=font(HELV_NEUE, 168, index=1), fill=(255, 255, 255, 255))
    d.text((78, 280), "operator-ordered work,",
           font=font(HELV_NEUE, 28, index=0), fill=(255, 255, 255, 230))
    d.text((78, 318), "dispatched to the right agent.",
           font=font(HELV_NEUE, 28, index=0), fill=(255, 255, 255, 230))

    base_x, base_y = 720, 100
    tile_w, tile_h = 488, 88
    rows = [
        ("EPIC", "gm-e4.2", PRI["p0"], "deployment-engineer"),
        ("FEAT", "gm-pd2",  PRI["p1"], "ux-pm"),
        ("REFR", "gm-6av",  PRI["p2"], "core-platform"),
        ("DOCS", "gm-77u",  PRI["p3"], "documentarian"),
    ]
    overlay = Image.new("RGBA", img.size, (0, 0, 0, 0))
    od = ImageDraw.Draw(overlay)
    for i, (kind, bead, color, agent) in enumerate(rows):
        y = base_y + i * (tile_h + 12)
        od.rounded_rectangle((base_x, y, base_x + tile_w, y + tile_h),
                             radius=12, fill=(255, 255, 255, 36),
                             outline=(255, 255, 255, 110), width=1)
        od.rectangle((base_x, y, base_x + 6, y + tile_h), fill=(*color, 255))
    img.alpha_composite(overlay)
    d = ImageDraw.Draw(img)
    for i, (kind, bead, color, agent) in enumerate(rows):
        y = base_y + i * (tile_h + 12)
        d.text((base_x + 22, y + 14), kind,
               font=font(MENLO, 14, index=1), fill=(255, 255, 255, 230))
        d.text((base_x + 22, y + 36), bead,
               font=font(MENLO, 22, index=1), fill=(255, 255, 255, 255))
        d.text((base_x + 200, y + 36), "→",
               font=font(MENLO, 22, index=1), fill=(255, 255, 255, 220))
        d.text((base_x + 240, y + 36), agent,
               font=font(MENLO, 20, index=0), fill=(255, 255, 255, 240))
    d.text((72, H_SOCIAL - 60), "github.com/GembaCore/gemba-core",
           font=font(MENLO, 18, index=0), fill=(255, 255, 255, 200))
    return img


def banner_05_gradient_dispatch():
    img = Image.new("RGBA", (W, H_BANNER), (0, 0, 0, 0))
    _vertical_gradient(img, (15, 18, 48), (90, 42, 150), (200, 60, 110))
    d = ImageDraw.Draw(img)
    d.text((52, 56), "gemba", font=font(HELV_NEUE, 96, index=1), fill=(255, 255, 255, 255))
    d.text((56, 168), "operator-ordered work,",
           font=font(HELV_NEUE, 18, index=0), fill=(255, 255, 255, 230))
    d.text((56, 198), "dispatched to the right agent.",
           font=font(HELV_NEUE, 18, index=0), fill=(255, 255, 255, 230))
    # Two compact tiles on the right.
    base_x = 700
    tile_w, tile_h = 540, 56
    rows = [
        ("EPIC", "gm-e4.2", PRI["p0"], "→ deployment-engineer"),
        ("FEAT", "gm-pd2",  PRI["p1"], "→ ux-pm"),
        ("REFR", "gm-6av",  PRI["p2"], "→ core-platform"),
    ]
    overlay = Image.new("RGBA", img.size, (0, 0, 0, 0))
    od = ImageDraw.Draw(overlay)
    for i, (kind, bead, color, agent) in enumerate(rows):
        y = 56 + i * (tile_h + 8)
        od.rounded_rectangle((base_x, y, base_x + tile_w, y + tile_h),
                             radius=10, fill=(255, 255, 255, 36),
                             outline=(255, 255, 255, 110), width=1)
        od.rectangle((base_x, y, base_x + 5, y + tile_h), fill=(*color, 255))
    img.alpha_composite(overlay)
    d = ImageDraw.Draw(img)
    for i, (kind, bead, color, agent) in enumerate(rows):
        y = 56 + i * (tile_h + 8)
        d.text((base_x + 16, y + 8), kind,
               font=font(MENLO, 11, index=1), fill=(255, 255, 255, 220))
        d.text((base_x + 16, y + 24), bead,
               font=font(MENLO, 16, index=1), fill=(255, 255, 255, 255))
        d.text((base_x + 130, y + 26), agent,
               font=font(MENLO, 15, index=0), fill=(255, 255, 255, 230))
    return img


# =====================================================================
# Theme 05b — Architecture diagram (uses theme 05 gradient palette)
# =====================================================================
def _gradient_tile(img, x, y, w, h, stripe_color, header, sub_lines):
    """Theme-5 tile: semi-transparent white card on gradient + colored
    left stripe. Drawn on a separate RGBA layer + alpha-composited so
    transparency actually blends over the gradient."""
    overlay = Image.new("RGBA", img.size, (0, 0, 0, 0))
    od = ImageDraw.Draw(overlay)
    od.rounded_rectangle((x, y, x + w, y + h), radius=12,
                         fill=(255, 255, 255, 38),
                         outline=(255, 255, 255, 120), width=1)
    od.rectangle((x, y, x + 6, y + h), fill=(*stripe_color, 255))
    img.alpha_composite(overlay)
    d = ImageDraw.Draw(img)
    d.text((x + 22, y + 14), header,
           font=font(MENLO, 18, index=1), fill=(255, 255, 255, 255))
    for i, line in enumerate(sub_lines):
        d.text((x + 22, y + 44 + i * 24), line,
               font=font(MENLO, 15, index=0), fill=(255, 255, 255, 220))


def _arrow_v(d, x, y0, y1, color, width=2, head_at_top=True, head_at_bot=True):
    d.line([(x, y0), (x, y1)], fill=color, width=width)
    if head_at_top:
        d.polygon([(x - 6, y0 + 8), (x + 6, y0 + 8), (x, y0)], fill=color)
    if head_at_bot:
        d.polygon([(x - 6, y1 - 8), (x + 6, y1 - 8), (x, y1)], fill=color)


def social_arch_gradient():
    """Architecture diagram in theme-5 gradient style: SPA → core →
    WorkPlane / OrchestrationPlane adaptors. Captures the four-tier
    surface gemba actually has, ASCII original from the README brief."""
    img = Image.new("RGBA", (W, H_SOCIAL), (0, 0, 0, 0))
    _vertical_gradient(img, (15, 18, 48), (76, 36, 136), (200, 60, 110))
    d = ImageDraw.Draw(img)

    line_color = (255, 255, 255, 220)
    cx = W // 2
    inner_w = W - 144  # 72 gutter each side

    # ── Tier 1: SPA ───────────────────────────────────────────────────
    _gradient_tile(img, 72, 36, inner_w, 80, PRI["sky"],
                   "Gemba SPA  ·  React / TypeScript",
                   ["no role names  ·  no pack vocabulary  ·  capability-driven"])

    # Arrow + label between SPA and core.
    _arrow_v(d, cx, 124, 188, line_color, width=2)
    label = "HTTP  ·  SSE  —  capability-negotiated"
    lf = font(MENLO, 14, index=0)
    lw, _ = text_size(d, label, lf)
    d.text((cx - lw - 24, 148), label, font=lf, fill=(255, 255, 255, 220))

    # ── Tier 2: Core ──────────────────────────────────────────────────
    _gradient_tile(img, 72, 196, inner_w, 102, PRI["emerald"],
                   "Gemba core  ·  Go binary",
                   ["types:  WorkItem · AgentRef · Relationship · Evidence · DoD",
                    "        Sprint · TokenBudget · CostMeter · EscalationRequest"])

    # Branching connector from core down to the two adaptor stacks.
    bus_top = 308
    bus_y = 348
    bus_bot = 408
    left_x = 72 + 270
    right_x = W - 72 - 270
    d.line([(cx, bus_top), (cx, bus_y)], fill=line_color, width=2)
    d.line([(left_x, bus_y), (right_x, bus_y)], fill=line_color, width=2)
    d.line([(left_x, bus_y), (left_x, bus_bot)], fill=line_color, width=2)
    d.line([(right_x, bus_y), (right_x, bus_bot)], fill=line_color, width=2)
    d.polygon([(left_x - 6, bus_bot - 8),
               (left_x + 6, bus_bot - 8),
               (left_x, bus_bot)], fill=line_color)
    d.polygon([(right_x - 6, bus_bot - 8),
               (right_x + 6, bus_bot - 8),
               (right_x, bus_bot)], fill=line_color)

    # Adaptor labels above each branch.
    sf_b = font(MENLO, 13, index=1)
    sf_r = font(MENLO, 12, index=0)
    d.text((left_x - 220, bus_y + 10), "WorkPlaneAdaptor",
           font=sf_b, fill=(*PRI["p0"], 255))
    d.text((left_x - 220, bus_y + 28), "REQUIRED",
           font=font(MENLO, 11, index=1), fill=(255, 255, 255, 220))
    d.text((left_x - 220, bus_y + 44), "transport:  api · jsonl · mcp",
           font=sf_r, fill=(255, 255, 255, 200))

    d.text((right_x + 14, bus_y + 10), "OrchestrationPlaneAdaptor",
           font=sf_b, fill=(*PRI["amber"], 255))
    d.text((right_x + 14, bus_y + 28), "OPTIONAL",
           font=font(MENLO, 11, index=1), fill=(255, 255, 255, 220))
    d.text((right_x + 14, bus_y + 44), "transport:  api · jsonl · mcp",
           font=sf_r, fill=(255, 255, 255, 200))

    # ── Tier 3: adaptor tiles ─────────────────────────────────────────
    tile_y = 416
    tile_h = 168
    gap = 48
    tile_w = (inner_w - gap) // 2
    _gradient_tile(img, 72, tile_y, tile_w, tile_h, PRI["p0"],
                   "WorkPlane adaptors",
                   ["out-of-the-box:   Beads",
                    "forcing function:  Jira",
                    "future:  Linear · GitHub Projects · …"])
    _gradient_tile(img, 72 + tile_w + gap, tile_y, tile_w, tile_h, PRI["amber"],
                   "OrchestrationPlane adaptors",
                   ["out-of-the-box:  Native",
                    "optional:  Gas Town · LangGraph · Gas City",
                    "           OpenHands · CrewAI · …"])

    # Footer.
    d.text((72, H_SOCIAL - 28), "github.com/GembaCore/gemba-core",
           font=font(MENLO, 14, index=0), fill=(255, 255, 255, 200))
    return img


def banner_arch_gradient():
    """Compressed three-tier architecture diagram for README banners."""
    img = Image.new("RGBA", (W, H_BANNER), (0, 0, 0, 0))
    _vertical_gradient(img, (15, 18, 48), (90, 42, 150), (200, 60, 110))
    d = ImageDraw.Draw(img)

    line_color = (255, 255, 255, 220)
    cx = W // 2
    inner_w = W - 96  # 48 gutter each side

    # Tier 1: SPA — single-line tile.
    _gradient_tile(img, 48, 16, inner_w, 50, PRI["sky"],
                   "Gemba SPA  ·  React / TypeScript", [])
    # Squeeze the sub-line into the same tile space (drawn over the
    # alpha-composited card, since _gradient_tile only emits a header
    # for an empty sub list).
    d.text((48 + 22, 16 + 30), "capability-driven · no role names",
           font=font(MENLO, 12, index=0), fill=(255, 255, 255, 215))

    # Arrow + label.
    _arrow_v(d, cx, 76, 100, line_color, width=2)
    d.text((cx + 14, 78), "HTTP · SSE",
           font=font(MENLO, 11, index=0), fill=(255, 255, 255, 200))

    # Tier 2: Core — single line.
    _gradient_tile(img, 48, 108, inner_w, 50, PRI["emerald"],
                   "Gemba core  ·  Go binary", [])
    d.text((48 + 22, 108 + 30),
           "types: WorkItem · AgentRef · Sprint · CostMeter · EscalationRequest",
           font=font(MENLO, 11, index=0), fill=(255, 255, 255, 215))

    # Branching down.
    left_x = 48 + 200
    right_x = W - 48 - 200
    bus_y_top = 168
    bus_y = 184
    bus_bot = 208
    d.line([(cx, bus_y_top), (cx, bus_y)], fill=line_color, width=2)
    d.line([(left_x, bus_y), (right_x, bus_y)], fill=line_color, width=2)
    d.line([(left_x, bus_y), (left_x, bus_bot)], fill=line_color, width=2)
    d.line([(right_x, bus_y), (right_x, bus_bot)], fill=line_color, width=2)
    d.polygon([(left_x - 5, bus_bot - 6),
               (left_x + 5, bus_bot - 6),
               (left_x, bus_bot)], fill=line_color)
    d.polygon([(right_x - 5, bus_bot - 6),
               (right_x + 5, bus_bot - 6),
               (right_x, bus_bot)], fill=line_color)

    # Tier 3: two adaptor tiles side by side.
    tile_y = 216
    tile_h = 90
    gap = 24
    tile_w = (inner_w - gap) // 2
    _gradient_tile(img, 48, tile_y, tile_w, tile_h, PRI["p0"],
                   "WorkPlane  ·  REQUIRED",
                   ["Beads · Jira",
                    "(Linear · GH Projects)"])
    _gradient_tile(img, 48 + tile_w + gap, tile_y, tile_w, tile_h, PRI["amber"],
                   "OrchestrationPlane  ·  OPTIONAL",
                   ["Native · Gas Town",
                    "LangGraph · OpenHands · CrewAI"])
    return img
def _draw_blueprint_grid(d, w, h, color):
    # Major lines every 80, minor every 20.
    for x in range(0, w, 20):
        c = color if x % 80 != 0 else (color[0] + 10, color[1] + 10, color[2] + 30, color[3])
        d.line([(x, 0), (x, h)], fill=color, width=1)
    for y in range(0, h, 20):
        d.line([(0, y), (w, y)], fill=color, width=1)


def _blueprint_box(d, x, y, w, h, label, sub, accent):
    border = (180, 230, 240, 255)
    d.rectangle([(x, y), (x + w, y + h)], outline=border, width=2)
    d.text((x + 14, y + 12), label,
           font=font(MENLO, 18, index=1), fill=(220, 240, 250, 255))
    if sub:
        d.text((x + 14, y + 38), sub,
               font=font(MENLO, 14, index=0), fill=(*accent, 255))


def social_06_blueprint():
    img = Image.new("RGBA", (W, H_SOCIAL), (10, 18, 38, 255))
    d = ImageDraw.Draw(img)
    _draw_blueprint_grid(d, W, H_SOCIAL, (24, 40, 70, 255))
    d.text((72, 60), "gemba", font=font(HELV_NEUE, 132, index=1),
           fill=(220, 240, 250, 255))
    d.text((78, 210), "any work tracker  ×  any orchestrator",
           font=font(MENLO, 24, index=0), fill=(*PRI["cyan"], 255))

    # Schematic: three columns — WorkPlane | core | OrchestrationPlane
    box_y = 300
    bw = 280
    bh = 90
    # WorkPlane stack.
    _blueprint_box(d, 80, box_y, bw, bh, "WorkPlane", "bd · jira · linear", PRI["cyan"])
    _blueprint_box(d, 80, box_y + bh + 20, bw, bh, "CapabilityManifest",
                   "transport · state_map · flags", PRI["amber"])
    # Core box.
    _blueprint_box(d, 80 + bw + 60, box_y, bw, bh + (bh + 20),
                   "core", "WorkItem · Persona · Walk", PRI["emerald"])
    # OrchestrationPlane stack.
    _blueprint_box(d, 80 + 2 * (bw + 60), box_y, bw, bh,
                   "OrchestrationPlane", "gas town · langgraph · native", PRI["cyan"])
    _blueprint_box(d, 80 + 2 * (bw + 60), box_y + bh + 20, bw, bh,
                   "Personas", "PM · Docs · Deploy · QA", PRI["fuchsia"])

    # Connector dashed lines.
    cx0 = 80 + bw
    cx1 = 80 + bw + 60
    cx2 = 80 + bw + 60 + bw
    cx3 = 80 + 2 * (bw + 60)
    cy = box_y + (bh + 10) // 2 + 15
    for sy_off in (0, bh + 20):
        for x_pair in [(cx0, cx1), (cx2, cx3)]:
            yy = box_y + sy_off + bh // 2
            for k in range(x_pair[0] + 4, x_pair[1] - 4, 12):
                d.line([(k, yy), (k + 6, yy)], fill=(*PRI["cyan"], 200), width=2)

    foot_f = font(MENLO, 16, index=0)
    d.text((72, H_SOCIAL - 56),
           "GembaCore/gemba-core   ·   adaptor-agnostic single binary",
           font=foot_f, fill=(160, 200, 220, 255))
    return img


def banner_06_blueprint():
    img = Image.new("RGBA", (W, H_BANNER), (10, 18, 38, 255))
    d = ImageDraw.Draw(img)
    _draw_blueprint_grid(d, W, H_BANNER, (24, 40, 70, 255))
    d.text((48, 48), "gemba", font=font(HELV_NEUE, 76, index=1),
           fill=(220, 240, 250, 255))
    d.text((52, 148), "any work tracker  ×  any orchestrator",
           font=font(MENLO, 16, index=0), fill=(*PRI["cyan"], 255))
    d.text((52, 178), "single binary · capability-aware UI",
           font=font(MENLO, 14, index=0), fill=(160, 200, 220, 255))

    # Compact 3-box schematic right side.
    bw, bh = 200, 60
    by = 80
    boxes = [
        ("WorkPlane", "bd · jira", 600),
        ("core", "WorkItem", 600 + bw + 30),
        ("Orchestrator", "gas town · native", 600 + 2 * (bw + 30)),
    ]
    for label, sub, x in boxes:
        d.rectangle([(x, by), (x + bw, by + bh)],
                    outline=(180, 230, 240, 255), width=2)
        d.text((x + 12, by + 10), label,
               font=font(MENLO, 14, index=1), fill=(220, 240, 250, 255))
        d.text((x + 12, by + 30), sub,
               font=font(MENLO, 12, index=0), fill=(*PRI["cyan"], 255))
    # Dashed connectors.
    yy = by + bh // 2
    for x_start, x_end in [(boxes[0][2] + bw, boxes[1][2]), (boxes[1][2] + bw, boxes[2][2])]:
        for k in range(x_start + 2, x_end - 2, 10):
            d.line([(k, yy), (k + 5, yy)], fill=(*PRI["cyan"], 200), width=2)
    return img


# =====================================================================
# Theme 07 — Adaptor matrix grid
# =====================================================================
def social_07_adaptor_matrix():
    img = Image.new("RGBA", (W, H_SOCIAL), (250, 250, 249, 255))
    d = ImageDraw.Draw(img)
    d.text((72, 80), "gemba",
           font=font(HELV_NEUE, 132, index=1), fill=PRI["ink"] + (255,))
    d.text((78, 222), "pluggable adaptors. capability-aware UI.",
           font=font(HELV_NEUE, 24, index=0), fill=(80, 84, 96, 255))

    # Matrix: rows = WorkPlane, cols = Orchestrator. Filled cells use a
    # stroke-drawn checkmark (no glyph reliance, so any font works).
    rows = ["bd", "jira", "linear"]
    cols = ["gas town", "langgraph", "native", "noop"]
    matrix = [
        [True, True, True, True],
        [False, True, True, True],
        [False, False, True, True],
    ]
    cell = 92
    gap = 14
    grid_x = 100
    grid_y = 290
    label_w = 90

    for j, c in enumerate(cols):
        cx = grid_x + label_w + j * (cell + gap) + cell // 2
        cw, _ = text_size(d, c, font(MENLO, 14, index=1))
        d.text((cx - cw // 2, grid_y - 28), c,
               font=font(MENLO, 14, index=1), fill=(80, 84, 96, 255))

    for i, r in enumerate(rows):
        ry = grid_y + i * (cell + gap)
        d.text((grid_x, ry + cell // 2 - 8), r,
               font=font(MENLO, 14, index=1), fill=(80, 84, 96, 255))
        for j in range(len(cols)):
            x = grid_x + label_w + j * (cell + gap)
            y = ry
            if matrix[i][j]:
                color = [PRI["p0"], PRI["p1"], PRI["emerald"], PRI["sky"]][j]
                d.rounded_rectangle((x, y, x + cell, y + cell),
                                    radius=10, fill=(*color, 235))
                draw_check(d, x + cell // 2, y + cell // 2,
                           size=44, color=(255, 255, 255), width=5)
            else:
                d.rounded_rectangle((x, y, x + cell, y + cell),
                                    radius=10, fill=(245, 245, 244, 255),
                                    outline=(220, 220, 218, 255), width=1)
                d.line([(x + cell // 2 - 14, y + cell // 2),
                        (x + cell // 2 + 14, y + cell // 2)],
                       fill=(170, 172, 180, 255), width=3)

    foot = "github.com/GembaCore/gemba-core"
    foot_f = font(MENLO, 18, index=0)
    fw, _ = text_size(d, foot, foot_f)
    d.text((W - fw - 72, H_SOCIAL - 60), foot, font=foot_f, fill=(140, 144, 156, 255))
    return img


def banner_07_adaptor_matrix():
    img = Image.new("RGBA", (W, H_BANNER), (250, 250, 249, 255))
    d = ImageDraw.Draw(img)
    d.text((48, 48), "gemba",
           font=font(HELV_NEUE, 76, index=1), fill=PRI["ink"] + (255,))
    d.text((52, 148), "pluggable adaptors.",
           font=font(HELV_NEUE, 18, index=0), fill=(80, 84, 96, 255))
    d.text((52, 178), "capability-aware UI.",
           font=font(HELV_NEUE, 18, index=0), fill=(80, 84, 96, 255))

    # Compact 4×3 grid on the right.
    cols = ["gt", "lg", "native"]
    rows = ["bd", "jira", "linear", "noop"]
    cell = 50
    gap = 6
    gx = 700
    gy = 60
    label_w = 60
    for j, c in enumerate(cols):
        cx = gx + label_w + j * (cell + gap) + cell // 2 - len(c) * 3
        d.text((cx, gy - 22), c, font=font(MENLO, 12, index=1),
               fill=(80, 84, 96, 255))
    matrix = [
        [True, True, True],
        [False, True, True],
        [False, False, True],
        [True, True, True],
    ]
    palette = [PRI["p0"], PRI["p1"], PRI["emerald"]]
    for i, r in enumerate(rows):
        ry = gy + i * (cell + gap)
        d.text((gx, ry + 16), r, font=font(MENLO, 12, index=1),
               fill=(80, 84, 96, 255))
        for j in range(len(cols)):
            x = gx + label_w + j * (cell + gap)
            y = ry
            if matrix[i][j]:
                d.rounded_rectangle((x, y, x + cell, y + cell),
                                    radius=8, fill=(*palette[j], 235))
                draw_check(d, x + cell // 2, y + cell // 2,
                           size=22, color=(255, 255, 255), width=3)
            else:
                d.rounded_rectangle((x, y, x + cell, y + cell),
                                    radius=8, fill=(245, 245, 244, 255),
                                    outline=(220, 220, 218, 255), width=1)
                d.line([(x + cell // 2 - 8, y + cell // 2),
                        (x + cell // 2 + 8, y + cell // 2)],
                       fill=(170, 172, 180, 255), width=2)
    return img


# =====================================================================
# Theme 08 — Capability flags
# =====================================================================
def _capability_chip(d, x, y, on, label):
    if on:
        d.rounded_rectangle((x, y, x + 320, y + 40), radius=8,
                            fill=(20, 60, 50, 255),
                            outline=(*PRI["emerald"], 200), width=1)
        draw_check(d, x + 22, y + 20, size=22, color=PRI["emerald"], width=3)
        d.text((x + 44, y + 12), label,
               font=font(MENLO, 16, index=0),
               fill=(220, 240, 232, 255))
    else:
        d.rounded_rectangle((x, y, x + 320, y + 40), radius=8,
                            fill=(36, 38, 46, 255),
                            outline=(70, 74, 84, 255), width=1)
        # Inactive marker — short horizontal stroke instead of a dot
        # glyph that font-rendering may flake on.
        d.line([(x + 16, y + 20), (x + 28, y + 20)],
               fill=(120, 124, 138, 255), width=2)
        d.text((x + 44, y + 12), label,
               font=font(MENLO, 16, index=0),
               fill=(140, 145, 158, 255))


def social_08_capability_flags():
    img = Image.new("RGBA", (W, H_SOCIAL), (12, 14, 20, 255))
    d = ImageDraw.Draw(img)
    d.text((72, 70), "gemba",
           font=font(HELV, 132, index=1), fill=(250, 250, 249, 255))
    d.text((78, 212), "the UI gates on what the adaptor declares.",
           font=font(MENLO, 22, index=0), fill=(180, 186, 200, 255))

    # Code-style header.
    d.text((72, 270), "CapabilityManifest:",
           font=font(MENLO, 18, index=1), fill=(*PRI["cyan"], 255))
    flags = [
        (True,  "transport: api"),
        (True,  "state_map: 6 categories"),
        (True,  "sprint_native"),
        (False, "evidence_synthesis_required"),
        (True,  "dependency_graph_native"),
        (True,  "ready_set_query"),
        (False, "token_budget_enforced"),
        (True,  "schema_enforcement: strict"),
    ]
    col_w = 350
    for i, (on, label) in enumerate(flags):
        col = i // 4
        row = i % 4
        x = 72 + col * (col_w + 30)
        y = 310 + row * 52
        _capability_chip(d, x, y, on, label)

    d.text((72, H_SOCIAL - 60),
           "GembaCore/gemba-core   ·   capability-aware UI",
           font=font(MENLO, 18, index=0), fill=(120, 124, 138, 255))
    return img


def banner_08_capability_flags():
    img = Image.new("RGBA", (W, H_BANNER), (12, 14, 20, 255))
    d = ImageDraw.Draw(img)
    d.text((48, 40), "gemba",
           font=font(HELV, 76, index=1), fill=(250, 250, 249, 255))
    d.text((52, 138), "the UI gates on what the adaptor declares.",
           font=font(MENLO, 16, index=0), fill=(180, 186, 200, 255))
    d.text((52, 168), "GembaCore/gemba-core",
           font=font(MENLO, 14, index=0), fill=(120, 124, 138, 255))
    flags = [
        (True,  "sprint_native"),
        (True,  "dependency_graph_native"),
        (False, "token_budget_enforced"),
        (True,  "ready_set_query"),
    ]
    for i, (on, label) in enumerate(flags):
        x = 700
        y = 50 + i * 50
        _capability_chip(d, x, y, on, label)
    return img


# =====================================================================
# Theme 09 — Persona constellation
# =====================================================================
import math


def _draw_persona_dot(d, img, cx, cy, r, color, label, label_offset_y=24):
    halo = Image.new("RGBA", img.size, (0, 0, 0, 0))
    ImageDraw.Draw(halo).ellipse(
        [(cx - r - 14, cy - r - 14), (cx + r + 14, cy + r + 14)],
        fill=(*color, 80))
    img.alpha_composite(halo)
    d = ImageDraw.Draw(img)
    d.ellipse([(cx - r, cy - r), (cx + r, cy + r)], fill=(*color, 255))
    if label:
        f = font(MENLO, 14, index=1)
        tw, _ = text_size(d, label, f)
        d.text((cx - tw // 2, cy + r + label_offset_y - 18), label,
               font=f, fill=(220, 224, 232, 255))


def _draw_persona_constellation(d, img, *, cx, cy, hub_r, orbit_r, personas, line_color):
    # Hub.
    d.ellipse([(cx - hub_r, cy - hub_r), (cx + hub_r, cy + hub_r)],
              fill=(40, 44, 56, 255), outline=(220, 224, 232, 255), width=2)
    f = font(MENLO, 18, index=1)
    tw, th = text_size(d, "operator", f)
    d.text((cx - tw // 2, cy - th // 2), "operator", font=f,
           fill=(220, 224, 232, 255))

    # Orbit ring.
    d.ellipse([(cx - orbit_r, cy - orbit_r), (cx + orbit_r, cy + orbit_r)],
              outline=(60, 64, 72, 255), width=1)

    n = len(personas)
    for i, (label, color) in enumerate(personas):
        ang = -math.pi / 2 + (i / n) * 2 * math.pi
        px = cx + int(orbit_r * math.cos(ang))
        py = cy + int(orbit_r * math.sin(ang))
        # Connector.
        d.line([(cx, cy), (px, py)], fill=line_color, width=1)
        _draw_persona_dot(d, img, px, py, 22, color, label)


def social_09_persona_constellation():
    img = Image.new("RGBA", (W, H_SOCIAL), (12, 14, 20, 255))
    d = ImageDraw.Draw(img)
    d.text((72, 80), "gemba",
           font=font(HELV, 132, index=1), fill=(250, 250, 249, 255))
    d.text((78, 222), "typed personas. coded skills. operator orbit.",
           font=font(MENLO, 22, index=0), fill=(180, 186, 200, 255))
    d.text((72, H_SOCIAL - 60),
           "GembaCore/gemba-core   ·   Coach / Manager varieties · capability-gated",
           font=font(MENLO, 18, index=0), fill=(120, 124, 138, 255))

    personas = [
        ("PM", PRI["p0"]),
        ("docs", PRI["p1"]),
        ("deploy", PRI["amber"]),
        ("qa", PRI["emerald"]),
        ("arch", PRI["cyan"]),
        ("review", PRI["violet"]),
    ]
    _draw_persona_constellation(d, img,
                                cx=950, cy=H_SOCIAL // 2,
                                hub_r=58, orbit_r=180,
                                personas=personas,
                                line_color=(60, 64, 72, 255))
    return img


def banner_09_persona_constellation():
    img = Image.new("RGBA", (W, H_BANNER), (12, 14, 20, 255))
    d = ImageDraw.Draw(img)
    d.text((48, 60), "gemba",
           font=font(HELV, 80, index=1), fill=(250, 250, 249, 255))
    d.text((52, 162), "typed personas. coded skills. operator orbit.",
           font=font(MENLO, 16, index=0), fill=(180, 186, 200, 255))
    d.text((52, 196), "GembaCore/gemba-core",
           font=font(MENLO, 14, index=0), fill=(120, 124, 138, 255))

    personas = [
        ("PM", PRI["p0"]),
        ("docs", PRI["p1"]),
        ("deploy", PRI["amber"]),
        ("qa", PRI["emerald"]),
        ("arch", PRI["cyan"]),
        ("review", PRI["violet"]),
    ]
    _draw_persona_constellation(d, img,
                                cx=1010, cy=H_BANNER // 2,
                                hub_r=38, orbit_r=110,
                                personas=personas,
                                line_color=(60, 64, 72, 255))
    return img


# =====================================================================
# Theme 10 — Three-pane app
# =====================================================================
def _draw_app_pane(d, x, y, w, h, header, items, header_color, pane_bg=(248, 248, 247, 255)):
    d.rounded_rectangle((x, y, x + w, y + h), radius=10,
                        fill=pane_bg,
                        outline=(225, 225, 222, 255), width=1)
    # Header strip.
    d.rectangle((x, y, x + w, y + 28), fill=(238, 238, 235, 255))
    d.text((x + 12, y + 8), header,
           font=font(MENLO, 12, index=1), fill=(*header_color, 255))
    # Items.
    iy = y + 40
    for line, accent in items:
        d.rounded_rectangle((x + 8, iy, x + w - 8, iy + 30),
                            radius=6, fill=(255, 255, 255, 255),
                            outline=(232, 232, 230, 255), width=1)
        if accent:
            d.rectangle((x + 8, iy, x + 12, iy + 30), fill=(*accent, 255))
        d.text((x + 22, iy + 8),
               line, font=font(MENLO, 11, index=0),
               fill=(64, 68, 80, 255))
        iy += 36


def social_10_three_pane():
    img = Image.new("RGBA", (W, H_SOCIAL), (250, 250, 249, 255))
    d = ImageDraw.Draw(img)
    d.text((72, 70), "gemba",
           font=font(HELV_NEUE, 124, index=1), fill=PRI["ink"] + (255,))
    d.text((78, 200), "operator queue · dispatch · live sessions.",
           font=font(HELV_NEUE, 24, index=0), fill=(80, 84, 96, 255))

    # Three side-by-side panes.
    py = 280
    ph = 280
    gap = 16
    pw = (W - 144 - 2 * gap) // 3
    _draw_app_pane(
        d, 72, py, pw, ph,
        "BACKLOG",
        [
            ("gm-e4.2  P0  openapi spec",   PRI["p0"]),
            ("gm-pd2   P1  epic anatomy",   PRI["p1"]),
            ("gm-lq1   P2  output policy",  PRI["p2"]),
            ("gm-77u   P2  walk summary",   PRI["p2"]),
            ("gm-xst   P1  qa linters",     PRI["p1"]),
        ],
        header_color=(120, 60, 200),
    )
    _draw_app_pane(
        d, 72 + pw + gap, py, pw, ph,
        "DISPATCH",
        [
            ("gm-e4.2  →  deployment-engineer", PRI["emerald"]),
            ("gm-pd2   →  ux-pm",               PRI["sky"]),
            ("gm-77u   →  documentarian",       PRI["amber"]),
        ],
        header_color=(*PRI["sky"], ),
    )
    _draw_app_pane(
        d, 72 + 2 * (pw + gap), py, pw, ph,
        "SESSIONS",
        [
            ("a91415c1  ·  in_progress",        PRI["amber"]),
            ("a21fed66  ·  awaiting handoff",   PRI["sky"]),
            ("aded672c  ·  ✓ complete",         PRI["emerald"]),
            ("aadb6dfc  ·  ✓ complete",         PRI["emerald"]),
        ],
        header_color=(*PRI["emerald"], ),
    )

    foot_f = font(MENLO, 18, index=0)
    foot = "github.com/GembaCore/gemba-core"
    fw, _ = text_size(d, foot, foot_f)
    d.text((W - fw - 72, H_SOCIAL - 56), foot, font=foot_f,
           fill=(140, 144, 156, 255))
    return img


def banner_10_three_pane():
    img = Image.new("RGBA", (W, H_BANNER), (250, 250, 249, 255))
    d = ImageDraw.Draw(img)
    d.text((48, 50), "gemba",
           font=font(HELV_NEUE, 76, index=1), fill=PRI["ink"] + (255,))
    d.text((52, 152), "queue · dispatch · sessions.",
           font=font(HELV_NEUE, 18, index=0), fill=(80, 84, 96, 255))
    d.text((52, 182), "GembaCore/gemba-core",
           font=font(MENLO, 14, index=0), fill=(140, 144, 156, 255))

    # Compact three panes on the right.
    py = 36
    ph = H_BANNER - 72
    gap = 10
    pw = 170
    px0 = W - (3 * pw + 2 * gap) - 32
    _draw_app_pane(d, px0, py, pw, ph, "BACKLOG",
                   [("gm-e4.2 P0", PRI["p0"]),
                    ("gm-pd2  P1", PRI["p1"]),
                    ("gm-lq1  P2", PRI["p2"])],
                   header_color=(120, 60, 200))
    _draw_app_pane(d, px0 + pw + gap, py, pw, ph, "DISPATCH",
                   [("gm-e4.2 → deploy", PRI["emerald"]),
                    ("gm-pd2  → ux-pm",  PRI["sky"]),
                    ("gm-77u  → docs",   PRI["amber"])],
                   header_color=(*PRI["sky"], ))
    _draw_app_pane(d, px0 + 2 * (pw + gap), py, pw, ph, "SESSIONS",
                   [("a91415  in_prog", PRI["amber"]),
                    ("a21fed  ✓",       PRI["emerald"]),
                    ("aded67  ✓",       PRI["emerald"])],
                   header_color=(*PRI["emerald"], ))
    return img


# =====================================================================
# Orchestration
# =====================================================================
THEMES = [
    ("01-dark-kanban",          social_01_dark_kanban,          banner_01_dark_kanban),
    ("02-light-serif",          social_02_light_serif,          banner_02_light_serif),
    ("03-brutalist-mono",       social_03_brutalist_mono,       banner_03_brutalist_mono),
    ("04-terminal",             social_04_terminal,             banner_04_terminal),
    ("05-gradient-dispatch",    social_05_gradient_dispatch,    banner_05_gradient_dispatch),
    ("06-blueprint",            social_06_blueprint,            banner_06_blueprint),
    ("07-adaptor-matrix",       social_07_adaptor_matrix,       banner_07_adaptor_matrix),
    ("08-capability-flags",     social_08_capability_flags,     banner_08_capability_flags),
    ("09-persona-constellation", social_09_persona_constellation, banner_09_persona_constellation),
    ("10-three-pane",           social_10_three_pane,           banner_10_three_pane),
    ("arch-gradient",           social_arch_gradient,           banner_arch_gradient),
]


def main():
    for name, social_fn, banner_fn in THEMES:
        save(social_fn(), os.path.join(OUT_SOCIAL, f"social-{name}.png"))
        save(banner_fn(), os.path.join(OUT_BANNER, f"banner-{name}.png"))


if __name__ == "__main__":
    main()
