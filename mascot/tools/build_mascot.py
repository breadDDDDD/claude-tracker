"""
Tally — the Claude tracker mascot. Source of truth for every generated asset.

Run:  python mascot/tools/build_mascot.py
Regenerates everything under mascot/ (sprites.json, manifest.json, ansi/, png/, preview/).

Sprites are 20x20 pixel grids, drawn with one character per pixel (see PALETTE).
In a terminal they render with half-block characters: 20 columns x 10 rows.

Layers, composited in order:
  1. BASE          head + hair + shoulders (outfit colours are symbolic: O o C c)
  2. outfit        recolours O/o/C/c, optional HAT rows replace the top of the head
  3. expression    rows over the face window (x=5..14, y=6..12) + free "extras" pixels
  4. effects       aura / shake computed per frame
"""
import json
import os
import sys

from PIL import Image, ImageDraw, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
W = H = 20
FACE_X = 5  # face window left edge; windows are 10 wide
FACE_W = 10

# ---------------------------------------------------------------- palette
PALETTE = {
    ".": None,          # transparent
    "K": "#2b2130",     # outline / lashes / brows
    "H": "#3f3150",     # hair (dark violet)
    "h": "#62507a",     # hair shine
    "P": "#8f6bb3",     # inner purple streak
    "S": "#fce6da",     # skin
    "s": "#eac7b8",     # skin shade
    "W": "#ffffff",     # eye white / highlight
    "E": "#a8475a",     # iris (rose)
    "e": "#4a2030",     # pupil / dark iris
    "B": "#f3a0a6",     # blush
    "M": "#6e2a38",     # mouth line
    "m": "#ee8f93",     # mouth inside
    "Y": "#7fbdf0",     # sweat / tears
    "y": "#d4ecfb",     # sweat highlight
    "G": "#6a73c9",     # gloom lines
    "Z": "#fff3a8",     # sparkle
    "F": "#ff7a33",     # aura outer
    "f": "#ffc34d",     # aura inner
    # outfit slots, filled per outfit
    "O": None, "o": None, "C": None, "c": None,
    # hat slots
    "A": "#f4f1ec", "a": "#d3cec6", "b": "#b7aecf",
}

# ---------------------------------------------------------------- base body
BASE = [
    "......KKKKKKKK......",  # 0
    "....KKHHHhhHHHKK....",  # 1
    "...KHHHhhHHhHHHHK...",  # 2
    "..KHHhHHHHHHHHhHHK..",  # 3
    "..KHPHHSHHHHSHHPHK..",  # 4  bangs
    "..KHPHSSHSSSHSHPHK..",  # 5  bang tips
    "..KHPSSSSSSSSSSPHK..",  # 6  brows
    "..KHPSSSSSSSSSSPHK..",  # 7  eyes
    "..KHPSSSSSSSSSSPHK..",  # 8
    "..KHPSSSSSSSSSSPHK..",  # 9
    "..KHPSSSSSSSSSSPHK..",  # 10 blush
    "..KHPSSSSSSSSSSPHK..",  # 11 mouth
    "..KHPsSSSSSSSSsPHK..",  # 12
    "..KHHKsSSSSSSsKHHK..",  # 13 chin
    "..KHHHKKssssKKHHHK..",  # 14 neck
    "...KKKKKssssKKKKK...",  # 15
    "..KOOOOCCssCCOOOOK..",  # 16 collar
    ".KOOOOOOCssCOOOOOOK.",  # 17
    ".KoOOOOOOCCOOOOOOoK.",  # 18
    ".KooOOOOOOOOOOOOooK.",  # 19
]

# ---------------------------------------------------------------- outfits (= model)
HAT = [
    "......KKKKKKKK......",
    ".....KAAAAAAAAK.....",
    ".....KbbbbbbbbK.....",
    "KKaAAAAAAAAAAAAAAaKK",
    ".KKKaaaaaaaaaaaaKKK.",
]

