# Session Workbench

Desktop app for running many Claude Code sessions side by side. Each session
is bound to a repository directory and has a **Local** terminal (host shell for
git), a **Files** tab, a **Remote** terminal (ssh → tmux → claude on the isolated
agent host), **Notes** and **Web** (build runs and links). See [SPEC.md](SPEC.md) and [PLAN.md](PLAN.md).

**Setting up the agent host and sharing files with it — and what that means for
security — is described in [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).** Please
read its security section if the agent host can write to your repositories.

Status: v1 feature set (M0–M6): sessions with Remote, Local and Files tabs, themes
and color modes, quick switcher, activity badges, auto-reconnect, settings;
builds for Linux, Windows and macOS.

## Download

Every push to `main` is built by GitHub Actions (see the *build* workflow's
artifacts); tagged versions (`v*`) are published under **Releases**:

| File | Runs on | Notes |
|---|---|---|
| `session-workbench_…_amd64.deb` | Ubuntu 24.04+ / Debian 13+ (x86-64) | `sudo apt install ./session-workbench_…_amd64.deb` installs the dependencies and a menu entry |
| `session-workbench-…x86_64.rpm` | Fedora 40+ (x86-64) | `sudo dnf install ./session-workbench-…x86_64.rpm` installs the dependencies and a menu entry |
| `…-linux-amd64.tar.gz` | any of the above | just the binary; needs WebKitGTK 4.1 and GTK 3 (Debian/Ubuntu: `libwebkit2gtk-4.1-0`; Fedora: `webkit2gtk4.1 gtk3`) |
| `…-windows-amd64.zip` | Windows 10/11 (x86-64) | uses the built-in OpenSSH client and the WebView2 runtime (both standard on current Windows). The exe is unsigned: SmartScreen asks once (*More info → Run anyway*). |
| `…-macos-universal.zip` | macOS 11+ with Safari 16+ (Apple silicon and Intel) | contains `session-workbench.app`; unsigned: the first time, right-click the app → *Open*, or run `xattr -dr com.apple.quarantine session-workbench.app` |

The agent host needs `tmux` (and `claude`); nothing is installed there by the app.
`session-workbench -version` prints the version; it is also shown in Settings.

To release: tag and push (`git tag v0.7.0 && git push origin v0.7.0`); CI builds all three
platforms, sets the version from the tag and publishes a GitHub release.

## Build

Requirements: Go 1.25+, Node 18+ with npm, Wails CLI v2.16
(`go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`).

Linux also needs `libgtk-3-dev libwebkit2gtk-4.1-dev build-essential pkg-config`.
(`wails doctor` falsely reports libwebkit missing on systems that only ship
webkit2gtk-4.1; the Makefile adds `-tags webkit2_41` when 4.1 is present.)

Windows and macOS builds come from CI (or build on those systems with `wails build`;
Windows also cross-builds from Linux with `make build-windows`).

```
make check   # list missing build requirements
make deps    # install them (system packages via sudo, wails via go install)
make build   # → build/bin/session-workbench (version from git describe)
make build-windows   # → build/bin/session-workbench.exe, cross-built
make packages        # → dist/*.deb and dist/*.rpm (installs nfpm if needed)
make run     # build, then start the app
make dev     # live-reload dev mode
make test    # Go unit tests (race detector) + frontend unit tests (node --test)
             # (CI runs the same on Linux, Windows and macOS)
make test-ssh SSH_TARGET=localhost  # ssh/tmux, working-copy and live-share tests against a real host
make vet     # vet for linux and windows (all packages), darwin (internal packages)
make smoke   # headless GUI test of terminal fidelity (needs xvfb, xdotool, imagemagick)
make smoke-sessions  # headless GUI test of sessions/dialogs/switching (needs ssh localhost)
make smoke-files     # headless GUI test of the Files tab
make smoke-ui        # themes/colors, switcher, settings/keys, reconnect, badges, find/unicode/copy,
                     # folders, git markers, repository check, working copies, live share,
                     # notes and tab order, Web tab and custom tab order,
                     # layout (font size everywhere, current tab, narrow sidebar)
make smoke-all       # all GUI tests; each checks its results and fails on a wrong one
                     # (make -k smoke-all: run the others after a failure)
```

