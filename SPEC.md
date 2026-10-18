# Session Workbench — Specification

Status: v1.2 (2026-10-17, app v0.8.21) — v1.0 shipped in v0.6.0. Since then: session folders (F5),
narrow and resizable sidebar (F6), Claude backups (F7), one instance per config (F8), working
copies and backups without rsync (F9), desktop notifications (F4), git status and the
repository check in the Files tab (B5, B6), Notes and Web tabs (N1, W1), notes for all sessions
(N2), terminal copy/paste (T2), live share and working copies (docs/CONNECTIVITY.md) and Linux
packages. Sections describe the implemented behaviour.

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
**sessions**. Each session is bound to one directory and provides five tabs, by default in this order:

| Tab        | What it is                                                                      |
|------------|---------------------------------------------------------------------------------|
| **Local**  | Terminal: local shell on the host, cwd = `<local path>` (git, builds, etc.)      |
| **Files**  | Tree view of `<local path>`; double-click opens the file with the OS default app |
| **Remote** | Terminal: SSH to agent host → tmux session → `cd <remote path>` → claude/shell  |
| **Notes**  | Free-text notes about the project (N1)                                          |
| **Web**    | Build runs and web links of the repository (W1)                                 |

A session reopens on the tab used last; new sessions start on Local. The order of the tabs and
which are shown is a setting (`tab_order`, `hidden_tabs`; Settings → Tabs); tabs added in a newer
version are appended to a saved order. The current tab is marked like the current session in
the sidebar (tinted background, thick accent bar, bold label).

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