OUTFITS = {
    "blazer": {  # grey work suit (refs 28, 441, 442)
        "colors": {"O": "#5b5866", "o": "#45424f", "C": "#e3d6f1", "c": "#bfb0d6"},
        "hat": None,
        "note": "Grey work blazer over a lavender shirt — serious mode.",
    },
    "shirt": {  # her signature lavender blouse (refs 30-33, 35)
        "colors": {"O": "#cdb8e6", "o": "#ad96cc", "C": "#f4eefb", "c": "#ad96cc"},
        "hat": None,
        "note": "Signature lavender blouse — the everyday default.",
    },
    "tee": {  # mint lounge t-shirt (refs 70-72)
        "colors": {"O": "#bde6d5", "o": "#97cdb8", "C": "#bde6d5", "c": "#97cdb8"},
        "hat": None,
        "note": "Mint lounge tee — light and quick.",
    },
    "sunhat": {  # sun hat + sage tee (refs 430, 67)
        "colors": {"O": "#b9c8a1", "o": "#97a880", "C": "#b9c8a1", "c": "#97a880"},
        "hat": HAT,
        "note": "White sun hat and sage tee — out exploring.",
    },
}

MODELS = {
    # substring of the model id -> outfit + accent colour for UI chrome
    "opus":   {"outfit": "blazer", "accent": "#d97757", "label": "Opus"},
    "sonnet": {"outfit": "shirt",  "accent": "#9b7fd1", "label": "Sonnet"},
    "haiku":  {"outfit": "tee",    "accent": "#5fbf9f", "label": "Haiku"},
    "fable":  {"outfit": "sunhat", "accent": "#d9a441", "label": "Fable"},
    "default": {"outfit": "shirt", "accent": "#9b7fd1", "label": "Claude"},
}

# ---------------------------------------------------------------- expressions
# Each frame: "face" = {y: 10-char row over x=5..14, spaces keep the base}
#             "extras" = {(x, y): char} absolute pixels
#             "fx" = optional effect: "aura0", "aura1", "shake"
SWEAT = {(14, 8): "y", (14, 9): "Y"}
SWEAT_LOW = {(14, 9): "y", (14, 10): "Y"}

EYES_OPEN = {7: " KKK  KKK ", 8: " EWe  eWE ", 9: " EeE  EeE "}
EYES_CLOSED = {7: "          ", 8: "          ", 9: " KKK  KKK "}


def frame(face=None, extras=None, fx=None):
    return {"face": face or {}, "extras": extras or {}, "fx": fx}


def with_rows(*dicts):
    out = {}
    for d in dicts:
        out.update(d)
    return out


