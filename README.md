# Session Workbench

Desktop app for running many Claude Code sessions side by side. Each session
is bound to a repository directory and has a **Local** terminal (host shell for
git), a **Files** tab, a **Remote** terminal (ssh → tmux → claude on the isolated
agent host), **Notes** and **Web** (build runs and links). See [SPEC.md](SPEC.md) and [PLAN.md](PLAN.md).

**Setting up the agent host and sharing files with it — and what that means for
security — is described in [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).** Please
read its security section if the agent host can write to your repositories.

Status: v0.8.11: sessions with Local, Files, Remote, Notes and Web tabs, session
folders, themes and color modes, quick switcher, activity badges, auto-reconnect,
live share and working copies for hosts without a shared file system, Claude backups,
settings; builds for Linux, Windows and macOS.

## Download

Every push to `main` is built by GitHub Actions (see the *build* workflow's
artifacts); tagged versions (`v*`) are published under **Releases**:

| File | Runs on | Notes |
|---|---|---|
| `session-workbench_…_amd64.deb` | Ubuntu 24.04+ / Debian 13+ (x86-64) | `sudo apt install ./session-workbench_…_amd64.deb` installs the dependencies (and recommends git, rsync, xdg-utils) and a menu entry |
| `session-workbench-…x86_64.rpm` | Fedora 40+ (x86-64) | `sudo dnf install ./session-workbench-…x86_64.rpm` installs the dependencies and a menu entry |
| `…-linux-amd64.tar.gz` | any of the above | just the binary; needs WebKitGTK 4.1 and GTK 3 (Debian/Ubuntu: `libwebkit2gtk-4.1-0`; Fedora: `webkit2gtk4.1 gtk3`) |
| `…-windows-amd64.zip` | Windows 10/11 (x86-64) | uses the built-in OpenSSH client and the WebView2 runtime (both standard on current Windows). The exe is unsigned: SmartScreen asks once (*More info → Run anyway*). |
| `…-macos-universal.zip` | macOS 12+ (Apple silicon and Intel) | contains `session-workbench.app`; unsigned: the first time, right-click the app → *Open*, or run `xattr -dr com.apple.quarantine session-workbench.app` |

