#!/usr/bin/env python3
"""Turns docs/TUTORIAL.md into a PDF (A4, black on white, one chapter per page).

usage: tutorial-pdf.py <TUTORIAL.md> <out.pdf> [version]
Needs the Python packages markdown-it-py, mdit-py-plugins and weasyprint (scripts/tutorial-pdf.sh makes a venv).
"""
import re
import sys
from pathlib import Path

from markdown_it import MarkdownIt
from mdit_py_plugins.anchors import anchors_plugin
from weasyprint import HTML

CSS = """
@page { size: A4; margin: 18mm 16mm 20mm 16mm;
        @bottom-center { content: "Session Workbench tutorial " string(version) " · page " counter(page); font-family: "DejaVu Sans", sans-serif; font-size: 8pt; } }
html { string-set: version attr(data-version); }
body { font-family: "DejaVu Sans", sans-serif; font-size: 10pt; line-height: 1.45; color: #000; background: #fff; }
h1 { font-size: 22pt; margin: 0 0 6mm; }
h2 { font-size: 15pt; page-break-before: always; border-bottom: 1px solid #000; padding-bottom: 1mm; }
h3 { font-size: 12pt; margin-top: 6mm; }
a { color: #000; }
code { font-family: "DejaVu Sans Mono", monospace; font-size: 9pt; }
pre { background: #f3f3f3; border: 1px solid #c0c0c0; padding: 3mm; white-space: pre-wrap; page-break-inside: avoid; }
pre code { font-size: 8pt; }
table { border-collapse: collapse; margin: 3mm 0; page-break-inside: avoid; }
th, td { border: 1px solid #000; padding: 1.2mm 2mm; vertical-align: top; text-align: left; }
img { max-width: 100%; border: 1px solid #000; margin: 2mm 0; page-break-inside: avoid; }
p:has(img) { page-break-inside: avoid; }
"""


def main():
    src, out = Path(sys.argv[1]), Path(sys.argv[2])
    version = sys.argv[3] if len(sys.argv) > 3 else ""
    text = src.read_text(encoding="utf-8")
    # Links to other files make no sense on paper: keep their text (not
    # pictures, nor links within the tutorial or to the web).
    text = re.sub(r"(?<!!)\[([^\]]+)\]\((?!#|https?:)[^)]+\)", r"\1", text)
    # CommonMark as on GitHub, with tables and heading anchors (#3-prepare-the-agent-host).
    md = MarkdownIt("commonmark").enable("table").enable("strikethrough")
    md.use(anchors_plugin, min_level=1, max_level=3)
    body = md.render(text)
    html = f'<html data-version="{version}"><head><meta charset="utf-8"><style>{CSS}</style></head><body>{body}</body></html>'
    out.parent.mkdir(parents=True, exist_ok=True)
    HTML(string=html, base_url=str(src.parent)).write_pdf(out)


if __name__ == "__main__":
    main()