EXPRESSIONS = {
    "neutral": {
        "ref": "431 — calm, open eyes, flat mouth",
        "frames": [
            frame(with_rows(EYES_OPEN, {11: "    MM    "})),
            frame(with_rows(EYES_CLOSED, {11: "    MM    "})),
        ],
        "timeline": [[0, 3200], [1, 140]],
    },
    "bliss": {
        "ref": "442/443 — 'jyuwawawa~n' blank round eyes, open smile, blush",
        "frames": [
            frame({7: " KKK  KKK ", 8: " KWK  KWK ", 9: " KKK  KKK ",
                   10: "BB      BB", 11: "   MmmM   ", 12: "    MM    "},
                  {(1, 1): "Z", (18, 6): "Z", (0, 11): "Z", (19, 2): "Z"}),
            frame({7: " KKK  KKK ", 8: " KWK  KWK ", 9: " KKK  KKK ",
                   10: "BB      BB", 11: "   MmmM   ", 12: "    MM    "},
                  {(18, 1): "Z", (1, 6): "Z", (19, 11): "Z", (0, 2): "Z"}),
        ],
        "timeline": [[0, 450], [1, 450]],
    },
    "cheerful": {
        "ref": "441 — waving hello, ^^ eyes, open smile",
        "frames": [
            frame({8: "  K    K  ", 9: " K K  K K ", 10: " B      B ",
                   11: "   MmmM   ", 12: "    MM    "},
                  {(0, 3): "Z", (19, 8): "Z"}),
            frame({8: "  K    K  ", 9: " K K  K K ", 10: " B      B ",
                   11: "   MmmM   ", 12: "    MM    "},
                  {(19, 3): "Z", (0, 8): "Z"}),
        ],
        "timeline": [[0, 400], [1, 400]],
    },
    "relaxed": {
        "ref": "439 — eyes closed by the fan, little 'o' mouth",
        "frames": [
            frame({8: " K K  K K ", 9: "  K    K  ", 10: " B      B ",
                   11: "    MM    ", 12: "    MM    "}),
        ],
        "timeline": [[0, 0]],
    },
    "focused": {
        "ref": "438 — heads-down writing, eyes glancing at the work",
        "frames": [
            frame({6: "  K    K  ", 7: " KKK  KKK ", 8: " eEE  eEE ",
                   11: "    MM    "}),
            frame({6: "  K    K  ", 7: "          ", 8: " KKK  KKK ",
                   11: "    MM    "}),
        ],
        "timeline": [[0, 2600], [1, 120]],
    },
    "determined": {
        "ref": "67 — burning aura, stern frown",
        "frames": [
            frame({6: " KK    KK ", 7: "   K  K   ", 8: " KKK  KKK ", 9: " EWe  eWE ",
                   11: "    MM    ", 12: "   M  M   "}, fx="aura0"),
            frame({6: " KK    KK ", 7: "   K  K   ", 8: " KKK  KKK ", 9: " EWe  eWE ",
                   11: "    MM    ", 12: "   M  M   "}, fx="aura1"),
        ],
        "timeline": [[0, 220], [1, 220]],
    },
    "annoyed": {
        "ref": "30 — half-lidded, gloom lines, grumbling",
        "frames": [
            frame({7: " KKK  KKK ", 8: " eEe  eEe ", 9: " G G  G G ", 10: " G G  G G ",
                   11: "   MmmM   ", 12: "    MM    "},
                  {(5, 9): "y", (5, 10): "Y"}),
        ],
        "timeline": [[0, 0]],
    },
    "glare": {
        "ref": "34 — narrowed stare, furrowed brows, blush",
        "frames": [
            frame({6: " KK    KK ", 7: "   K  K   ", 8: " KKK  KKK ", 9: " EWE  EWE ",
                   10: "BBB    BBB", 11: "   MMMM   "}),
            frame({6: " KK    KK ", 7: "   K  K   ", 8: "          ", 9: " KKK  KKK ",
                   10: "BBB    BBB", 11: "   MMMM   "}),
        ],
        "timeline": [[0, 2800], [1, 120]],
    },
    "sleepy": {
        "ref": "35 — deadpan half-moon eyes, blush, dozing",
        "frames": [
            frame({8: " KKK  KKK ", 9: " eee  eee ", 10: "BB      BB", 11: "    MM    "},
                  {(17, 2): "Z", (19, 0): "Z"}),
            frame({8: "          ", 9: " KKK  KKK ", 10: "BB      BB", 11: "    MM    "},
                  {(18, 1): "Z"}),
        ],
        "timeline": [[0, 1400], [1, 1400]],
    },
    "shocked": {
        "ref": "28/440 — blank white eyes, open 'O' mouth",
        "frames": [
            frame({7: " KKK  KKK ", 8: " KWK  KWK ", 9: " KKK  KKK ",
                   11: "   MmmM   ", 12: "   MmmM   "}),
        ],
        "timeline": [[0, 0]],
    },
    "nervous": {
        "ref": "429/440 — shrunken pupils, wobbly mouth, sweat drop",
        "frames": [
            frame({7: " KKK  KKK ", 8: " SSS  SSS ", 9: "  e    e  ",
                   11: "  M MM M  ", 12: "   M  M   "}, SWEAT),
            frame({7: " KKK  KKK ", 8: " SSS  SSS ", 9: "  e    e  ",
                   11: "  M MM M  ", 12: "   M  M   "}, SWEAT_LOW),
        ],
        "timeline": [[0, 600], [1, 600]],
    },
    "despair": {
        "ref": "431 (volcano) — sad brows, streaming tears",
        "frames": [
            frame({6: "   K  K   ", 7: " KK    KK ", 8: " KKK  KKK ", 9: " WeW  WeW ",
                   10: " Y Y  Y Y ", 11: " Y Y  Y Y ", 12: "    MM    "}),
            frame({6: "   K  K   ", 7: " KK    KK ", 8: " KKK  KKK ", 9: " WeW  WeW ",
                   10: "  Y    Y  ", 11: " Y Y  Y Y ", 12: "    MM    "}),
        ],
        "timeline": [[0, 500], [1, 500]],
    },
    "panic": {
        "ref": "70/71/72 — >< eyes, yelling, flailing",
        "frames": [
            frame({7: " K      K ", 8: "  KK  KK  ", 9: " K      K ", 10: "BBB    BBB",
                   11: "  MmmmmM  ", 12: "   MmmM   "}, SWEAT),
            frame({7: " K      K ", 8: "  KK  KK  ", 9: " K      K ", 10: "BBB    BBB",
                   11: "  MmmmmM  ", 12: "   MmmM   "}, SWEAT_LOW, fx="shake"),
        ],
        "timeline": [[0, 120], [1, 120]],
    },
    "flustered": {
        "ref": "32 — teary, heavy blush, small open mouth",
        "frames": [
            frame({6: "   K  K   ", 7: " KKK  KKK ", 8: " EWE  EWE ", 9: " EYE  EYE ",
                   10: "BBBB  BBBB", 11: "    mm    "}, SWEAT),
        ],
        "timeline": [[0, 0]],
    },
    "sassy": {
        "ref": "31 — one eye narrowed, telling you off",
        "frames": [
            frame({6: "      KK  ", 7: " KKK      ", 8: " EWe  KKK ", 9: " EEE  eEE ",
                   10: " B      B ", 11: "   MmmM   ", 12: "    MM    "}),
            frame({6: "      KK  ", 7: " KKK      ", 8: " EWe  KKK ", 9: " EEE  eEE ",
                   10: " B      B ", 11: "    MM    "}),
        ],
        "timeline": [[0, 260], [1, 260]],
    },
}