[[hosts]]                               # no shared file system (docs/CONNECTIVITY.md)
name           = "lab"
ssh_target     = "me@lab-box"
files          = "sshfs"                # shared (default) | sshfs (live share) | sync (working copy)
remote_workdir = "~/sw"                 # default; sessions at <remote_workdir>/<dir>-<id>
```
Remote path = `remote_prefix + (local_path − local_prefix)`; separators are normalized, so
`C:\Users\me\share\proj` under `local_prefix = 'C:\Users\me\share'` maps to `<remote_prefix>/proj`
(Windows paths compare case-insensitively). A session whose local path is not under
`local_prefix` is rejected. Without prefixes the local path is used as-is, which only works for
POSIX paths (not on Windows). `~` is expanded in local paths and prefixes. Prefixes are not used
with `files = "sync"`/`"sshfs"`; set both prefixes or neither. Host rules are checked by the host
dialog and, for hand-edited values, when the config is read (§4.7).

**Session**:
```toml
[[sessions]]
id         = "s-7f3a"                  # stable, used for tmux name
name       = "terminal-app"            # display name (the dialog defaults it to the dir name)
host       = "vibe"
local_path = "/home/mboot/share-local/workspace/terminal-app"
start_mode = "claude-continue"         # claude | claude-continue | shell
color_mode = "mono"                    # optional, overrides the global color mode (T3)
local_init = "git status"              # optional, typed into the Local tab (L2)
env        = { FOO = "bar" }           # optional, Local tab environment (L2)
group      = "Work"                    # optional, sidebar folder listed in `groups` (F5)
links      = [{ name = "Staging", url = "https://staging.example.com" }]   # optional, Web tab (W1)
```
Session ids are generated (`s-` + 4 hex digits) and restricted to `[a-z0-9-]`, starting with a
letter or digit, at most 63 characters.
Sidebar order is the order of `[[sessions]]` in the file (no separate `order` field).

**Workspace roots** — the first entry is where Browse… starts in the New session dialog (else the
host's `local_prefix`), e.g. `~/share-local/workspace`.

## 4. Functional requirements

### 4.1 Session list
- F0. Multiple agent hosts are a first-class use case: sidebar groups sessions by host profile
  (collapsible); a host profile can be added/edited from the UI; a session's host is fixed at creation.
- F1. Left sidebar lists sessions (name, local path, status dot, number for Ctrl+Alt+n) grouped
  under their host. Drag (or menu Move up/down) to reorder within its folder and host. Edit
  (rename, folder, start mode, colors), disconnect, end remote session, delete (optionally ending
  the tmux session).
- F2. "New session" dialog: pick host profile → pick directory (Browse… starts at the first
  workspace root; any path under `local_prefix`) → name → folder → start mode (`claude`,
  `claude --continue`, plain shell). It previews the remote path or why the directory can't be
  used; one session per directory and host. Host and directory are fixed afterwards.
- F3. Sessions persist in config; on app start all sessions are restored (terminals re-attach lazily
  on first view, or eagerly — configurable).
- F4. Activity indicator: a session gets a badge when a non-visible terminal (or any terminal
  while the window is in the background) rings the bell, or produces output after 10 s idle (at
  least max(512 bytes, 2 × columns) within 2 s; redraws within 1.5 s of a resize don't count). The
  tab that caused it is marked too; the window title counts sessions with badges. claude waiting
  for input → bell; the user sets Claude Code's `preferredNotifChannel` to `terminal_bell`.
  A bell in such a terminal also shows a desktop notification (`notifications`, default on; Wails
  runtime notifications: freedesktop D-Bus on Linux, toasts on Windows, Notification Center on
  macOS, which may refuse an unsigned app): "Claude is waiting" for a Remote tab, "Bell in
  <tab>" otherwise, with the session name and host; at most one per session per minute, and
  none again until the session was looked at, which withdraws it and resets the minute. A click
  brings the window forward and opens that session's tab that rang. Without a notification service nothing is shown.
- F5. Folders: sessions can be put in folders (e.g. "Work", "Private"); `groups` in the config
  gives their order, `group` on a session its folder. The sidebar shows ungrouped sessions first,
  then each folder (collapsible, with a count), each split by host. Folders can be created,
  renamed (sessions follow), reordered and deleted (sessions stay, ungrouped). Sessions move
  between folders via the session dialog, "Move to folder…", or drag onto a folder header.
- F6. Sidebar width: drag the sidebar's right edge (or ←/→ on the focused edge; 12 rem to 60% of
  the window), double-click for the default (18.5 rem); stored in rem, so it scales with the font
  size, and remembered. Narrow sidebar: a toggle (header button, `toggle_sidebar` key) shrinks the sidebar to a rail
  of session markers (initials, number, status dot, badges; name and path as tooltip). Folder
  and host headers stay, abbreviated; the sidebar footer (Add host…, Settings…) is hidden.
  Remembered across restarts.
- F7. Claude backups: each agent host's `~/.claude` and `~/.claude.json` are copied with rsync
  over ssh to `<data dir>/claude-backups/<host name>/home/` (`status.json` beside it). Checked a
  minute after start and then every minute; a host is due when `claude_backup_every` (default 1h,
  5m…168h) has passed since its last attempt (kept across restarts), at most every 15 minutes
  after a failure (`claude_backup`, default on). rsync `-a --safe-links --no-devices
  --no-specials`, never `--delete`; excludes `.claude/.credentials.json` (claude's login),
  `cache/`, `plugins/cache/`, `plugins/marketplaces/`, `shell-snapshots/`, `session-env/`,
  `statsig/`, `telemetry/`, `debug/`. A missing `~/.claude` is a failure, a missing
  `~/.claude.json` is not. Status per host in the host header tooltip, ⚠ after a failure; host
  menu: back up now, restore (`--ignore-existing`: for a new or reinstalled host), open the
  backup folder. Uses rsync where installed; without it (Windows) the built-in transfer
  (internal/xfer): `tar -cf -` with the same exclusions on the agent host, unpacked here through
  `os.Root` (no absolute or `..` paths, symlinks only relative and inside, no hard links or
  devices; the exclusions applied here too), copying everything each time; restore sends a
  tar unpacked there with GNU `--skip-old-files` / BSD `-k`.
- F8. One instance per config file: an OS file lock `<config>.lock`; a second start brings the
  running window forward (Wails single-instance lock, keyed on the config path) or, where that
  is not available, shows only a message and touches nothing.
- F9. Working copies without rsync (Windows, or `SW_NO_RSYNC=1`): the built-in transfer gives
  rsync's result (`-a --delete --filter=':- .gitignore'`). At a repository's top the files to
  send are those git does not ignore (`git ls-files --cached --others --exclude-standard`) plus
  `.git`; for a session in a subdirectory (or outside a repository) every file that no
  `.gitignore` inside the session directory ignores (`git check-ignore --no-index` against an
  empty temporary repository). The agent copy is listed with POSIX `cksum`; new and changed
  files (by CRC and size) go over as a tar stream (executable bit from git's index on Windows);
  files missing here are removed there unless ignored here (those stay, as with rsync's filter).
  Symlinks are sent as links; none is followed through a linked parent. If every file is ignored
  nothing is sent (an error, not a deleted agent copy); without git here and with a
  `.gitignore`, nothing is deleted and the preview says why. The preview counts the same changes.

### 4.2 Remote tab
- R1. Implemented by spawning the system `ssh` client inside a local PTY (simplified; quoting
  per §6):
  `ssh -t -o ForwardAgent=no -o ForwardX11=no -o ForwardX11Trusted=no -o ClearAllForwardings=yes
  -o RemoteCommand=none -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -- <target>
  'exec "$SHELL" -lc "cd <remote_path> || { echo \"session directory <remote_path> does not exist
  on this host\" >&2; exit 3; }; exec tmux new-session -A -s cw-<id> -c \"\$PWD\" <pane cmd>"'`
  (a leading `~/` in the path is expanded through `"$HOME"`). Everything runs in the remote user's
  login shell so that PATH includes `~/.local/bin` (claude) or Homebrew (tmux). A missing remote
  directory exits 3 and the Remote tab says so (for a working copy it offers *Send to agent…*)
  instead of starting in `$HOME`.
