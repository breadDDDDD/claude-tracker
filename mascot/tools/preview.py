"""
Preview Tally in the terminal.

  python mascot/tools/preview.py                      # every expression, shirt outfit
  python mascot/tools/preview.py blazer               # every expression, one outfit
  python mascot/tools/preview.py tee panic --animate  # loop one expression's timeline
  python mascot/tools/preview.py --256                # use the 256-colour files
"""
import json
import os
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def load(sub, outfit, expr, i):
    with open(os.path.join(ROOT, "ansi", sub, outfit, f"{expr}.{i}.ans"), encoding="utf-8") as fh:
        return fh.read().rstrip("\n").split("\n")


def main(argv):
    if os.name == "nt":
        os.system("")  # enable VT processing on Windows consoles
    sys.stdout.reconfigure(encoding="utf-8")
    manifest = json.load(open(os.path.join(ROOT, "manifest.json"), encoding="utf-8"))
    sub = "ansi256" if "--256" in argv else "truecolor"
    args = [a for a in argv if not a.startswith("--")]
    outfit = args[0] if args else "shirt"
    exprs = manifest["expressions"]

    if len(args) > 1 and "--animate" in argv:
        name = args[1]
        timeline = exprs[name]["timeline"]
        frames = [load(sub, outfit, name, i) for i in range(exprs[name]["frames"])]
        print("\n" * len(frames[0]), end="")
        try:
            while True:
                for step in timeline:
                    sys.stdout.write(f"\x1b[{len(frames[0])}A" + "\n".join(frames[step["frame"]]) + "\n")
                    sys.stdout.flush()
                    time.sleep((step["ms"] or 1000) / 1000)
        except KeyboardInterrupt:
            return

    names = args[1:] or list(exprs)
    per_row = 5
    for start in range(0, len(names), per_row):
        chunk = names[start:start + per_row]
        blocks = [load(sub, outfit, n, 0) for n in chunk]
        for line in range(len(blocks[0])):
            print("  ".join(b[line] for b in blocks))
        print("  ".join(f"{n} {exprs[n]['kaomoji_ascii']}"[:20].ljust(20) for n in chunk))
        print()


if __name__ == "__main__":
    main(sys.argv[1:])