KAOMOJI = {
    # unicode: nicer, single-width glyphs only; ascii: safe everywhere
    "neutral":    ["(•_•)",   "(o_o)"],
    "bliss":      ["(○▽○)",   "(OvO)"],
    "cheerful":   ["(^▽^)/",  "(^o^)/"],
    "relaxed":    ["(ᴗoᴗ)",   "(-o-)"],
    "focused":    ["(•_•)✎",  "(._.)/"],
    "determined": ["(ò_ó)",   "(`_')"],
    "annoyed":    ["(¬_¬)",   "(-_-)"],
    "glare":      ["(ಠ_ಠ)",   "(>_>)"],
    "sleepy":     ["(-_-)zZ", "(-_-)zZ"],
    "shocked":    ["(O□O)",   "(O_O)!"],
    "nervous":    ["(•_•;)",  "(o_o;)"],
    "despair":    ["(T_T)",   "(T_T)"],
    "panic":      ["(>□<)!!", "(>_<)!!"],
    "flustered":  ["(//•_•//)", "(//o_o//)"],
    "sassy":      ["(•o-)",   "(o_-)"],
}

# Rules for the app. Evaluate top-down; first match wins.
STATE_RULES = [
    {"expression": "sassy",      "when": "last request errored / rate-limited response / auth problem",
     "hold_ms": 4000},
    {"expression": "panic",      "when": "usage_pct >= 100 (limit hit)"},
    {"expression": "despair",    "when": "usage_pct >= 95"},
    {"expression": "shocked",    "when": "burn spike: usage_pct rose >= 10 points within 2 minutes",
     "hold_ms": 3000},
    {"expression": "nervous",    "when": "usage_pct >= 85"},
    {"expression": "flustered",  "when": "weekly_pct >= 80 (weekly cap closing in, session still OK)"},
    {"expression": "cheerful",   "when": "app just started, or window just reset", "hold_ms": 4000},
    {"expression": "sleepy",     "when": "no activity for >= 10 minutes"},
    {"expression": "glare",      "when": "usage_pct >= 75"},
    {"expression": "annoyed",    "when": "usage_pct >= 60"},
    {"expression": "determined", "when": "actively generating AND model is opus"},
    {"expression": "focused",    "when": "actively generating / running tools"},
    {"expression": "bliss",      "when": "usage_pct < 10"},
    {"expression": "relaxed",    "when": "idle 1-10 minutes AND usage_pct < 40"},
    {"expression": "neutral",    "when": "otherwise"},
]