- R2. **tmux is mandatory on the agent host** — it makes sessions survive app restarts, closed
  windows and network drops (not agent-host reboots). Reopening the tab re-attaches (`-A`); the
  start command only runs when the tmux session is first created. When claude exits, the pane
  falls back to a shell: pane cmd = `exec "$SHELL" -lic '<cmd>; exec "$SHELL" -l'`
  (`claude`, `claude --continue || claude`, or just a login shell: `--continue` exits 1 without a
  conversation in the directory, quitting claude exits 0). Changing a session's start mode
  offers to end a running tmux session and reconnects an open Remote tab, since a running
  session keeps its command. claude starts from an *interactive*
  login shell, so it gets the PATH of a manual login: a non-interactive shell skips most of
  `~/.bashrc` (Ubuntu's returns early), where `~/.local/bin` is often added.
- R3. When the connection ends, an overlay says why and offers to reconnect (Enter). Failed or
  dropped connections (ssh exit 255), and a Remote tab that could not start (e.g. the live share
  did not mount), reconnect automatically after 1, 2, 4, 8, 16, then every 30 s (back to 1 s after
  a connection that lasted 30 s; on/off with `auto_reconnect`; Esc stops). tmux detach / session
  end (exit 0) and a user Disconnect do not.
- R4. "End remote session" action (runs `tmux kill-session -t '=cw-<id>'` over ssh, exact match,
  `BatchMode=yes`, 20 s timeout; a session that is not running counts as ended), with confirm.
- R5. Agent forwarding and X11 forwarding are **always forced off** — forwarding the agent would
  hand the isolated machine the host's git credentials.

### 4.3 Local tab
- L1. Local login shell in a PTY with cwd = `local_path`. Shell from config (`[local] shell/args`),
  default: `$SHELL -l` (Linux/macOS), `pwsh.exe` → `powershell.exe` → `cmd.exe` (Windows, via ConPTY).
- L2. Optional per-session env vars and an optional init command.
- L3. Local shells are not persistent across app restarts (it's the host; use tmux locally if wanted).

### 4.4 Files tab
- B1. Lazy-loaded tree of `local_path` (expand to load children); directories first, sorted
  case-insensitively; columns for size (files) and last modification time (local,
  `YYYY-MM-DD HH:MM`, aligned; the status line repeats it for the selected entry); sort order
  name, newest first or oldest first (folders first, ties by name; per session, remembered); at most 5000 entries per directory. Hidden files (dotfiles, incl. `.git`)
  toggle, remembered per session (the Windows hidden attribute is not used). The 5000 cut is
  made in directory order, before sorting. Keyboard navigation: arrows (Left/Right collapse /
  expand or go to the parent), Home/End, PgUp/PgDn (20 rows), Enter, F5.
- B2. Double-click / Enter on a file → open with OS default (`xdg-open` / `open` / `ShellExecute`).
  Directory → expand. Files the OS would execute are refused (§6).
- B3. Context menu: on a file open and open containing folder (Linux: the file manager selects it
  via D-Bus FileManager1, else `xdg-open` on the folder); on a folder expand/collapse and open in
  the file manager; always copy path (local), copy path (remote), refresh. Symlinks are followed
  only inside the session directory: one leading out (or broken and pointing out) is shown,
  marked *↗ outside*, but not expanded or opened (§6). A link's size and time are its target's.
- B4. Manual refresh + refresh whenever the tab is shown (no filesystem watching in v1).
- B5. Git status (host-side `git status --porcelain=v2`, read-only: `--no-optional-locks`,
  `core.fsmonitor=false`, no submodules, 15 s timeout; skipped when the repository's own config
  defines `filter.*`, `include*`, `core.fsmonitor` or `core.worktree`, see docs/CONNECTIVITY.md): a letter per entry (M, A, D, R, U untracked,
  ! conflict), a dot on folders containing changes (! for conflicts), entries inside ignored or
  untracked folders inherit that, ignored entries in italics (not counted as changed); branch and
  ahead/behind in the files bar ("git status skipped" or "git: error" with the reason in its
  tooltip). Loaded after the listing; works when the session directory is a subdirectory of the
  repository; nothing shown outside a repository or without git.
- B6. Repository check: active hooks (executable regular files, or links to them, in the folder
  git uses, following `core.hooksPath`) and risky repository config, found through the git directory (not the
  worktree, which a planted `core.worktree` redirects) (programs git would run, push and traffic
  redirection; list in docs/CONNECTIVITY.md) are shown as a ⚠ on the session and a Files-tab
  banner; Details… lists each with why it matters and a shell-quoted command to inspect or remove
  it (`git -C / config --file <file> --unset-all <key>`, which works even when git refuses the
  repository), credentials in URLs masked. A config file git cannot read is a finding too. "Mark as reviewed" stores a fingerprint (`trusted_git`: hook
  names with a SHA-256 of their contents (first MB, plus the size), names and values of the flagged keys) and the warning
  returns when it changes; changes to keys that are not flagged don't count. Read-only; runs at
  start, on session switch and window focus, on Files refresh, when Details… opens, and every 2
  minutes for all sessions.

### 4.4a Notes tab
- N1. A plain-text editor per session, saved automatically (shortly after typing stops, on
  leaving the tab, when the window loses focus). Stored as `notes/<session id>.md` next to the
  config file (owner-only permissions, atomic writes, at most 4 MB), not in the project: notes
  stay out of git and are not visible to the agent host. Empty notes remove the file. Reloaded
  when shown if changed outside the app (unless there are unsaved edits); kept when the session
  is deleted.
- N2. Notes for all sessions: a page over the main area (sidebar ✎, `app_notes` key) with the same
  editor for `notes/_app.md` (`_` keeps it apart from session ids). Every `## name` heading starts a
  prompt (its text runs to the next `#`/`##` heading); the page lists them with Copy (clipboard)
  and Paste into Remote (bracketed paste into the active session's Remote tab once connected and
  the program there has turned bracketed paste on; a multi-line prompt is refused until then, so
  its lines cannot run one by one; no Enter). Headings inside fenced code blocks don't count. Esc, ✕, the key or selecting a session closes it.

### 4.4b Web tab
- W1. Links for the repository's web pages derived from its git remote (read from the
  repository's own config without includes; remote `origin`, else the first; credentials and
  ports dropped, always `https://host/path`): github.com gets Repository/Actions/Pull
  requests/Issues/Releases, hosts whose name contains "gitlab" the GitLab equivalents, any other
  host the repository page. Per-session custom links (`links`, http/https only). The last 12
  GitHub Actions runs via the user's GitHub CLI (`gh run list -R owner/repo`, so gh does not run
  git in the project; 30 s timeout), refreshed while visible (15 s while a run is in progress,
  else 2 min, also after an error). Without gh the tab says how to install and log in. Everything
  opens in the system browser; web pages are not embedded (GitHub forbids framing). No tokens are
  stored.