## Usage

Start `session-workbench`. The first time it shows **Add host…** to set up an agent host:

- **SSH target** — anything `ssh` accepts (`user@host`, a `~/.ssh/config` alias).
- **Local / remote prefix** — where the agent host sees your directories, e.g.
  `~/share-local` ↔ `/net/192.168.122.1/home/mboot/share-local`. Leave both empty
  if the paths are identical on both machines.

Then **New session** (Ctrl+Shift+N): pick a directory under the local prefix,
a name, and what a new remote tmux session starts with (`claude`,
`claude --continue`, or a shell). Each session has:

Default tab order (change it, or hide tabs, in Settings → Tabs; Ctrl+Shift+1…
always selects the 1st… visible tab):

- **Local** (Ctrl+Shift+1): your shell in the directory (not kept across app restarts).
- **Files** (Ctrl+Shift+2): the directory as a tree, refreshed whenever you
  switch to it. Double-click or Enter opens a file with its default application
  (directories expand); arrow keys navigate, F5 refreshes. Right-click to open
  the containing folder, copy the local or remote path, or refresh. Dotfiles
  (including `.git`) are hidden unless "Hidden files" is ticked. Programs and
  files the OS would execute rather than open (`.exe`, `.bat`, `.desktop`,
  `.app`, ELF/Mach-O binaries, …) are refused.
- **Remote** (Ctrl+Shift+3): `ssh` → tmux session `cw-<id>` in the mapped
  directory. It survives closing the app and lost connections; Enter reconnects.
  When claude exits you get a shell.
- **Notes** (Ctrl+Shift+4): your notes about the project, saved as you type. They
  are stored with the app's config (`notes/<session id>.md` next to `config.toml`),
  not in the project, so they stay out of git and off the agent host. Notes are
  kept when a session is deleted.
- **Web** (Ctrl+Shift+5): the repository's latest **GitHub Actions runs** (status,
  workflow, title, branch, age; refreshed while shown, every 15 s while one is
  running; click to open it in your browser), buttons for the repository's pages
  (Repository, Actions, Pull requests, Issues, Releases; GitLab equivalents), read
  from its git remote, and your own links per session. The runs need the GitHub CLI
  (`gh`) installed and logged in (`gh auth login`) on your computer; the app stores no
  tokens. Pages open in your normal browser: GitHub cannot be shown inside the app.

A session reopens on the tab you used last; new sessions start on Local. Sessions
look the way Settings says (theme, font, colors); a session can override the colors
in *Edit…* if needed. The font size applies to the whole app (sidebar, tab bar,
dialogs), as does the font family when you set one. Very large directories show the first
  5000 entries. In a git repository each entry gets a letter — **M** modified,
  **A** added, **D** deleted, **R** renamed, **U** untracked, **!** conflict — folders
  with changes get a dot, ignored files are in italics, and the bar shows the branch with
  ahead/behind (↑/↓). This needs `git` on the host and only reads (no locks). If the
  repository's own config names programs git status would run (filters, includes,
  fsmonitor), the status is skipped and the bar says so — see
  [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).

**Repository check.** If a session's repository contains hooks or settings that git
on your computer would run (or that redirect pushes), the session gets a ⚠ and the
Files tab a banner with the details. An agent host that can write the repository could
have planted them. Mark them as reviewed if they are yours; the warning returns when
anything changes. Background: [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).

Terminals: Ctrl+Shift+F (Cmd+F) opens a find bar (Enter / Shift+Enter for next /
previous); links are clickable; emoji and CJK characters get their proper two-cell
width. With "copy on select" (Settings) a selection goes to the clipboard at once.