# ---------------------------------------------------------------- compositing
def compose(outfit_name, expr_name, frame_idx):
    grid = [list(r) for r in BASE]
    outfit = OUTFITS[outfit_name]
    if outfit["hat"]:
        for y, row in enumerate(outfit["hat"]):
            grid[y] = list(row)
    fr = EXPRESSIONS[expr_name]["frames"][frame_idx]
    for y, row in fr["face"].items():
        assert len(row) == FACE_W, (expr_name, y, row)
        for i, ch in enumerate(row):
            if ch != " ":
                grid[y][FACE_X + i] = ch
    for (x, y), ch in fr["extras"].items():
        grid[y][x] = ch
    fx = fr["fx"]
    if fx in ("aura0", "aura1"):
        phase = 0 if fx == "aura0" else 1
        src = [r[:] for r in grid]
        for y in range(16):
            for x in range(W):
                if src[y][x] != ".":
                    continue
                near = any(0 <= x + dx < W and 0 <= y + dy < H and src[y + dy][x + dx] not in ".Z"
                           for dx, dy in ((1, 0), (-1, 0), (0, 1), (0, -1)))
                if near:
                    grid[y][x] = "F" if (x + y + phase) % 2 else "f"
    if fx == "shake":
        grid = [["."] + r[:-1] for r in grid]
    return ["".join(r) for r in grid]


def color_of(ch, outfit_name):
    if ch in OUTFITS[outfit_name]["colors"]:
        return OUTFITS[outfit_name]["colors"][ch]
    return PALETTE[ch]


def hex_rgb(h):
    return tuple(int(h[i:i + 2], 16) for i in (1, 3, 5))