### 4.5 Terminals (both)
- T1. xterm.js with fit, web-links, search (find bar, Ctrl+Shift+F / Cmd+F), unicode11 addons;
  true color; scrollback configurable (default 10 000).
- T2. Copy on select (optional, `copy_on_select`), Ctrl+Shift+C/V copy/paste (Cmd+C/V on macOS),
  and on Linux/Windows plain Ctrl+C (copies only when text is selected, else the program gets its
  interrupt) and Ctrl+V (`ctrl_copy_paste`, default on); a right-click menu (Copy, Paste, Select
  all, Find…, Clear scrollback); clickable URLs (opened in the default browser).
- T3. Font family/size, theme (light/dark), configurable globally. The font size scales the whole
  app (sidebar, tab bar, dialogs, menus); a set font family is used everywhere too. The light
  theme is black text on a white background (the primary user's preference); all ordinary app text
  is black, secondary text too (no grey text: grey on grey is hard to read; hierarchy comes from
  size and weight, dimmed entries are shown in italics); color only marks status (errors,
  warnings, git letters, directory names). It must stay readable with programs that
  assume a dark background:
  - `color_mode = "full"` — ANSI colors as sent, but with a minimum contrast against the
    background (xterm.js `minimumContrastRatio`, default ~4.5), so yellow/light green on white
    get darkened instead of vanishing.
  - `color_mode = "mono"` — no coloring at all: every foreground rendered in the theme's text
    color (contrast ratio 21), including 256-color and truecolor output; bold/underline/inverse
    still shown.
  - Configurable globally (Settings: the default for every session), overridable per session
    in Edit… (Colors) or with the session menu (choosing the global value removes the override);
    switchable at runtime without restarting the terminal.
- T4. Resize propagates to the PTY (and thus through ssh to tmux).

### 4.6 Keyboard
Defaults (all rebindable in config). Chosen to avoid clashing with shells, tmux, and claude:

| Action                 | Linux/Windows        | macOS          | `[keys]` name                 |
|------------------------|----------------------|----------------|-------------------------------|
| Next / prev session    | Ctrl+Shift+PgDn/PgUp | Cmd+Shift+]/[  | `next_session`, `prev_session` |
| Session 1…9            | Ctrl+Alt+1…9         | Cmd+1…9        | `session_1` … `session_9`     |
| 1st … 9th visible tab  | Ctrl+Shift+1…9       | Cmd+Shift+1…9  | `tab_1` … `tab_9`             |
| A tab by name          | none                 | none           | `tab_local` … `tab_web`       |
| New session            | Ctrl+Shift+N         | Cmd+Shift+N    | `new_session`                 |
| Quick switcher (fuzzy) | Ctrl+Shift+P         | Cmd+Shift+P    | `quick_switcher`              |
| Find in terminal       | Ctrl+Shift+F         | Cmd+F          | `find`                        |
| Narrow / wide sidebar  | Ctrl+Shift+B         | Cmd+Shift+B    | `toggle_sidebar`              |
| Notes for all sessions | Ctrl+Shift+M         | Cmd+Shift+M    | `app_notes`                   |
| Settings               | Ctrl+Shift+,         | Cmd+,          | `settings`                    |

Each tab's tooltip shows its shortcut. Rebinding: `[keys]` table, action name → key (`Mod` =
Cmd on macOS, Ctrl elsewhere; `"none"` disables; `PgUp`/`PgDn` = `PageUp`/`PageDown`). A binding
needs Ctrl, Alt or Cmd, except F-keys. A bad binding is reported in the UI and the default is
kept; when two actions use the same key, both are named: your binding beats a default (whose
action becomes unbound), otherwise the later one in this table is disabled. Binding the terminal's
copy/paste/interrupt keys (Ctrl+C/V, Ctrl+Shift+C/V) is allowed with a warning. Read at start (restart after editing); Settings lists the current bindings.

