# Session Workbench

Desktop app for running many Claude Code sessions side by side. Each session
is bound to a repository directory and has a **Remote** terminal (ssh → tmux →
claude on the isolated agent host), a **Local** terminal (host shell for git),
and a **Files** tab. See [SPEC.md](SPEC.md) and [PLAN.md](PLAN.md).

Status: M1 — single local terminal.

## Build

Requirements: Go 1.25+, Node/npm, Wails CLI v2.16
(`go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`).

Linux also needs `libgtk-3-dev libwebkit2gtk-4.1-dev build-essential pkg-config`.
(`wails doctor` falsely reports libwebkit missing on systems that only ship
webkit2gtk-4.1; the Makefile adds `-tags webkit2_41` when 4.1 is present.)

```
make check   # list missing build requirements
make deps    # install them (system packages via sudo, wails via go install)
make build   # → build/bin/session-workbench
make dev     # live-reload dev mode
make test    # Go unit tests (race detector)
make vet     # vet for linux, windows, darwin
make smoke   # headless GUI smoke test (needs xvfb, xdotool, imagemagick)
```

## Layout

```
main.go, app.go        Wails entry point and frontend bindings
internal/terminal      pty processes + batched output events
frontend/src           vanilla JS UI (xterm.js)
scripts/gui-smoke.sh   headless GUI test driver
```
