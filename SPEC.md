# Session Workbench — Specification

Status: draft v0.2 (2026-10-14)

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
Remote path = `remote_prefix + (local_path − local_prefix)` (separators normalized; on Windows
`C:\...` → `/...`). A session whose local path is not under `local_prefix` is rejected.

**Session**:
```toml
[[sessions]]
id         = "s-7f3a"                  # stable, used for tmux name
name       = "terminal-app"            # display name, defaults to dir name
host       = "vibe"
local_path = "/home/mboot/share-local/workspace/terminal-app"
start_mode = "claude-continue"         # claude | claude-continue | shell
local_init = "git status"              # optional, typed into the Local tab (L2)
env        = { FOO = "bar" }           # optional, Local tab environment (L2)
```
Sidebar order is the order of `[[sessions]]` in the file (no separate `order` field).

**Workspace root(s)** — directories offered when creating a session (e.g. `~/share-local/workspace`).

## 4. Functional requirements

### 4.1 Session list
- F0. Multiple agent hosts are a first-class use case: sidebar groups sessions by host profile
  (collapsible); a host profile can be added/edited from the UI; a session's host is fixed at creation.
- F1. Left sidebar lists sessions (name, host, status dot). Drag to reorder. Rename, close, delete.
- F2. "New session" dialog: pick host profile → pick directory (browse under workspace roots, or any
  path under `local_prefix`) → name → start mode (`claude`, `claude --continue`, plain shell).
- F3. Sessions persist in config; on app start all sessions are restored (terminals re-attach lazily
  on first view, or eagerly — configurable).
- F4. Activity indicator: a session gets a badge when a non-visible terminal rings the bell or
  produces output after being idle (claude waiting for input → bell; user configures Claude Code's
  `preferredNotifChannel` to `terminal_bell`).

### 4.2 Remote tab
- R1. Implemented by spawning the system `ssh` client inside a local PTY:
  `ssh -t -o ForwardAgent=no -o ForwardX11=no <target> "tmux new-session -A -s cw-<id> -c '<remote_path>' '<start cmd>'"`
- R2. **tmux is mandatory on the agent host** — it makes sessions survive app restarts, host
  reboots, and network blips. Reopening the tab re-attaches (`-A`); the start command only runs when
  the tmux session is first created. When claude exits, the tmux pane falls back to a shell
  (start cmd is wrapped: `<cmd>; exec $SHELL -l`).
- R3. On SSH disconnect: show an overlay "Disconnected — press Enter to reconnect"; auto-reconnect
  optional.
- R4. "Kill remote session" action (runs `tmux kill-session -t cw-<id>` over ssh), with confirm.
- R5. Agent forwarding and X11 forwarding are **always forced off** — forwarding the agent would
  hand the isolated machine the host's git credentials.

### 4.3 Local tab
- L1. Local login shell in a PTY with cwd = `local_path`. Shell from config, default: `$SHELL`
  (Linux/macOS), `pwsh.exe` → `powershell.exe` fallback (Windows, via ConPTY).
- L2. Optional per-session env vars and an optional init command.
- L3. Local shells are not persistent across app restarts (it's the host; use tmux locally if wanted).

### 4.4 Files tab
- B1. Lazy-loaded tree of `local_path` (expand to load children). Hidden files toggle. `.git`
  hidden by default.
- B2. Double-click file → open with OS default (`xdg-open` / `open` / `start`). Directory → expand.
- B3. Context menu: open, open containing folder in OS file manager, copy path (local), copy path
  (remote), refresh.
- B4. Manual refresh + refresh on tab focus (no filesystem watching in v1).

### 4.5 Terminals (both)
- T1. xterm.js with fit, web-links, search, unicode11 addons; true color; scrollback configurable
  (default 10 000).
- T2. Copy on select (optional), Ctrl+Shift+C/V copy/paste (Cmd+C/V on macOS), clickable URLs.
- T3. Font family/size, theme (light/dark), configurable globally.
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

### 4.7 Configuration
- Single TOML file: `$XDG_CONFIG_HOME/session-workbench/config.toml` (Linux),
  `%APPDATA%\session-workbench\config.toml` (Windows), `~/Library/Application Support/...` (macOS).
- Contains: hosts, workspace roots, sessions, UI prefs, keybindings, shell commands.
- Written atomically (write temp + rename). Hand-editable; app reloads on restart.

## 5. Architecture

```
┌──────────────────────── Wails app (host) ────────────────────────┐
│ Frontend (plain JS + xterm.js, no framework)                      │
│   sidebar · tab bar · xterm instances · file tree · dialogs       │
│        ▲  events: pty output (base64), bell, exit                 │
│        │  calls:  Write, Resize, ListDir, Open, Session CRUD      │
│ Backend (Go)                                                      │
│   config  · session manager · pty manager · pathmap · fs · opener │
└───────────┬───────────────────────────────┬───────────────────────┘
            │ PTY: local shell              │ PTY: ssh → agent host → tmux → claude
```

- **Language/framework**: Go 1.26 + Wails v2.16 (v3 is still beta). On Ubuntu 24.04+ build with
  `-tags webkit2_41` (libwebkit2gtk-4.1). Frontend is vanilla JS with
  xterm.js installed via npm; no frontend framework.
- **PTY**: `github.com/aymanbagabas/go-pty` (Unix pty + Windows ConPTY behind one API), fallback
  `creack/pty` + `UserExistsError/conpty`.
- **SSH**: system `ssh` binary (OpenSSH; built into Windows 10+/macOS/Linux) run in a PTY. This keeps
  `~/.ssh/config`, agent, known_hosts, ProxyJump etc. working with zero code and zero credential
  handling in the app.
- **Data flow**: each terminal has an ID; backend reads PTY in a goroutine, batches output
  (~8 ms / 32 KB) and emits `pty:<id>:data`. Frontend sends keystrokes via `Write(id, data)`.
- **Hidden terminals keep running**; xterm.js instances stay alive in the DOM (display:none) so
  scrollback is preserved. Lazy creation on first view.

### Package layout (Go)
```
cmd/ (wails main)        internal/config     internal/session
internal/pty             internal/pathmap    internal/fsbrowse
internal/opener          frontend/ (index.html, main.js, style.css)
```

## 6. Security considerations
- App stores no secrets. Config contains only hostnames and paths.
- `ForwardAgent=no`, `ForwardX11=no` enforced on every ssh invocation regardless of ssh_config.
- Remote command built with strict shell quoting; session IDs restricted to `[a-z0-9-]`; paths
  quoted with single-quote escaping.
- Files tab only operates on the host side; never executes files, only "open with default app"
  (note: on Windows, default app for `.bat/.exe` is execution — v1 refuses to open executables).

## 7. Open questions
1. ~~Wails v2 vs v3~~ → v2.16 (decided).
2. Should Files tab also show `git status` markers later (host-side git)? (v2 candidate)
3. ~~Multiple agent hosts?~~ → yes, see F0. Same local dir may be opened against different hosts
   (each gets its own session).
4. Session groups/folders in the sidebar if session count grows large?