| Keys (Linux/Windows) | macOS | Action | `[keys]` name |
|---|---|---|---|
| Ctrl+Shift+PgDn / PgUp | ⇧⌘] / ⇧⌘[ | next / previous session | `next_session`, `prev_session` |
| Ctrl+Alt+1…9 | ⌘1…9 | session by number (shown in the sidebar) | `session_1` … `session_9` |
| Ctrl+Shift+1…9 | ⇧⌘1…9 | 1st…9th visible tab | `tab_1` … `tab_9`; `tab_local`, `tab_remote`, … bind one tab wherever it is |
| Ctrl+Shift+P | ⇧⌘P | quick switcher (fuzzy, most recent first) | `quick_switcher` |
| Ctrl+Shift+N | ⇧⌘N | new session | `new_session` |
| Ctrl+Shift+, | ⌘, | settings | `settings` |
| Ctrl+Shift+F | ⌘F | find in terminal | `find` |
| Ctrl+Shift+B | ⇧⌘B | narrow / wide sidebar | `toggle_sidebar` |
| Ctrl+Shift+C / V | ⌘C / V | copy / paste | — |

Rebind in the config, e.g. `[keys]` `next_session = "Ctrl+Tab"`; `Mod` means Cmd on
macOS and Ctrl elsewhere; `"none"` disables a shortcut. Keys are physical positions,
so `Ctrl+Alt+1` is the 1 key on any layout. Bad or conflicting bindings are shown in
the sidebar.

**Colors.** The default theme is black on white. Programs often pick colors for dark
backgrounds; `full` colors keeps them but enforces a minimum contrast (yellow and
light green on white get darkened), `mono` removes all colors (16/256/truecolor,
foreground and background) and keeps bold, underline and inverse. Set it in
Settings (Ctrl+Shift+,), per session in the session dialog, or with "Use mono
colors" in the session menu; it switches live.

**Badges.** A session you are not looking at gets an orange dot when one of its
terminals rings the bell, and a blue ring when it starts printing after being quiet;
the window title shows how many sessions want attention. To have claude ring the bell
when it waits for you, run `/config` inside claude on the agent host and set the
notification channel to *terminal bell* (setting `preferredNotifChannel`, value
`terminal_bell`).

**Connection drops.** A dropped or failed Remote connection reconnects automatically
(1, 2, 4 … 30 s; Esc stops it). Detaching tmux or ending the remote session does not
reconnect; press Enter when you want to.

**Folders.** Group sessions into folders such as *Work* and *Private*: "+" →
*New folder…*, then put sessions in it from the session dialog, with *Move to
folder…*, or by dragging a session onto the folder header. Right-click a folder to
rename, reorder or delete it (deleting keeps its sessions). Sessions without a folder
are listed first.

Right-click a session (or its ⋯) to edit it (name, folder, start mode, colors), move
it up or down, move it to a folder, switch it between full and mono colors,
disconnect, end its remote tmux session, or delete it; drag sessions to reorder them
within their folder and host. Click a folder or host header to collapse it;
right-click a host to add a session on it, edit or delete it. Menus also work from the
keyboard (arrows, Enter, first letter). The app remembers the active session and
reopens it on start.

**Narrow sidebar.** « in the sidebar header (or Ctrl+Shift+B) shrinks the sidebar to a
rail of session markers: initials, number, status dot and badges; hover for the name
and path. » widens it again; the choice is remembered.

**Without a shared file system** there are two options when adding a host (details
and trade-offs in [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md)):

**Live share.** *File access: live share over ssh*: the app serves the session
directory over its own ssh connection and the agent host mounts it with `sshfs` (needs
sshfs + FUSE there, no root, no credentials for your machine). Nothing leaves the
session directory. The share runs while the app runs; the bar shows *● shared*.

**Working copies.** *File access: working copy*. Each session then gets a copy on the agent host;
**⇡ Send** mirrors your directory there (with a preview, never destroying unfetched
agent commits), and **⇣ Get** fetches the commits claude made there into
`agent-<id>/<branch>` and can fast-forward your branch. Needs rsync on both sides
(not available on Windows yet). Details: [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).

Agent and X11 forwarding are always forced off on every ssh connection. The
agent host needs `tmux`; `claude` is started from an interactive login shell, so it
is found on the same PATH as when you log in by hand (`~/.profile` and `~/.bashrc`).

### Config file

`~/.config/session-workbench/config.toml` (Linux), `%AppData%\session-workbench\config.toml`
(Windows), `~/Library/Application Support/session-workbench/config.toml` (macOS);
override with `-config <file>`. The app rewrites it on every change; hand edits are
fine while it is not running (comments are not kept). If the file cannot be parsed
the app shows why and saves nothing, so your file is never overwritten.

```toml
workspace_roots = ["~/share-local/DEV"]   # where "Browse…" starts
groups = ["Work", "Private"]              # sidebar folders, in this order

[ui]                                      # all of these are in Settings too
theme = "light"                           # or "dark"
color_mode = "full"                       # or "mono"
min_contrast = 4.5                        # 1 (off) .. 21
font_family = "JetBrains Mono"
font_size = 13
scrollback = 10000
connect_on_start = "lazy"                 # or "eager": connect all Remote tabs at startup
auto_reconnect = true
activity_badges = true
copy_on_select = false
tab_order = ["local", "files", "remote", "notes", "web"]
hidden_tabs = []                          # e.g. ["web"]

[keys]
next_session = "Ctrl+Tab"
prev_session = "Ctrl+Shift+Tab"

[local]
shell = "/bin/zsh"                        # Local tab shell; default $SHELL / pwsh
args = ["-l"]

[[hosts]]
name = "vibe"
ssh_target = "mboot@vibe-u26"
local_prefix = "~/share-local"
remote_prefix = "/net/192.168.122.1/home/mboot/share-local"

[[hosts]]                                 # a host without a shared file system
name = "lab"
ssh_target = "me@lab-box"
files = "sync"                            # working copies; "sshfs" = live share; default shared
remote_workdir = "~/sw"                   # where they live on the agent host

[[sessions]]                              # sidebar order = order in this file
id = "s-7f3a"                             # tmux session cw-s-7f3a
name = "workbench"
host = "vibe"
local_path = "~/share-local/DEV/github.com/maarten-boot/multi-session-ssh-dev"
start_mode = "claude-continue"
group = "Work"                            # optional: sidebar folder
trusted_git = "…"                         # set by "Mark as reviewed" (repository check)
color_mode = "mono"                       # optional: overrides [ui] color_mode
local_init = "git status"                 # optional: typed into the Local tab
links = [{ name = "Staging", url = "https://staging.example.com" }]   # optional: Web tab
env = { GIT_PAGER = "less -R" }           # optional: Local tab environment
```

## Layout

```
main.go, app.go        Wails entry point and frontend bindings
menu.go, proc_*.go     macOS menu bar; hiding console windows on Windows
internal/terminal      pty processes + batched output events
internal/pathmap       host path -> agent-host path
internal/remote        ssh/tmux command lines (quoting, forced options)
internal/config        TOML config load / atomic save
internal/session       hosts + sessions: validation, mapping, ordering
internal/notes         per-session notes files
internal/forge         Web tab: links from the git remote, GitHub runs via gh
internal/fsbrowse      directory listing for the Files tab
internal/opener        open / reveal with the OS, refusing executables
internal/gitstatus     git status markers and the repository check for the Files tab
internal/syncer        working copies: rsync send, git fetch back
internal/share         live share: confined SFTP server + sshfs mount manager
packaging/             .desktop file, icons and nfpm config for .deb/.rpm
frontend/src           vanilla JS UI: main.js (app), terminal.js, files.js, ui.js,
                       appearance.js (themes, mono filter), keys.js, fuzzy.js
scripts/deps.sh        make check / make deps
scripts/gui-smoke*.sh  headless GUI test drivers
.github/workflows      CI: test + build on Linux/Windows/macOS, release on v* tags
```
