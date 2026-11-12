#!/bin/sh
# docs/TUTORIAL.md → a PDF, with weasyprint in a venv of its own (nothing is
# installed on the system; the venv is kept for the next run).
# usage: scripts/tutorial-pdf.sh <out.pdf> [version] [venv dir]
set -e
OUT=${1:-build/session-workbench-tutorial.pdf}
VERSION=${2:-}
VENV=${3:-${TMPDIR:-/tmp}/multi-session-ssh-dev/pdf-venv}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
if ! "$VENV/bin/python" -c "import markdown_it, mdit_py_plugins, weasyprint" 2>/dev/null; then
	[ -x "$VENV/bin/python" ] || python3 -m venv "$VENV"
	"$VENV/bin/pip" install --quiet markdown-it-py==3.0.0 mdit-py-plugins==0.4.2 weasyprint==63.1
fi
"$VENV/bin/python" -I "$ROOT/scripts/tutorial-pdf.py" "$ROOT/docs/TUTORIAL.md" "$OUT" "$VERSION"
echo "wrote $OUT"