### 4.7 Configuration
- Single TOML file: `$XDG_CONFIG_HOME` (default `~/.config`)`/session-workbench/config.toml` (Linux),
  `%APPDATA%\session-workbench\config.toml` (Windows), `~/Library/Application Support/...` (macOS);
  `-config <file>` overrides. A relative `$XDG_CONFIG_HOME`/`$XDG_DATA_HOME` is ignored.
- Contains: hosts, workspace roots, folders (`groups`), sessions (incl. links and the reviewed
  repository fingerprint), UI prefs (also editable in a Settings dialog), keybindings, local shell.
- Written atomically (write temp + rename) on every change. Hand-editable while the app is not
  running (comments are not preserved); unknown keys are rejected. If the file cannot be read the
  app starts empty, shows the error and never overwrites it. Invalid setting values get their
  defaults; a file hiding every tab shows them all. Hosts and sessions are checked with the
  dialogs' rules when the file is read (`session.Problems`), listed at the top of the sidebar,
  and the file is not changed. What keeps a session from working (invalid host, unknown start
  mode, relative `local_path`, duplicate id) is its error: red dot, reason above the tabs, no
  Remote tab. Otherwise the app falls back: unknown color mode → the global one, a non-http(s)
  link is not shown, the second of two hosts with one name is ignored; an empty name, a
  directory with two sessions on one host, a folder not in `groups`, a duplicate or empty folder
  name are only reported. A Remote tab that cannot start for such an error does not reconnect
  automatically; it shows the reason. Out-of-range `font_size` (6–48) and `scrollback`
  (1–1 000 000) get their defaults.
