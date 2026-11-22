#!/usr/bin/env python3
"""Build Shiliu's packaging icon from the checked-in ImageGen master.

Run with the workspace Python (needs Pillow):

    python scripts/make-icon.py

The source and generation prompt live in resources/design/. MyGo excludes
that directory from application bundles. Preserve the master's transparency.
"""

from __future__ import annotations

import pathlib

from PIL import Image

SIZE = 1024


def main() -> None:
    resources = pathlib.Path(__file__).resolve().parent.parent / "resources"
    source = resources / "design" / "icon-source.png"
    with Image.open(source) as master:
        if master.width != master.height:
            raise ValueError(f"icon master must be square: {master.size}")
        icon = master.convert("RGBA").resize((SIZE, SIZE), Image.Resampling.LANCZOS)
    output = resources / "icon.png"
    icon.save(output, optimize=True)
    print(f"wrote {output} ({SIZE}x{SIZE}, RGBA)")


if __name__ == "__main__":
    main()