def xterm256(rgb):
    def q(v):
        return 0 if v < 48 else 1 if v < 115 else (v - 35) // 40
    r, g, b = (q(v) for v in rgb)
    cube = 16 + 36 * r + 6 * g + b
    levels = [0, 95, 135, 175, 215, 255]
    cr = (levels[r], levels[g], levels[b])
    avg = sum(rgb) // 3
    gi = max(0, min(23, (avg - 8) // 10))
    gray = 232 + gi
    gv = 8 + gi * 10
    d_cube = sum((a - b_) ** 2 for a, b_ in zip(rgb, cr))
    d_gray = sum((a - gv) ** 2 for a in rgb)
    return cube if d_cube <= d_gray else gray


def to_ansi(rows, outfit_name, mode):
    def fg(c):
        r = hex_rgb(c)
        return f"\x1b[38;2;{r[0]};{r[1]};{r[2]}m" if mode == "truecolor" else f"\x1b[38;5;{xterm256(r)}m"

    def bg(c):
        r = hex_rgb(c)
        return f"\x1b[48;2;{r[0]};{r[1]};{r[2]}m" if mode == "truecolor" else f"\x1b[48;5;{xterm256(r)}m"

    out = []
    for y in range(0, H, 2):
        line = ""
        for x in range(W):
            top = color_of(rows[y][x], outfit_name)
            bot = color_of(rows[y + 1][x], outfit_name)
            if top is None and bot is None:
                line += "\x1b[0m "
            elif top is None:
                line += "\x1b[0m" + fg(bot) + "▄"
            elif bot is None:
                line += "\x1b[0m" + fg(top) + "▀"
            else:
                line += fg(top) + bg(bot) + "▀"
        out.append(line + "\x1b[0m")
    return "\n".join(out) + "\n"


def to_png(rows, outfit_name, scale):
    img = Image.new("RGBA", (W * scale, H * scale), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    for y, row in enumerate(rows):
        for x, ch in enumerate(row):
            c = color_of(ch, outfit_name)
            if c:
                d.rectangle([x * scale, y * scale, (x + 1) * scale - 1, (y + 1) * scale - 1],
                            fill=hex_rgb(c) + (255,))
    return img


# ---------------------------------------------------------------- build
def main():
    for r in BASE + HAT:
        assert len(r) == W, r
    assert len(BASE) == H

    sprites = {}
    for oname in OUTFITS:
        sprites[oname] = {}
        for ename, e in EXPRESSIONS.items():
            frames = [compose(oname, ename, i) for i in range(len(e["frames"]))]
            sprites[oname][ename] = frames
            for i, rows in enumerate(frames):
                for mode, sub in (("truecolor", "truecolor"), ("256", "ansi256")):
                    p = os.path.join(ROOT, "ansi", sub, oname, f"{ename}.{i}.ans")
                    os.makedirs(os.path.dirname(p), exist_ok=True)
                    with open(p, "w", encoding="utf-8", newline="\n") as fh:
                        fh.write(to_ansi(rows, oname, "truecolor" if mode == "truecolor" else "256"))
                p = os.path.join(ROOT, "png", oname, f"{ename}.{i}.png")
                os.makedirs(os.path.dirname(p), exist_ok=True)
                to_png(rows, oname, 12).save(p)

    palette = {k: v for k, v in PALETTE.items() if v}
    sprites_doc = {
        "format": "tally-sprites/1",
        "width": W, "height": H,
        "note": "Each frame is 20 strings of 20 chars. '.' is transparent. "
                "Look up a char in outfits[o].colors first, then palette.",
        "palette": palette,
        "outfits": {o: v["colors"] for o, v in OUTFITS.items()},
        "sprites": sprites,
    }
    with open(os.path.join(ROOT, "sprites.json"), "w", encoding="utf-8") as fh:
        json.dump(sprites_doc, fh, indent=1, ensure_ascii=False)

    manifest = {
        "name": "Tally",
        "version": 1,
        "sprite": {"width_px": W, "height_px": H, "terminal_cols": W, "terminal_rows": H // 2,
                   "render": "half-block: '▀' fg=top pixel, bg=bottom pixel; '▄' when top is transparent"},
        "models": MODELS,
        "outfits": {o: {"note": v["note"]} for o, v in OUTFITS.items()},
        "expressions": {
            n: {
                "reference": e["ref"],
                "frames": len(e["frames"]),
                "timeline": [{"frame": f, "ms": ms} for f, ms in e["timeline"]],
                "kaomoji": KAOMOJI[n][0],
                "kaomoji_ascii": KAOMOJI[n][1],
            } for n, e in EXPRESSIONS.items()
        },
        "state_rules": STATE_RULES,
        "paths": {
            "ansi_truecolor": "ansi/truecolor/{outfit}/{expression}.{frame}.ans",
            "ansi_256": "ansi/ansi256/{outfit}/{expression}.{frame}.ans",
            "png_12x": "png/{outfit}/{expression}.{frame}.png",
            "pixels": "sprites.json",
        },
    }
    with open(os.path.join(ROOT, "manifest.json"), "w", encoding="utf-8") as fh:
        json.dump(manifest, fh, indent=2, ensure_ascii=False)

    build_sheet(sprites)
    n = sum(len(f) for o in sprites.values() for f in o.values())
    print(f"built {n} frames ({len(OUTFITS)} outfits x {len(EXPRESSIONS)} expressions)")


def build_sheet(sprites):
    scale, pad, label_h, left = 6, 14, 18, 70
    names = list(EXPRESSIONS)
    cw = W * scale + pad
    ch = H * scale + pad + label_h
    sheet = Image.new("RGB", (left + cw * len(names), label_h + ch * len(OUTFITS)), (30, 28, 36))
    d = ImageDraw.Draw(sheet)
    font = ImageFont.load_default()
    for c, n in enumerate(names):
        d.text((left + c * cw, 4), n, fill=(220, 214, 230), font=font)
    for r, o in enumerate(OUTFITS):
        y0 = label_h + r * ch
        d.text((6, y0 + H * scale // 2), o, fill=(220, 214, 230), font=font)
        for c, n in enumerate(names):
            img = to_png(sprites[o][n][0], o, scale)
            sheet.paste(img, (left + c * cw, y0 + 6), img)
    p = os.path.join(ROOT, "preview", "contact_sheet.png")
    os.makedirs(os.path.dirname(p), exist_ok=True)
    sheet.save(p)


if __name__ == "__main__":
    sys.exit(main())