- `<config>.lock` next to the file keeps a second instance from using it (F8); `notes/` is there
  too.
- Data that is not configuration lives in the data dir: `$XDG_DATA_HOME/session-workbench`
  (Linux, default `~/.local/share`), `%LocalAppData%\session-workbench` (Windows),
  `~/Library/Application Support/session-workbench` (macOS): Claude backups, the window size
  and position (`window.json`: width, height, x, y, maximised; written atomically when the
  window closes, also on SIGTERM/Ctrl+C, which close it the normal way; while maximised,
  fullscreen or minimised the last normal size and position are kept; a missing file or a size
  outside 640×400…16384 gives 1280×800). The position is restored once the page is loaded:
  Wails sets it relative to the current monitor but reports screen coordinates, so the move is
  read back and corrected (at most 3 times); it is skipped when less than 100 pixels of the
  window, or its title bar, would be on a screen (only the primary monitor's place is known,
  the others count as possibly on any side), and on Wayland the compositor places windows. and on Linux WebKit's storage. Window state (active session, last tab per session, Hidden files and sort order per session, collapsed headers, sidebar width,
  narrow sidebar, switcher order, last host and start mode for a new session) is in the web view's
  storage: there on Linux, `%AppData%\session-workbench.exe\` on Windows (WebView2 default),
  WebKit's default store on macOS. Notes stay next to the config file.

## 5. Architecture

```
┌──────────────────────── Wails app (host) ────────────────────────┐
│ Frontend (plain JS + xterm.js, no framework)                      │
│   sidebar · tab bar · xterm instances · file tree · dialogs       │
│        ▲  events: term:data:<id> (base64), term:exit:<id>         │
│        │  calls:  TermWrite/Resize, StartLocal/Remote, ListDir,   │
│        │          OpenPath, session/host/folder CRUD, Sync*, Backup*│
│ Backend (Go)                                                      │
│   config · session manager · pty manager · pathmap · fs · opener  │
│   gitstatus · notes · forge · syncer · xfer · share · backup ·    │
│   instance · notify                                               │
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
notify.go                  desktop notifications
proc_*.go                  child processes without a console window (Windows)
internal/terminal          PTY processes, batched output events
internal/remote            ssh/tmux command lines
internal/pathmap           local -> remote path mapping
internal/config            TOML config
internal/session           hosts/sessions logic on top of the config
internal/fsbrowse          Files tab listing
internal/opener            open / reveal, refusing executables
internal/gitstatus         git markers and repository check for the Files tab
internal/forge             Web tab: forge links, GitHub runs via gh
internal/syncer            working copies (rsync or built-in out, git fetch back)
internal/xfer              built-in transfer without rsync (tar over ssh, cksum)
internal/share             live share (confined SFTP server, sshfs mounts)
internal/notes             per-session notes files
internal/backup            Claude backups (rsync or built-in copy of ~/.claude, restore)
internal/instance          one instance per config file (lock)
internal/winstate          window size and position across restarts
frontend/src               main.js, terminal.js, files.js, notes.js, appnotes.js, prompts.js,
                           web.js, ui.js, appearance.js, keys.js, fuzzy.js, format.js, style.css
