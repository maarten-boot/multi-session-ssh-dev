# Session Workbench

Desktop app for running many Claude Code sessions side by side. Each session
is bound to a repository directory and has a **Remote** terminal (ssh → tmux →
claude on the isolated agent host), a **Local** terminal (host shell for git),
and a **Files** tab. See [SPEC.md](SPEC.md) and [PLAN.md](PLAN.md).

Status: M3 — multiple sessions with a sidebar, persisted in a config file.
Next: Files tab (M4), keyboard/polish (M5), cross-platform CI (M6).

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
make smoke   # headless GUI test of terminal fidelity (needs xvfb, xdotool, imagemagick)
make smoke-sessions  # headless GUI test of sessions/dialogs/switching (needs ssh localhost)
```

## Usage

Start `session-workbench`. The first time it asks for an **agent host**:

- **SSH target** — anything `ssh` accepts (`user@host`, a `~/.ssh/config` alias).
- **Local / remote prefix** — where the agent host sees your directories, e.g.
  `~/share-local` ↔ `/net/192.168.122.1/home/mboot/share-local`. Leave both empty
  if the paths are identical on both machines.

Then **New session** (Ctrl+Shift+N): pick a directory under the local prefix,
a name, and what a new remote tmux session starts with (`claude`,
`claude --continue`, or a shell). Each session has:

- **Remote** (Ctrl+Shift+1): `ssh` → tmux session `cw-<id>` in the mapped
  directory. It survives closing the app and lost connections; Enter reconnects.
  When claude exits you get a shell.
- **Local** (Ctrl+Shift+2): your shell in the directory.

| Keys (Linux/Windows) | macOS | Action |
|---|---|---|
| Ctrl+Shift+PgDn / PgUp | ⇧⌘] / ⇧⌘[ | next / previous session |
| Ctrl+Alt+1…9 | ⌘1…9 | session by number (shown in the sidebar) |
| Ctrl+Shift+1 / 2 | ⇧⌘1 / 2 | Remote / Local tab |
| Ctrl+Shift+N | ⇧⌘N | new session |
| Ctrl+Shift+C / V | ⌘C / V | copy / paste |

Right-click a session (or its ⋯) to edit, reorder, disconnect, end its remote
tmux session, or delete it; drag sessions to reorder them within a host.
Right-click a host header to edit or delete the host.

Agent and X11 forwarding are always forced off on every ssh connection. The
agent host needs `tmux`; `claude` is found through the login shell's PATH.

### Config file

`~/.config/session-workbench/config.toml` (Linux), `%AppData%\session-workbench\config.toml`
(Windows), `~/Library/Application Support/session-workbench/config.toml` (macOS);
override with `-config <file>`. The app rewrites it on every change; hand edits are
fine while it is not running (comments are not kept). If the file cannot be parsed
the app shows why and saves nothing, so your file is never overwritten.

```toml
workspace_roots = ["~/share-local/DEV"]   # where "Browse…" starts

[ui]
font_family = "JetBrains Mono"
font_size = 13
scrollback = 10000
connect_on_start = "lazy"                 # or "eager": connect all Remote tabs at startup

[local]
shell = "/bin/zsh"                        # Local tab shell; default $SHELL / pwsh
args = ["-l"]

[[hosts]]
name = "vibe"
ssh_target = "mboot@vibe-u26"
local_prefix = "~/share-local"
remote_prefix = "/net/192.168.122.1/home/mboot/share-local"

[[sessions]]                              # sidebar order = order in this file
id = "s-7f3a"                             # tmux session cw-s-7f3a
name = "workbench"
host = "vibe"
local_path = "~/share-local/DEV/github.com/maarten-boot/multi-session-ssh-dev"
start_mode = "claude-continue"
local_init = "git status"                 # optional: typed into the Local tab
env = { GIT_PAGER = "less -R" }           # optional: Local tab environment
```

## Layout

```
main.go, app.go        Wails entry point and frontend bindings
internal/terminal      pty processes + batched output events
internal/pathmap       host path -> agent-host path
internal/remote        ssh/tmux command lines (quoting, forced options)
internal/config        TOML config load / atomic save
internal/session       hosts + sessions: validation, mapping, ordering
frontend/src           vanilla JS UI (main.js app, terminal.js xterm.js view)
scripts/gui-smoke*.sh  headless GUI test drivers
```