On your computer the app uses `ssh`, and optionally `git` (Files tab markers),
`rsync` (Claude backups, working copies) and `gh` (Web tab runs). The agent host needs
`tmux`, `claude` and `rsync`; nothing is installed there by the app (requirements and how
to check them: [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md#requirements-on-the-agent-host)).
`session-workbench -version` prints the version; it is also shown in Settings.

To release: tag and push (`git tag v0.8.12 && git push origin v0.8.12`); CI builds all three
platforms, sets the version from the tag (also in `wails.json`'s `productVersion`, which local
builds take as is) and publishes a GitHub release.

## Build

Requirements: Go 1.25+, Node 18+ with npm, Wails CLI v2.16
(`go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`).

Linux also needs `libgtk-3-dev libwebkit2gtk-4.1-dev build-essential pkg-config`.
(`wails doctor` falsely reports libwebkit missing on systems that only ship
webkit2gtk-4.1; the Makefile adds `-tags webkit2_41` when 4.1 is present.)

Windows and macOS builds come from CI (or build on those systems with
`wails build -ldflags "-X main.version=$(git describe --tags --always)"`; without the
`-ldflags` the version shows as "dev"). Windows also cross-builds from Linux with
`make build-windows`.

```
make check   # list missing build requirements
make deps    # install them (system packages via sudo, wails via go install)
make build   # → build/bin/session-workbench (version from git describe)
make build-windows   # → build/bin/session-workbench.exe, cross-built
make packages        # → dist/*.deb and dist/*.rpm (installs nfpm if needed)
make run     # build, then start the app
make dev     # live-reload dev mode
make test    # Go unit tests (race detector) + frontend unit tests (node --test);
             # CI runs them on Linux, Windows and macOS (race detector on Linux)
make test-ssh SSH_TARGET=localhost  # ssh/tmux, working-copy, live-share and Claude-backup
             # round trips against a real host (key auth, tmux, rsync, sshfs; same paths on both sides)
make vet     # vet for linux and windows (all packages), darwin (internal packages)
make clean   # remove build output (build/bin, dist, generated package docs)
make smoke   # headless GUI test of terminal fidelity
make smoke-sessions  # sessions/dialogs/switching/restart, add host, start-mode restart,
                     # one instance per config, Claude backups
make smoke-files     # Files tab
make smoke-ui        # themes/colors, switcher, settings/keys, reconnect, find/unicode/copy,
                     # folders, git markers, repository check, working copies, live share,
                     # notes and tab order, Web tab and custom tab order,
                     # layout (font size everywhere, current tab, narrow sidebar), badges (~2 min)
make smoke-all       # all GUI tests; each checks its results and fails on a wrong one
                     # (make -k smoke-all: run the others after a failure)
```

`make test` and `make vet` need the built frontend (`frontend/dist`, embedded in the
binary): run `make build` once on a fresh clone.

The GUI tests run the real app under Xvfb and save screenshots next to their results
(`/tmp/sw-smoke-*`). They need `xvfb xdotool imagemagick tmux rsync sshfs git jq dbus`
(Debian/Ubuntu package names) and `ssh localhost` without a password; they use
throwaway configs, never yours. One test on its own:

```
xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-web.sh build/bin/session-workbench /tmp/sw-smoke-web
```

## Usage

Start `session-workbench`. The first time it shows **Add host…** to set up an agent host:

- **SSH target** — anything `ssh` accepts (`user@host`, a `~/.ssh/config` alias).
- **Local / remote prefix** — where the agent host sees your directories, e.g.
  `~/share-local` ↔ `/net/192.168.122.1/home/mboot/share-local`. Leave both empty
  if the paths are identical on both machines.
- **File access** — shared file system (default), live share over ssh, or working copy;
  see [Without a shared file system](#without-a-shared-file-system).

Then **New session** (Ctrl+Shift+N): pick a directory under the local prefix,
a name (defaults to the directory name), and what a new remote tmux session starts
with (`claude`, `claude --continue`, or a shell). The dialog shows where the directory
appears on the agent host, or why it can't be used. Host and directory can't be changed
later, and a directory can have one session per host. `claude --continue` starts a new
conversation when the directory has none yet. The start mode only applies when the remote
tmux session is created; after changing it in *Edit…* the app offers to restart a running one.

Each session has these tabs. Change their order, or hide some, in Settings → Tabs;
Ctrl+Shift+1… always selects the 1st… visible tab, and hovering a tab shows its shortcut.
Default order:

- **Local** (Ctrl+Shift+1): your shell in the directory (not kept across app restarts).
- **Files** (Ctrl+Shift+2): the directory as a tree, refreshed whenever you
  switch to it. Double-click or Enter opens a file with its default application
  (directories expand); arrows move (Left/Right collapse and expand), Home/End and
  PgUp/PgDn jump, F5 refreshes. Right-click a file to open it or its containing folder,
  a folder to open it in the file manager; both offer copy local / remote path and refresh.
  Dotfiles (including `.git`) are hidden unless "Hidden files" is ticked. Programs and
  files the OS would execute rather than open (native binaries — ELF, PE, Mach-O — and
  per OS `.exe`, `.bat`, `.js`, `.lnk`…, `.app`, `.command`…, `.desktop`, `.run`…, `.jar`)
  are refused. A symlink that leads out of the session directory is marked *↗ outside*
  and not opened or listed (the agent host could have planted it). Very large
  directories show at most 5000 entries.

  In a git repository each entry gets a letter — **M** modified, **A** added,
  **D** deleted, **R** renamed, **U** untracked, **!** conflict — folders with changes
  get a dot (**!** if they contain a conflict), entries inside ignored or untracked
  folders inherit that, ignored files are in italics, and the bar shows the branch with
  ahead/behind (↑/↓). This needs `git` on your computer and only reads (no locks). If the
  repository's own config names programs git status would run (filters, includes,
  fsmonitor) or points git at another directory (`core.worktree`), the status is skipped and
  the bar says so — see
  [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).
- **Remote** (Ctrl+Shift+3): `ssh` → tmux session `cw-<id>` in the mapped
  directory. It survives closing the app and lost connections; Enter reconnects.
  When claude exits you get a shell.
- **Notes** (Ctrl+Shift+4): your notes about the project, saved shortly after you stop
  typing, when you leave the tab and when the window loses focus. They are stored with
  the app's config (`notes/<session id>.md` next to `config.toml`, owner-only, up to 4 MB),
  not in the project, so they stay out of git and off the agent host. Emptying the notes
  removes the file; notes are kept when a session is deleted.
- **Web** (Ctrl+Shift+5): the repository's latest **GitHub Actions runs** (the last 12:
  status, workflow, title, branch, age; refreshed while shown, every 15 s while one is
  running, otherwise every 2 minutes; click to open it in your browser), buttons for the
  repository's pages read from its git remote (`origin`, else the first one), and your own
  links per session (http/https). github.com repositories get Repository, Actions, Pull
  requests, Issues and Releases; GitLab (a host name containing "gitlab") the equivalents;
  other hosts the repository page. The runs need the GitHub CLI (`gh`) installed and
  logged in (`gh auth login`) on your computer; the app stores no tokens. Pages open in
  your normal browser: GitHub cannot be shown inside the app.

A session reopens on the tab you used last; new sessions start on Local. Sessions
look the way Settings says (theme, font, colors); a session can override the colors
in *Edit…* if needed. The font size applies to the whole app (sidebar, tab bar,
dialogs), as does the font family when you set one. Text is black (light theme) or
bright (dark theme), never grey; color only marks status (errors, warnings, git letters,
directory names).

**Repository check.** If a session's repository contains hooks or settings that git
on your computer would run (or that redirect pushes), the session gets a ⚠ and the
Files tab a banner; **Details…** lists each item with why it matters and a command to
inspect or remove it. An agent host that can write the repository could have planted
them. **Mark as reviewed** if they are yours; the warning returns when anything flagged
changes. The check runs at start, when you switch sessions or return to the window, when
the Files tab refreshes, when Details… opens, and every two minutes. Background:
[docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).

Terminals: Ctrl+Shift+F (Cmd+F) opens a find bar (Enter / Shift+Enter for next /
previous); links are clickable; emoji and CJK characters get their proper two-cell
width. With *Copy terminal selection to the clipboard automatically* (Settings,
`copy_on_select`) a selection goes to the clipboard at once.

| Keys (Linux/Windows) | macOS | Action | `[keys]` name |
|---|---|---|---|
| Ctrl+Shift+PgDn / PgUp | ⇧⌘] / ⇧⌘[ | next / previous session | `next_session`, `prev_session` |
| Ctrl+Alt+1…9 | ⌘1…9 | session by number (shown in the sidebar) | `session_1` … `session_9` |
| Ctrl+Shift+1…9 | ⇧⌘1…9 | 1st…9th visible tab | `tab_1` … `tab_9` |
| — | — | one tab wherever it is in the order (unbound) | `tab_local`, `tab_files`, `tab_remote`, `tab_notes`, `tab_web` |
| Ctrl+Shift+P | ⇧⌘P | quick switcher (fuzzy, most recent first) | `quick_switcher` |
| Ctrl+Shift+N | ⇧⌘N | new session | `new_session` |
| Ctrl+Shift+, | ⌘, | settings | `settings` |
| Ctrl+Shift+F | ⌘F | find in terminal | `find` |
| Ctrl+Shift+B | ⇧⌘B | narrow / wide sidebar | `toggle_sidebar` |
| Ctrl+Shift+C / V | ⌘C / V | copy / paste | — |

Rebind in the config's `[keys]` table (see the example below); Settings → Keyboard
shortcuts lists the current bindings. A key is modifiers plus one key: `Ctrl`, `Shift`,
`Alt` (`Option`), `Cmd` (`Meta`, `Super`) and `Mod` (Cmd on macOS, Ctrl elsewhere), then a
letter, digit, `F1`–`F24`, `PageUp`/`PgUp`, `PageDown`/`PgDn`, `Home`, `End`, `Insert`,
`Delete`, `Tab`, `Enter`, `Escape`, `Space`, `Backspace`, `Up`/`Down`/`Left`/`Right` or a
punctuation key (`[ ] , . / ; ' \` - =`). Every binding needs Ctrl, Alt or Cmd (F-keys
excepted): plain keys belong to the terminal. `"none"` disables a shortcut. Keys are
physical positions, so `Ctrl+Alt+1` is the 1 key on any layout. A bad binding is shown in
the sidebar and the default is kept; when two actions share a key, both are named and the
later one in the table is disabled. The config is read at start: edit `[keys]` while the
app is not running, or restart it afterwards.

**Colors.** The default theme is black on white. Programs often pick colors for dark
backgrounds; `full` colors keeps them but enforces a minimum contrast (yellow and
light green on white get darkened), `mono` removes all colors (16/256/truecolor,
foreground and background) and keeps bold, underline and inverse. Set it in
Settings (Ctrl+Shift+,), per session in *Edit…* (Colors), or with *Use mono colors
(no colors)* / *Use full colors* in the session menu; it switches live. Choosing the
Settings value there removes the session's override.

**Badges.** A session you are not looking at gets an orange dot when one of its
terminals rings the bell, and a blue ring when it starts printing again after 10 s of
quiet (tmux's clock redraw does not count). The same happens to the visible session while
the window is in the background. The tab that caused it is marked too; the mark clears
when you look at it, and the window title shows how many sessions want attention. To have
claude ring the bell when it waits for you, run `/config` inside claude on the agent host
and set the notification channel to *terminal bell* (setting `preferredNotifChannel`, value
`terminal_bell`).

The dot before a session shows its Remote connection: green connected, pulsing orange
reconnecting, orange ended, red misconfigured (the reason is in the bar above the tabs),
grey not connected yet.

**Connection drops.** A dropped or failed Remote connection reconnects automatically
(after 1, 2, 4, 8, 16, then every 30 s; back to 1 s after a connection that lasted 30 s;
Esc stops it; off with *Reconnect dropped connections automatically* in Settings).
Detaching tmux, ending the remote session or *Disconnect* does not reconnect; press Enter
when you want to.

**Folders.** Group sessions into folders such as *Work* and *Private*: "+" →
*New folder…*, then put sessions in it from the session dialog, with *Move to
folder…*, or by dragging a session onto the folder header; *Move to folder…* →
*No folder* takes it out again. Right-click a folder (or its ⋯) for *New session in this
folder…*, rename, move up/down or delete (deleting keeps its sessions). Sessions without
a folder are listed first.

Right-click a session (or its ⋯) for *Edit…* (name, folder, start mode, colors),
*Move up*, *Move down*, *Move to folder…*, *Use mono colors* / *Use full colors*,
*Disconnect*, *End remote session…* and *Delete session…* (optionally ending its tmux
session too; its notes are kept). Working-copy sessions also have *Send to agent…* and
*Get commits from agent…*, live-share sessions *Stop sharing*. Drag sessions to reorder
them within their folder and host. Click a folder or host header to collapse it;
right-click a host (or its ⋯) to add a session on it, edit it, back up or restore its
Claude data, or delete it (once no session uses it). Menus also work from the keyboard
(arrows, Home/End, Enter, Esc, first letter).

**Sidebar width.** Drag the sidebar's right edge to make it wider or narrower (or focus the
edge with Tab and use ← / →); double-click the edge for the default width. The width is
remembered and scales with the font size.

**Narrow sidebar.** « in the sidebar header (or Ctrl+Shift+B) shrinks the sidebar to a
rail of session markers: initials, number, status dot and badges; hover for the name
and path. » widens it again (to the width you dragged it to); the choice is remembered. While narrow, *Add host…* and
*Settings…* are hidden: use "+" and Ctrl+Shift+, instead.

### Without a shared file system

There are two options when adding a host (details and trade-offs in
[docs/CONNECTIVITY.md](docs/CONNECTIVITY.md)):

**Live share.** *File access: live share over ssh*: the app serves the session
directory over its own ssh connection and the agent host mounts it with `sshfs` (needs
sshfs + FUSE there, no root, no credentials for your machine). Nothing leaves the
session directory. The share runs while the app runs; the bar shows *● shared*.

**Working copies.** *File access: working copy*. Each session then gets a copy on the agent host;
**⇡ Send** mirrors your directory there (with a preview, never destroying unfetched
agent commits), and **⇣ Get** fetches the commits claude made there into
`agent-<id>/<branch>` and can fast-forward your branch. Until the first Send the Remote
tab says there is no working copy yet and offers *Send to agent…*. Create the session for
the repository's top folder: a subdirectory is sent without `.git`, so commits cannot come
back (Send and Get say so). Needs rsync on both
sides (not available on Windows yet). Details: [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).

**Claude backups.** The app copies each agent host's Claude Code data — `~/.claude`
(settings, CLAUDE.md, memory, conversations, skills, agents, history, checkpoints) and
`~/.claude.json` — to this computer with rsync over ssh. It checks a minute after it
starts and then every minute; a host is backed up when `claude_backup_every` (default an
hour; Settings: on/off and how often) has passed since its last attempt, also across
restarts, and at most every 15 minutes after a failure. Hover a host header to see the
last backup; ⚠ means the last one failed. The host's menu has *Back up Claude data now*,
*Restore Claude data to this host…* (for a new or reinstalled agent host: copies everything
back, keeping files that exist there) and *Open Claude backup folder*. A backup only grows:
files deleted on the agent host stay in it. Left out are claude's login
(`.credentials.json`; log in to claude once after a restore) and caches and downloads
claude recreates. Backups are kept per host *name*: keep the name when the machine behind
it changes. Needs rsync on both sides; without rsync on this computer (Windows) automatic
backups pause and the host tooltip says so. Restoring a crashed agent host, step by step:
[docs/CONNECTIVITY.md](docs/CONNECTIVITY.md#claude-backups).

Agent and X11 forwarding are always forced off on every ssh connection. The
agent host needs `tmux` on the PATH of a login shell; `claude` is started from an
interactive login shell, so it is found on the same PATH as when you log in by hand
(`~/.profile` and `~/.bashrc`).

### Config file

`~/.config/session-workbench/config.toml` (Linux), `%AppData%\session-workbench\config.toml`
(Windows), `~/Library/Application Support/session-workbench/config.toml` (macOS);
override with `-config <file>`. The app reads it at start and rewrites it on every change;
hand edits are fine while it is not running (comments are not kept). Unknown keys are
rejected: if the file cannot be parsed the app shows why and saves nothing, so your file
is never overwritten. A setting with an invalid value gets its default. Hosts and sessions
are checked with the dialogs' rules when the file is read, and problems are listed at the
top of the sidebar; nothing in the file is changed. A session that cannot work (an invalid
host, an unknown start mode, a relative `local_path`, an id used twice) gets a red dot and
the reason above its tabs; for the rest (an unknown color mode, a link that is not http(s),
an empty name, a host or directory defined twice) the app uses the default, skips the value
or ignores the later duplicate.

Only one copy of the app runs per config file: starting it again brings the running
window to the front (where that is not possible, the second window only says so).

```toml
workspace_roots = ["~/share-local/DEV"]   # the first is where "Browse…" starts
groups = ["Work", "Private"]              # sidebar folders, in this order

[ui]                                      # all of these are in Settings too
theme = "light"                           # or "dark"
color_mode = "full"                       # or "mono"
min_contrast = 4.5                        # 1 (off) .. 21
font_family = "JetBrains Mono"            # default: DejaVu Sans Mono / Cascadia Mono / Menlo
font_size = 13                            # 6 .. 48, the whole app follows it
scrollback = 10000                        # lines, 1 .. 1000000
connect_on_start = "lazy"                 # or "eager": connect all Remote tabs at startup
auto_reconnect = true
activity_badges = true
copy_on_select = false
claude_backup = true                      # back up ~/.claude of the agent hosts
claude_backup_every = "1h"                # 5m … 168h
tab_order = ["local", "files", "remote", "notes", "web"]
hidden_tabs = []                          # e.g. ["web"]; at least one tab stays visible

[keys]
next_session = "Ctrl+Tab"
prev_session = "Ctrl+Shift+Tab"
quick_switcher = "Mod+K"                  # Cmd+K on macOS, Ctrl+K elsewhere
tab_remote = "Ctrl+Alt+R"                 # the Remote tab wherever it is in the order
find = "none"                             # disabled

[local]
shell = "/bin/zsh"                        # Local tab shell; default $SHELL -l (Windows: pwsh → powershell → cmd)
args = ["-l"]                             # used as given: "-l" makes it a login shell

[[hosts]]                                 # a shared file system (NFS, SMB, VM share)
name = "vibe"
ssh_target = "mboot@vibe-u26"
local_prefix = "~/share-local"
remote_prefix = "/net/192.168.122.1/home/mboot/share-local"

[[hosts]]                                 # no shared file system: working copies
name = "lab"
ssh_target = "me@lab-box"
files = "sync"                            # "sshfs" = live share; default shared
remote_workdir = "~/sw"                   # copies at ~/sw/<dir>-<session id> (no prefixes with sync/sshfs)

[[hosts]]                                 # no shared file system: live share
name = "cloud"
ssh_target = "me@cloud-box"
files = "sshfs"                           # mounted at ~/sw/<dir>-<session id> while the app runs

[[sessions]]                              # sidebar order = order in this file
id = "s-7f3a"                             # tmux session cw-s-7f3a; [a-z0-9-], at most 63 characters
name = "workbench"
host = "vibe"
local_path = "~/share-local/DEV/github.com/maarten-boot/multi-session-ssh-dev"
start_mode = "claude-continue"            # or "claude" or "shell"
group = "Work"                            # optional: sidebar folder (listed in groups)
color_mode = "mono"                       # optional: overrides [ui] color_mode
local_init = "git status"                 # optional: typed into the Local tab
links = [{ name = "Staging", url = "https://staging.example.com" }]   # optional: Web tab
env = { GIT_PAGER = "less -R" }           # optional: Local tab environment
# trusted_git = "…"                       # written by "Mark as reviewed"; don't set by hand
```

### Where the app keeps things

| What | Linux | macOS | Windows |
|---|---|---|---|
| config (`config.toml`, its `.lock`, `notes/`) | `~/.config/session-workbench/` | `~/Library/Application Support/session-workbench/` | `%AppData%\session-workbench\` |
| Claude backups (`claude-backups/<host>/`) | `~/.local/share/session-workbench/` | `~/Library/Application Support/session-workbench/` | `%LocalAppData%\session-workbench\` |
| window state | `~/.local/share/session-workbench/` | WebKit's default store | `%AppData%\session-workbench.exe\` |

`$XDG_CONFIG_HOME` / `$XDG_DATA_HOME` move them on Linux; the lock and `notes/` follow
`-config`. Window state is what this computer remembers outside `config.toml`: the active
session, the last tab per session, collapsed headers, the sidebar width, the narrow sidebar, the switcher's
recent order and the last host and start mode used for a new session. Nothing is stored in
your projects; on the agent hosts only the tmux sessions and, for working copies and live
share, the directories under `remote_workdir` (deleting a session leaves them).

## Layout

```
main.go, app.go        Wails entry point and frontend bindings
menu.go, proc_*.go     macOS menu bar; hiding console windows on Windows
internal/terminal      pty processes + batched output events
internal/pathmap       host path -> agent-host path
internal/remote        ssh/tmux command lines (quoting, forced options)
internal/config        TOML config load / atomic save, data directory
internal/session       hosts + sessions: validation, mapping, ordering
internal/notes         per-session notes files
internal/forge         Web tab: links from the git remote, GitHub runs via gh
internal/fsbrowse      directory listing for the Files tab (confined to the session directory)
internal/opener        open / reveal with the OS, refusing executables
internal/gitstatus     git status markers and the repository check for the Files tab
internal/syncer        working copies: rsync send, git fetch back
internal/share         live share: confined SFTP server + sshfs mount manager
internal/backup        Claude backups: ~/.claude of the agent hosts, rsync, restore
internal/instance      one instance per config file (lock)
frontend/src           vanilla JS UI: main.js (app), terminal.js, files.js, notes.js, web.js,
                       ui.js, appearance.js (themes, mono filter), keys.js, fuzzy.js,
                       style.css; *.test.mjs are the node --test unit tests
build/                 app icon, Windows/macOS resources for Wails
packaging/             .desktop file, icons, man page, copyright, nfpm config for .deb/.rpm,
                       gen-docs.sh (changelog and man page per version)
docs/                  CONNECTIVITY.md: agent host setup, file access modes, security
scripts/deps.sh        make check / make deps
scripts/gui-smoke*.sh  headless GUI tests; scripts/smoke-lib.sh holds their shared checks
.github/workflows      CI: test + build on Linux/Windows/macOS, release on v* tags
```
