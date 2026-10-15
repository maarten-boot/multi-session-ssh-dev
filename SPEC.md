# Session Workbench — Specification

Status: v1.0 — implemented in v0.6.0 (2026-10-15). Sections describe the shipped behaviour.

## 1. Problem

Development with Claude Code runs on an isolated machine (VM or hardware, the "**agent host**").
Git repositories live on the user's workstation (the "**host**") and are shared to the agent host
over NFS. Git credentials never leave the host; the agent host cannot push.

Today each concurrent piece of work is handled by an XFCE workspace containing:
an SSH terminal on the agent host (running `claude`), a local terminal on the host (for git), and a
file manager — all pointing at the same repository. Switching between work items means switching
workspaces. This does not scale well past a few sessions and is Linux/XFCE specific.

## 2. Goal

A cross-platform (Linux, Windows, macOS) desktop app running on the **host** that manages many
**sessions**. Each session is bound to one directory and provides three tabs:

| Tab        | What it is                                                                      |
|------------|---------------------------------------------------------------------------------|
| **Remote** | Terminal: SSH to agent host → tmux session → `cd <remote path>` → claude/shell  |
| **Local**  | Terminal: local shell on the host, cwd = `<local path>` (git, builds, etc.)      |
| **Files**  | Tree view of `<local path>`; double-click opens the file with the OS default app |

Switching sessions and tabs is instant and keyboard driven.

