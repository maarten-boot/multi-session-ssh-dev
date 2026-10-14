# Session Workbench

Desktop app for running many Claude Code sessions side by side. Each session
is bound to a repository directory and has a **Remote** terminal (ssh → tmux →
claude on the isolated agent host), a **Local** terminal (host shell for git),
and a **Files** tab. See [SPEC.md](SPEC.md) and [PLAN.md](PLAN.md).

Status: M2 — one session with Remote (ssh → tmux) and Local tabs, set up
with command-line flags. Multiple sessions and a config file come in M3.

## Build

Requirements: Go 1.25+, Node 18+ with npm, Wails CLI v2.16
(`go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`).

Linux also needs `libgtk-3-dev libwebkit2gtk-4.1-dev build-essential pkg-config`.
(`wails doctor` falsely reports libwebkit missing on systems that only ship
webkit2gtk-4.1; the Makefile adds `-tags webkit2_41` when 4.1 is present.)

```
make check   # list missing build requirements
make deps    # install them (system packages via sudo, wails via go install)
make build   # → build/bin/session-workbench
make run     # build, then start the app
make dev     # live-reload dev mode
make test    # Go unit tests (race detector)
make test-ssh SSH_TARGET=localhost  # ssh/tmux round trip against a real host
make vet     # vet for linux, windows, darwin
make smoke   # headless GUI smoke test (needs xvfb, xdotool, imagemagick)
make smoke-remote SSH_TARGET=localhost  # same for the Remote tab
```

## Usage

```
session-workbench -dir ~/share-local/DEV/myproj \
    -ssh mboot@vibe-u26 \
    -local-prefix ~/share-local \
    -remote-prefix /net/192.168.122.1/home/mboot/share-local \
    -mode claude-continue
```

- **Remote tab** (Ctrl+Shift+1): `ssh` to the agent host, into tmux session
  `cw-<dir>-<hash>` in the mapped directory. The first time it runs `claude`,
  `claude --continue`, or a shell (`-mode`); when claude exits you get a shell.
  Closing the app or losing the connection leaves the tmux session running;
  press Enter (or restart the app) to re-attach.
- **Local tab** (Ctrl+Shift+2): your shell in `-dir`.
- Without `-ssh` only the Local tab is shown. Without the prefixes the
  directory is assumed to have the same path on both machines.
- Uses your own `ssh` and `~/.ssh/config`; agent and X11 forwarding are always
  forced off. The agent host needs `tmux`; `claude` is found through the
  login shell's PATH.

## Layout

```
main.go, app.go        Wails entry point and frontend bindings
internal/terminal      pty processes + batched output events
internal/pathmap       host path -> agent-host path
internal/remote        ssh/tmux command lines (quoting, forced options)
frontend/src           vanilla JS UI (xterm.js)
scripts/gui-smoke*.sh  headless GUI test drivers
```
