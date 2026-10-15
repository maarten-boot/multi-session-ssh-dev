# Session Workbench

Desktop app for running many Claude Code sessions side by side. Each session
is bound to a repository directory and has a **Remote** terminal (ssh → tmux →
claude on the isolated agent host), a **Local** terminal (host shell for git),
and a **Files** tab. See [SPEC.md](SPEC.md) and [PLAN.md](PLAN.md).

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
make test-ssh SSH_TARGET=localhost  # ssh/tmux round trip against a real host
make vet     # vet for linux and windows (all packages), darwin (internal packages)
make smoke   # headless GUI test of terminal fidelity (needs xvfb, xdotool, imagemagick)
make smoke-sessions  # headless GUI test of sessions/dialogs/switching (needs ssh localhost)
make smoke-files     # headless GUI test of the Files tab
make smoke-ui        # themes/colors, switcher, settings/keys, reconnect, badges, find/unicode/copy,
                     # folders, git markers
make smoke-all       # all GUI tests
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

- **Remote** (Ctrl+Shift+1): `ssh` → tmux session `cw-<id>` in the mapped
  directory. It survives closing the app and lost connections; Enter reconnects.
  When claude exits you get a shell.
- **Local** (Ctrl+Shift+2): your shell in the directory (not kept across app restarts).
- **Files** (Ctrl+Shift+3): the directory as a tree, refreshed whenever you
  switch to it. Double-click or Enter opens a file with its default application
  (directories expand); arrow keys navigate, F5 refreshes. Right-click to open
  the containing folder, copy the local or remote path, or refresh. Dotfiles
  (including `.git`) are hidden unless "Hidden files" is ticked. Programs and
  files the OS would execute rather than open (`.exe`, `.bat`, `.desktop`,
  `.app`, ELF/Mach-O binaries, …) are refused. Very large directories show the first
  5000 entries. In a git repository each entry gets a letter — **M** modified,
  **A** added, **D** deleted, **R** renamed, **U** untracked, **!** conflict — folders
  with changes get a dot, ignored files are dimmed, and the bar shows the branch with
  ahead/behind (↑/↓). This needs `git` on the host and only reads (no locks).

Terminals: Ctrl+Shift+F (Cmd+F) opens a find bar (Enter / Shift+Enter for next /
previous); links are clickable; emoji and CJK characters get their proper two-cell
width. With "copy on select" (Settings) a selection goes to the clipboard at once.

| Keys (Linux/Windows) | macOS | Action | `[keys]` name |
|---|---|---|---|
| Ctrl+Shift+PgDn / PgUp | ⇧⌘] / ⇧⌘[ | next / previous session | `next_session`, `prev_session` |
| Ctrl+Alt+1…9 | ⌘1…9 | session by number (shown in the sidebar) | `session_1` … `session_9` |
| Ctrl+Shift+1 / 2 / 3 | ⇧⌘1 / 2 / 3 | Remote / Local / Files tab | `tab_remote`, `tab_local`, `tab_files` |
| Ctrl+Shift+P | ⇧⌘P | quick switcher (fuzzy, most recent first) | `quick_switcher` |
| Ctrl+Shift+N | ⇧⌘N | new session | `new_session` |
| Ctrl+Shift+, | ⌘, | settings | `settings` |
| Ctrl+Shift+F | ⌘F | find in terminal | `find` |
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

[[sessions]]                              # sidebar order = order in this file
id = "s-7f3a"                             # tmux session cw-s-7f3a
name = "workbench"
host = "vibe"
local_path = "~/share-local/DEV/github.com/maarten-boot/multi-session-ssh-dev"
start_mode = "claude-continue"
group = "Work"                            # optional: sidebar folder
color_mode = "mono"                       # optional: overrides [ui] color_mode
local_init = "git status"                 # optional: typed into the Local tab
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
internal/fsbrowse      directory listing for the Files tab
internal/opener        open / reveal with the OS, refusing executables
internal/gitstatus     git status markers for the Files tab
packaging/             .desktop file, icons and nfpm config for .deb/.rpm
frontend/src           vanilla JS UI: main.js (app), terminal.js, files.js, ui.js,
                       appearance.js (themes, mono filter), keys.js, fuzzy.js
scripts/deps.sh        make check / make deps
scripts/gui-smoke*.sh  headless GUI test drivers
.github/workflows      CI: test + build on Linux/Windows/macOS, release on v* tags
```