### Non-goals (v1)
- No built-in editor, diff viewer, or git UI.
- No credential handling of any kind (the app relies on the user's existing SSH setup).
- No running the app on the agent host; no multi-user features.
- No managing NFS mounts.

## 3. Core concepts

**Host profile** — how to reach an agent host and how paths map:
```toml
[[hosts]]
name         = "vibe"
ssh_target   = "mboot@vibe-u26"         # anything `ssh` accepts; ~/.ssh/config honored
local_prefix = "/home/mboot/share-local"
remote_prefix = "/net/192.168.122.1/home/mboot/share-local"
```
Remote path = `remote_prefix + (local_path − local_prefix)`; separators are normalized, so
`C:\Users\me\share\proj` under `local_prefix = 'C:\Users\me\share'` maps to `<remote_prefix>/proj`
(Windows paths compare case-insensitively). A session whose local path is not under
`local_prefix` is rejected. Without prefixes the local path is used as-is, which only works for
POSIX paths (not on Windows). `~` is expanded in local paths and prefixes.

**Session**:
```toml
[[sessions]]
id         = "s-7f3a"                  # stable, used for tmux name
name       = "terminal-app"            # display name, defaults to dir name
host       = "vibe"
local_path = "/home/mboot/share-local/workspace/terminal-app"
start_mode = "claude-continue"         # claude | claude-continue | shell
color_mode = "mono"                    # optional, overrides the global color mode (T3)
local_init = "git status"              # optional, typed into the Local tab (L2)
env        = { FOO = "bar" }           # optional, Local tab environment (L2)
```
Session ids are generated (`s-` + 4 hex digits) and restricted to `[a-z0-9-]`.
Sidebar order is the order of `[[sessions]]` in the file (no separate `order` field).

**Workspace root(s)** — directories offered when creating a session (e.g. `~/share-local/workspace`).

## 4. Functional requirements

### 4.1 Session list
- F0. Multiple agent hosts are a first-class use case: sidebar groups sessions by host profile
  (collapsible); a host profile can be added/edited from the UI; a session's host is fixed at creation.
- F1. Left sidebar lists sessions (name, local path, status dot, number for Ctrl+Alt+n) grouped
  under their host. Drag (or menu Move up/down) to reorder within a host. Edit (rename, start
  mode, colors), disconnect, end remote session, delete (optionally ending the tmux session).
- F2. "New session" dialog: pick host profile → pick directory (browse under workspace roots, or any
  path under `local_prefix`) → name → start mode (`claude`, `claude --continue`, plain shell).
- F3. Sessions persist in config; on app start all sessions are restored (terminals re-attach lazily
  on first view, or eagerly — configurable).
- F4. Activity indicator: a session gets a badge when a non-visible terminal rings the bell or
  produces output after being idle (claude waiting for input → bell; user configures Claude Code's
  `preferredNotifChannel` to `terminal_bell`).

### 4.2 Remote tab
- R1. Implemented by spawning the system `ssh` client inside a local PTY (simplified; quoting
  per §6):
  `ssh -t -o ForwardAgent=no -o ForwardX11=no -o ForwardX11Trusted=no -o ClearAllForwardings=yes
  -o RemoteCommand=none -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -- <target>
  'exec "$SHELL" -lc "cd <remote_path> || exit 1; exec tmux new-session -A -s cw-<id> -c <remote_path> <pane cmd>"'`
  Everything runs in the remote user's login shell so that PATH includes `~/.local/bin` (claude)
  or Homebrew (tmux). A missing remote directory is reported instead of starting in `$HOME`.
- R2. **tmux is mandatory on the agent host** — it makes sessions survive app restarts, closed
  windows and network drops (not agent-host reboots). Reopening the tab re-attaches (`-A`); the
  start command only runs when the tmux session is first created. When claude exits, the pane
  falls back to a shell: pane cmd = `exec "$SHELL" -lc '<cmd>; exec "$SHELL" -l'`
  (`claude`, `claude --continue`, or just a login shell).
- R3. When the connection ends, an overlay says why and offers to reconnect (Enter). Failed or
  dropped connections (ssh exit 255) reconnect automatically with backoff 1, 2, 4 … 30 s
  (configurable; Esc stops). tmux detach / session end (exit 0) and a user Disconnect do not.
- R4. "End remote session" action (runs `tmux kill-session -t '=cw-<id>'` over ssh, exact match,
  `BatchMode=yes`), with confirm.
- R5. Agent forwarding and X11 forwarding are **always forced off** — forwarding the agent would
  hand the isolated machine the host's git credentials.

### 4.3 Local tab
- L1. Local login shell in a PTY with cwd = `local_path`. Shell from config (`[local] shell/args`),
  default: `$SHELL -l` (Linux/macOS), `pwsh.exe` → `powershell.exe` → `cmd.exe` (Windows, via ConPTY).
- L2. Optional per-session env vars and an optional init command.
- L3. Local shells are not persistent across app restarts (it's the host; use tmux locally if wanted).

### 4.4 Files tab
- B1. Lazy-loaded tree of `local_path` (expand to load children); directories first, sorted
  case-insensitively; at most 5000 entries per directory. Hidden files (dotfiles, incl. `.git`)
  toggle, remembered per session. Keyboard navigation (arrows, Enter, F5).
- B2. Double-click / Enter on a file → open with OS default (`xdg-open` / `open` / `ShellExecute`).
  Directory → expand. Files the OS would execute are refused (§6).
- B3. Context menu: open, open containing folder in OS file manager, copy path (local), copy path
  (remote), refresh.
- B4. Manual refresh + refresh whenever the tab is shown (no filesystem watching in v1).

### 4.5 Terminals (both)
- T1. xterm.js with fit, web-links, search (find bar, Ctrl+Shift+F / Cmd+F), unicode11 addons;
  true color; scrollback configurable (default 10 000).
- T2. Copy on select (optional, `copy_on_select`), Ctrl+Shift+C/V copy/paste (Cmd+C/V on macOS),
  clickable URLs (opened in the default browser).
- T3. Font family/size, theme (light/dark), configurable globally. The light theme is black text
  on a white background (the primary user's preference) and must stay readable with programs
  that assume a dark background:
  - `color_mode = "full"` — ANSI colors as sent, but with a minimum contrast against the
    background (xterm.js `minimumContrastRatio`, default ~4.5), so yellow/light green on white
    get darkened instead of vanishing.
  - `color_mode = "mono"` — no coloring at all: every foreground rendered in the theme's text
    color (contrast ratio 21), including 256-color and truecolor output; bold/underline/inverse
    still shown.
  - Configurable globally, overridable per session; switchable at runtime without restarting
    the terminal.
- T4. Resize propagates to the PTY (and thus through ssh to tmux).

### 4.6 Keyboard
Defaults (all rebindable in config). Chosen to avoid clashing with shells, tmux, and claude:

| Action                 | Linux/Windows        | macOS          |
|------------------------|----------------------|----------------|
| Next / prev session    | Ctrl+Shift+PgDn/PgUp | Cmd+Shift+]/[  |
| Session 1…9            | Ctrl+Alt+1…9         | Cmd+1…9        |
| Remote / Local / Files | Ctrl+Shift+1/2/3     | Cmd+Shift+1/2/3|
| New session            | Ctrl+Shift+N         | Cmd+Shift+N    |
| Quick switcher (fuzzy) | Ctrl+Shift+P         | Cmd+Shift+P    |
| Find in terminal       | Ctrl+Shift+F         | Cmd+F          |
| Settings               | Ctrl+Shift+,         | Cmd+,          |

Rebinding: `[keys]` table, action name → key (`Mod` = Cmd on macOS, Ctrl elsewhere; `"none"`
disables). Bad or conflicting bindings are reported in the UI and fall back to the default.

### 4.7 Configuration
- Single TOML file: `$XDG_CONFIG_HOME/session-workbench/config.toml` (Linux),
  `%APPDATA%\session-workbench\config.toml` (Windows), `~/Library/Application Support/...` (macOS);
  `-config <file>` overrides.
- Contains: hosts, workspace roots, sessions, UI prefs (also editable in a Settings dialog),
  keybindings, local shell.
- Written atomically (write temp + rename) on every change. Hand-editable while the app is not
  running (comments are not preserved); unknown keys are rejected. If the file cannot be read the
  app starts empty, shows the error and never overwrites it.

## 5. Architecture

```
┌──────────────────────── Wails app (host) ────────────────────────┐
│ Frontend (plain JS + xterm.js, no framework)                      │
│   sidebar · tab bar · xterm instances · file tree · dialogs       │
│        ▲  events: term:data:<id> (base64), term:exit:<id>         │
│        │  calls:  Write, Resize, ListDir, Open, Session CRUD      │
│ Backend (Go)                                                      │
│   config  · session manager · pty manager · pathmap · fs · opener │
└───────────┬───────────────────────────────┬───────────────────────┘
            │ PTY: local shell              │ PTY: ssh → agent host → tmux → claude
```

- **Language/framework**: Go 1.25+ + Wails v2.16 (v3 is still beta). On Ubuntu 24.04+ build with
  `-tags webkit2_41` (libwebkit2gtk-4.1). Frontend is vanilla JS with xterm.js installed via npm
  and bundled by Vite 6 (Node 18+); no frontend framework.
- **PTY**: `github.com/aymanbagabas/go-pty` (Unix pty via creack/pty + Windows ConPTY behind one
  API).
- **SSH**: system `ssh` binary (OpenSSH; built into Windows 10+/macOS/Linux) run in a PTY. This keeps
  `~/.ssh/config`, agent, known_hosts, ProxyJump etc. working with zero code and zero credential
  handling in the app.
- **Data flow**: each terminal has an ID; backend reads PTY in a goroutine, batches output
  (~8 ms / 32 KB) and emits `term:data:<id>` (base64, so UTF-8 split across chunks survives) and
  `term:exit:<id>`. Frontend sends keystrokes via `TermWrite(id, data)`. Bells and activity are
  detected in the frontend (xterm.js), as are colors stripped for `mono`.
- **Hidden terminals keep running**; xterm.js instances stay alive in the DOM (display:none) so
  scrollback is preserved. Lazy creation on first view.

### Package layout
```
main.go, app.go, menu.go   Wails entry, bindings, macOS menu
internal/terminal          PTY processes, batched output events
internal/remote            ssh/tmux command lines
internal/pathmap           local -> remote path mapping
internal/config            TOML config
internal/session           hosts/sessions logic on top of the config
internal/fsbrowse          Files tab listing
internal/opener            open / reveal, refusing executables
frontend/src               main.js, terminal.js, files.js, ui.js, appearance.js, keys.js, fuzzy.js
```

## 6. Security considerations
- App stores no secrets. Config contains only hostnames and paths.
- `ForwardAgent=no`, `ForwardX11=no` (plus `ClearAllForwardings=yes`, `RemoteCommand=none`)
  enforced on every ssh invocation regardless of ssh_config; the target follows `--` and may not
  start with `-`.
- Remote command built with strict shell quoting; session IDs restricted to `[a-z0-9-]`; paths
  quoted with single-quote escaping.
- Files tab only operates on the host side, inside the session directory (paths from the UI cannot
  escape it); never executes files, only "open with default app". Refused: native binaries
  (ELF/PE/Mach-O, by content) everywhere; per OS the extensions the OS would run (`.exe .bat .cmd
  .ps1 .lnk .msi …` on Windows, `.app .command …` and executable-bit files on macOS,
  `.desktop .AppImage …` on Linux).

## 7. Open questions
1. ~~Wails v2 vs v3~~ → v2.16 (decided).
2. Should Files tab also show `git status` markers later (host-side git)? (v2 candidate)
3. ~~Multiple agent hosts?~~ → yes, see F0. Same local dir may be opened against different hosts
   (each gets its own session).
4. Session groups/folders in the sidebar if session count grows large?