```

## 6. Security considerations
See also docs/CONNECTIVITY.md: a shared repository lets the agent host write `.git/hooks` and
`.git/config`, which git on the host executes. The app never runs such programs itself; users
must check before running git on the host; the repository check (B6) flags hooks and risky
repository config and warns again whenever they change.

- App stores no secrets. Config contains only hostnames and paths. Claude backups leave out
  claude's login (`.credentials.json`); they do contain conversations, which may quote
  secrets claude saw, and `~/.claude.json` (MCP server settings): they are written owner-only.
- `ForwardAgent=no`, `ForwardX11=no` (plus `ClearAllForwardings=yes`, `RemoteCommand=none`)
  enforced on every ssh invocation regardless of ssh_config; the target follows `--` and may not
  start with `-`.
- Remote command built with strict shell quoting; session IDs restricted to `[a-z0-9-]`; paths
  quoted with single-quote escaping.
- Files tab only operates on the host side, inside the session directory: paths from the UI
  cannot escape it, and symlinks are followed only when their target is inside it (a link the
  agent host plants, e.g. `README` → `~/.bashrc`, is shown marked but not expanded or opened).
  Opening resolves the link first and checks and opens its target; only regular files open. It
  never executes files, only "open with default app". Refused (case-insensitive):
  - everywhere, by content: native binaries (ELF, PE/MZ, Mach-O thin and universal);
  - Windows: `.exe .com .bat .cmd .ps1 .psm1 .psd1 .vbs .vbe .js .jse .wsf .wsh .msi .msp .msc
    .scr .pif .lnk .hta .cpl .jar .reg .inf .scf .url .application .appref-ms .gadget
    .settingcontent-ms .library-ms .appx .msix .py .pyw .pyz .pyzw .chm .ws .wsc .sct .vb .xll`;
  - macOS: `.app .command .tool .terminal .workflow .action .pkg .mpkg .jar .webloc .inetloc
    .fileloc` and files with an executable bit;
  - Linux and other Unix: `.desktop .appimage .run .jar`.
- Fix commands the repository check suggests are shell-quoted: key names and paths come from a
  repository the agent host may have written.
- Live share: the SFTP server is confined with `os.Root`; symlinks only with relative targets
  inside the directory, never created through a symlinked parent (a rename re-checks moved
  links); hard links refused; ownership changes ignored. Working copies: the built-in transfer
  refuses paths through linked parents; fetching back uses `transfer.fsckObjects`. Claude
  backups: rsync `--safe-links --no-devices --no-specials`, never `--delete`, nothing in a
  backup is run; scheduled ones time out after 30 minutes.

## 7. Open questions
1. ~~Wails v2 vs v3~~ → v2.16 (decided).
2. ~~Should Files tab also show `git status` markers?~~ → yes, B5.
3. ~~Multiple agent hosts?~~ → yes, see F0. Same local dir may be opened against different hosts
   (each gets its own session).
4. ~~Session groups/folders in the sidebar?~~ → yes, F5.
5. ~~File access without a shared file system?~~ → live share over ssh (mode B) and working
   copies (mode C), host setting `files`; see docs/CONNECTIVITY.md.
6. Distribution: Linux `.deb` (Debian/Ubuntu) and `.rpm` (Fedora) with dependencies, menu entry
   and icon, alongside a plain tarball; Windows zip; macOS app zip. All unsigned.
