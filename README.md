# Session Workbench

Desktop app for running many Claude Code sessions side by side. Each session
is bound to a repository directory and has a **Local** terminal (host shell for
git), a **Files** tab, a **Remote** terminal (ssh → tmux → claude on the isolated
agent host), **Notes** and **Web** (build runs and links). See [SPEC.md](SPEC.md) and [PLAN.md](PLAN.md).

**Setting up the agent host and sharing files with it — and what that means for
security — is described in [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).** Please
read its security section if the agent host can write to your repositories.

Status: v0.8.42: sessions with Local, Files (sizes, dates, sort by name or date), Remote,
Notes (page trees with formatting, lists and check lists, spelling) and Web tabs, Remote tab recordings, session folders,
themes and color modes, quick switcher, a command menu and a prompts menu in the Local and Remote tabs, editable
start commands, private shell history per session, activity badges, desktop notifications,
auto-reconnect, live share and working copies for hosts without a shared file system (Get
lists and takes over every agent branch; full save, take, send and restore, also without git),
Claude backups, remembered window size and position, settings in pages; builds for Linux, Windows
and macOS.

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

On your computer the app uses `ssh`, and optionally `git` (Files tab markers, working
copies), `rsync` (faster Claude backups and working copies; without it the app transfers
files itself) and `gh` (Web tab runs). The agent host needs `tmux`, `claude`, `rsync` or
`tar` (standard), and `git` for working copies; nothing is installed there by the app (it only keeps
the sessions' shell history in `~/.local/state/session-workbench/history/`; requirements and how
to check them: [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md#requirements-on-the-agent-host)).
`session-workbench -version` prints the version; it is also shown in Settings.

To release: tag and push (`git tag -s vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z`, a numeric
version: it also becomes the Windows file version); CI builds all three
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
             # round trips against a real host (key auth, tmux, sshfs, git, tar there; rsync optional;
             # same paths on both sides), working copies and backups also with the built-in
             # transfer (SW_NO_RSYNC=1)
make vet     # vet for linux and windows (all packages), darwin (internal packages)
make clean   # remove build output (build/bin, frontend/dist/assets, dist, generated package docs)
make smoke   # headless GUI test of terminal fidelity
make smoke-sessions  # sessions/dialogs/switching/restart, add host, start-mode restart,
                     # one instance per config, remembered window size and position, Claude backups
make smoke-files     # Files tab
make smoke-ui        # themes/colors, switcher, settings/keys, reconnect, find/unicode/copy,
                     # folders, git markers, repository check, working copies, live share,
                     # notes and tab order, Web tab and custom tab order,
                     # layout (font size everywhere, current tab, narrow sidebar),
                     # desktop notifications, notes for all sessions and copy/paste,
                     # Remote tab recordings, command menu, prompts menu and start
                     # commands, private shell history, lists and spelling in the notes,
                     # badges (~2 min)
make smoke-all       # all GUI tests; each checks its results and fails on a wrong one
                     # (make -k smoke-all: run the others after a failure)
```

`make test` and `make vet` need the built frontend (`frontend/dist`, embedded in the
binary): run `make build` once on a fresh clone.

The GUI tests run the real app under Xvfb and save screenshots next to their results
(`/tmp/sw-smoke-*`). They need (Debian/Ubuntu package names) `xvfb xdotool imagemagick tmux
sshfs git jq dbus python3-dbus python3-gi libglib2.0-bin` (for gdbus), `ssh localhost`
without a password, and bash as your login shell (the history test reads bash's history file). rsync is optional: without it those runs use the built-in transfer, and
the working-copy and backup tests run once more with `SW_NO_RSYNC=1` anyway. They use
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
with: one of the start commands of Settings → Commands (out of the box `claude`,
`claude-continue` = `claude --continue || claude`, and `shell`; add your own, such as
`claude --model opus`). The dialog shows where the directory
appears on the agent host, or why it can't be used. Host and directory can't be changed
later, and a directory can have one session per host. `claude --continue` starts a new
conversation when the directory has none yet. The start mode only applies when the remote
tmux session is created; after changing it in *Edit…* the app offers to restart a running one.

Each session has these tabs. Change their order, or hide some, in Settings → Appearance;
Ctrl+Shift+1… always selects the 1st… visible tab, and hovering a tab shows its shortcut.
Default order:

- **Local** (Ctrl+Shift+1): your shell in the directory (not kept across app restarts).
- **Files** (Ctrl+Shift+2): the directory as a tree with each file's size and each
  entry's last modification time (local time, `2026-10-18 09:30`; a symlink inside the session
  directory shows its target's), refreshed whenever you switch to it. The menu in the bar sorts by *Name*,
  *Newest first* or *Oldest first* (folders stay on top; remembered per session). Double-click or Enter opens a file with its default application
  (directories expand); arrows move (Left/Right collapse and expand), Home/End and
  PgUp/PgDn jump, F5 refreshes. Right-click a file to open it or its containing folder,
  a folder to open it in the file manager; both offer copy local / remote path and refresh.
  Dotfiles (including `.git`) are hidden unless "Hidden files" is ticked. Programs and
  files the OS would execute rather than open (native binaries — ELF, PE, Mach-O — and
  per OS `.exe`, `.bat`, `.js`, `.lnk`…, `.app`, `.command`…, `.desktop`, `.run`…, `.jar`)
  are refused, also through a symlink (a link `docs` → `Evil.app` counts as the app), and so are
  FIFOs and other special files. A symlink that leads out of the session directory is shown,
  marked *↗ outside*, but not followed: not expanded or opened (the agent host could have planted
  it). Very large
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
- **Notes** (Ctrl+Shift+4): your notes about the project, as pages in a tree like the
  Zim desktop wiki: the tree on the left, the open page on the right. Every session starts
  with a *Home* page. **+** (or a right-click on empty space in the tree) makes a *New page…*;
  right-click a page for *New page…*, *New subpage…*, *Rename or move…* (subpages move along;
  `Design/API` puts a page under *Design*) and *Delete…* (with its subpages, after asking).
  In the tree ↑/↓ open the next or previous page, →/← unfold and fold (← on a folded page
  goes to its parent), Enter goes to the page. Page names: up to 100 characters per level
  and 10 levels deep; not `/ \ : * ? " < > |`, no dot at the start, no space at either end.
  The tab opens on the page you had open, else *Home*.
  Pages are Markdown, shown with their formatting while the markers stay visible
  (in italics): headings bold (`#` and `##` larger), `**bold**` bold, `_italic_` italic, `~~struck~~` struck
  through and `<u>underlined</u>` underlined (Markdown has no underline of its own; GitHub
  shows `<u>`). The **B I U S** buttons above the page, or Ctrl+B, Ctrl+I, Ctrl+U and
  Ctrl+Shift+X (⌘ on macOS), add or remove those markers around the selection (each line
  of a longer selection separately).
  **Lists and check lists**: the **•** and **☐** buttons, or Ctrl+Shift+8 and Ctrl+Shift+9
  (⌘⇧8 and ⌘⇧9 on macOS),
  make the selected lines (or the cursor's line) `- item` or `- [ ] item`, and plain lines again
  when they already are. A check list item shows a check box: click it, or press Ctrl+Enter
  on its line (⌘ on macOS; with several lines selected: all ticked, or all unticked when they
  were), and the file says `- [x] item`, as on GitHub. Enter in a list item starts the next one
  (`-`, `*`, `+`, numbers counting up; a new check box is empty; not inside a code block);
  Enter in an empty item outdents it, then ends the list. Tab and Shift+Tab indent and outdent a list item; elsewhere
  Tab inserts two spaces (with a selection: indents its lines) and Enter starts a plain new line.
  **Spelling**: misspelled words are underlined in red (English by default); right-click one
  for corrections and *Add “word” to dictionary*; right-click a word you added for *Remove
  “word” from dictionary*. Not checked: code (`` `x` `` and code
  blocks), links' addresses, HTML tags, CamelCase, ACRONYMS, file names, paths and words with
  digits; the word you are typing waits until you leave it. The tools this app is about
  (claude, tmux, ssh, sshfs, rsync, git, GitHub…) are known words in every language. Settings → Notes turns it off,
  picks the language and lists your added words (`spell_check`, `spell_language`,
  `spell_words` in the config). English (en_US) is built in; for another language put a
  Hunspell dictionary (`nl.aff` and `nl.dic`, say; the LibreOffice and Firefox ones work) in
  `dictionaries/` next to `config.toml`; on Linux installed `hunspell-*` / `myspell-*`
  packages are found too, and on macOS `~/Library/Spelling`. A page with lists, as the
  file stores it (the boxes show as check boxes):

  ```
  ## Before the release
  - [x] run the tests
  - [ ] update the changelog
    - [ ] mention the new Prompts menu
  1. tag
  2. push
  ```

  A Dutch dictionary, for example: copy `nl.aff` and `nl.dic` (from a LibreOffice
  extension, or `/usr/share/hunspell` on a Linux machine) to
  `~/.config/session-workbench/dictionaries/` (`%AppData%\session-workbench\dictionaries\`
  on Windows, `~/Library/Application Support/session-workbench/dictionaries/` on macOS), then pick `nl` in Settings → Notes. If a chosen dictionary goes
  missing, a message says spelling is off.
  Ctrl+C / Ctrl+X / Ctrl+V
  (⌘ on macOS) copy, cut and paste; right-click the page for *Cut*, *Copy*, *Paste*,
  *Select all*, the four formats, the two lists and *Tick / untick* (a right-click outside
  the selection moves the cursor there first). Both note panels work the same.
  A page is saved shortly after you stop typing, when you open another page or leave the
  tab, and when the window loses focus. Pages are stored with the app's config, one file
  per page: `notes/<session id>/Home.md`, `notes/<session id>/Design.md`,
  `notes/<session id>/Design/API.md` next to `config.toml` (owner-only, up to 4 MB each),
  not in the project, so they stay out of git and off the agent host. You can edit them
  with any editor; the app reloads a page changed outside it when you come back to the tab
  (unless you have unsaved edits there). Emptying a page removes its
  file; notes are kept when a session is deleted. Notes from before pages existed
  (`notes/<session id>.md`) become the session's *Home* page (unless it exists already;
  then the old file is left alone).

- **Web** (Ctrl+Shift+5): the repository's latest **GitHub Actions runs** (the last 12:
  status, workflow, title, branch, age; refreshed while shown, every 15 s while one is
  running, otherwise every 2 minutes; click to open it in your browser), buttons for the
  repository's pages read from its git remote (`origin`, else the first one), and your own
  links per session (http/https). github.com repositories get Repository, Actions, Pull
  requests, Issues and Releases; GitLab (a host name containing "gitlab") the equivalents;
  other hosts the repository page. The runs need the GitHub CLI (`gh`) installed and
  logged in (`gh auth login`) on your computer; the app stores no tokens. Pages open in
  your normal browser: GitHub cannot be shown inside the app.

**Notes for all sessions.** ✎ in the sidebar header (or Ctrl+Shift+M) opens a notes page
for the whole app, for prompts you use often and notes that belong to no session: the same
page tree and editor, stored under `notes/_app/` next to `config.toml`. Prompts live on the
page named *Prompts* and its subpages (the notes from before pages existed, `notes/_app.md`,
became that page; without one, the prompts list offers to create it). Every `## name`
heading there starts a prompt: the page lists them on the right with *Copy* and *Paste into
Remote*, which puts the prompt into the current
session's Remote tab as pasted text — claude runs it when you press Enter. **Prompts ▾** next to
the tabs (Local and Remote), and the terminal's right-click menu after its commands, paste one
into the tab you are in: first the session's own prompts
(a page named *Prompts* and its subpages in the session's Notes, same `## name` headings), then
those for all sessions. It waits until claude
(or the shell) accepts a *bracketed* paste; a multi-line prompt is not pasted before that, so
its lines cannot run one by one. A prompt runs to the next `#` or `##` heading (`###` and deeper
stay in it, and so do headings inside ```` ``` ```` or `~~~` code blocks); a heading starts at the
beginning of the line with a space after the `#`s (`  ## x` and `##x` are text); prompts without
text are skipped. For example (the arrows are comments here, not part of the page):

````
## Review
Review the changes since the last commit for bugs and missing tests. Be brief.

## Fix tests
Run the tests and fix what fails.
### Details      ← part of "Fix tests"

## Clean build
```sh
# not a heading: inside a code block, part of "Clean build"
make clean all
```

# Other notes    ← not a prompt; ends the one above
````

Esc, ✕, Ctrl+Shift+M or picking a session closes the page.

A session's own *Prompts* page (in its Notes tab; the first entries of its Prompts ▾), for example:

```
## Tests
Run `make test` and fix what fails; do not change the tests.

## Status
Summarise what you changed in this session in five lines.
```

A session reopens on the tab you used last; new sessions start on Local. Sessions
look the way Settings → Appearance says (theme, font, colors); a session can override the colors
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
width. Copy and paste: Ctrl+C copies when text is selected and otherwise interrupts the program
as usual, Ctrl+V pastes (*Ctrl+C copies selected text…* in Settings → Terminals, `ctrl_copy_paste`; switch it
off if you need Ctrl+V in vim — Ctrl+Q works there too); Ctrl+Shift+C / V always work, Cmd+C / V
on macOS. Right-click a terminal for Copy, Paste, Select all, Find… and Clear scrollback. With
*Copy terminal selection to the clipboard automatically* (Settings → Terminals, `copy_on_select`) a selection
goes to the clipboard at once.

**Command menu.** Commands you type often (`git fetch --all`, `make test`) are one click away
in the Local and Remote tabs: at the bottom of the right-click menu, and under **Commands ▾**
next to the tabs. Choosing one types it into the terminal and presses Enter (*run*), or only
types it, for a command you want to check or finish first (shown with *…* instead of *⏎*).
The list is in Settings → Commands; each session can add its own in *Edit…* → Commands, shown
first. In the config: `[[ui.commands]]` with `name`, `command` and `run` (default true), and
`commands = [{ name = …, command = …, run = false }]` on a session.

**Start commands.** What a session's Remote tab starts in a new tmux session is a list too
(Settings → Commands → Start commands; `[[ui.start_commands]]` with `name` and `command`):
the command runs in the agent host's interactive login shell, a login shell takes over when it
ends — also after Ctrl+C or a typo in the command — and an empty command is just the login
shell. A session names one as its `start_mode`. A list in the config replaces the default three
(`claude`, `claude-continue`, `shell`), so keep the ones your sessions use; removing or renaming
one that a session still uses stops that session from starting (the sidebar says why) until you
pick another in *Edit…*. A changed command applies to tmux sessions started afterwards (*End
remote session…* restarts one). A start command is one line and cannot start with `-`; with
zsh as the login shell, leave out `#` comments (zsh does not treat them as comments there).

**Shell history per session.** Each session's Local and Remote shells keep their own command
history, so ↑ and Ctrl+R show that project's commands, not everything from all your shells:
the app sets `HISTFILE` (bash, zsh) to `history/<session id>.history` in its data folder
(`~/.local/share/session-workbench/` on Linux, `~/Library/Application Support/session-workbench/`
on macOS) for the Local tab, and to `~/.local/state/session-workbench/history/<session id>` on
the agent host for the Remote tab (also for new tmux windows); both folders are owner-only.
fish gets `fish_history=session_<id>` with `-` as `_` (so `~/.local/share/fish/session_s_1a2b_history`).
Off: Settings → Terminals (`private_history = false`), or per session in *Edit…* → Shell history
(`history = "off"` / `"on"`). Notes:

- bash writes history when the shell exits; closing a tab, ending a session or quitting the
  app hangs up the shell first (SIGHUP), so that counts. To save after every command, put
  `PROMPT_COMMAND="history -a${PROMPT_COMMAND:+; $PROMPT_COMMAND}"` in `~/.bashrc`.
- zsh saves only with `SAVEHIST` above 0 (set it in `~/.zshrc`).
- A `HISTFILE` set in your startup files (`~/.bashrc`, `~/.zshrc`, macOS's `/etc/zshrc`) wins
  over the app's. Check with `echo "$HISTFILE"` in the tab.
- On the agent host it applies to tmux sessions started afterwards (*End remote session…*
  restarts one).
- On Windows the Local tab keeps each shell's own history (PowerShell and cmd take no history
  file from the environment); the Remote tab still gets its own (the setting's tooltip says so).
- History files may hold secrets you typed on a command line; they are as private as your
  shell's usual history.

**Settings** (Ctrl+Shift+, or *Settings…*) has six pages: Appearance (theme, colors, fonts,
tab order), Terminals, Commands, Backups and recordings, Notes (spelling) and Keys (the current shortcuts).

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
| Ctrl+Shift+M | ⇧⌘M | notes and prompts for all sessions | `app_notes` |
| Ctrl+C (with a selection) / Ctrl+V, Ctrl+Shift+C / V | ⌘C / V | copy / paste | — (`ctrl_copy_paste`) |

Rebind in the config's `[keys]` table (see the example below); Settings → Keys lists the
current bindings. A key is modifiers plus one key: `Ctrl`, `Shift`,
`Alt` (`Option`), `Cmd` (`Meta`, `Super`) and `Mod` (Cmd on macOS, Ctrl elsewhere), then a
letter, digit, `F1`–`F24`, `PageUp`/`PgUp`, `PageDown`/`PgDn`, `Home`, `End`, `Insert`,
`Delete`, `Tab`, `Enter`, `Escape`, `Space`, `Backspace`, `Up`/`Down`/`Left`/`Right` or a
punctuation key (`` [ ] , . / ; ' ` \ - = ``). Every binding needs Ctrl, Alt or Cmd (F-keys
excepted): plain keys belong to the terminal. `"none"` disables a shortcut. Keys are
physical positions, so `Ctrl+Alt+1` is the 1 key on any layout. A bad binding is shown in
the sidebar and the default is kept. When your binding uses a key another action has by
default, yours wins and the other action is unbound (both are named in the sidebar); when two
of your bindings collide (or two defaults), the first in this order keeps the key: next/prev session, the tabs by name, new session, quick switcher, settings, find, narrow sidebar, notes for all sessions, session 1–9, tab 1–9. Binding Ctrl+C,
Ctrl+V, Ctrl+Shift+C or Ctrl+Shift+V is allowed but warned about: the terminal then no longer gets
that copy, paste or interrupt key. The config is read at start: edit `[keys]` while the
app is not running, or restart it afterwards.

**Colors.** The default theme is black on white. Programs often pick colors for dark
backgrounds; `full` colors keeps them but enforces a minimum contrast (yellow and
light green on white get darkened), `mono` removes all colors (16/256/truecolor,
foreground and background) and keeps bold, underline and inverse. Set it in
Settings → Appearance (Ctrl+Shift+,), per session in *Edit…* (Colors), or with *Use mono colors
(no colors)* / *Use full colors* in the session menu; it switches live. Choosing the
Settings value there removes the session's override.

**Badges.** A session you are not looking at gets an orange dot when one of its
terminals rings the bell, and a blue ring when it starts printing again after 10 s of
quiet (tmux's clock redraw does not count). The same happens to the visible session while
the window is in the background. The tab that caused it is marked too; the mark clears
when you look at it, and the window title shows how many sessions want attention. A bell in
a session you are not looking at (or while the window is in the background) also shows a
desktop notification — *Claude is waiting* for the Remote tab — at most once a minute per
session (`<name> · <host>`; "Bell in Local" etc. for other tabs); clicking it opens that session
on the tab that rang, and looking at the session withdraws it (Settings → Terminals:
*Desktop notification when claude waits*; Linux uses the desktop's notification service,
macOS may refuse notifications for the unsigned app). To have claude ring the bell when it
waits for you, run `/config` inside claude on the agent host
and set the notification channel to *terminal bell* (setting `preferredNotifChannel`, value
`terminal_bell`).

The dot before a session shows its Remote connection: green connected, pulsing orange
reconnecting, orange ended, red misconfigured (the reason is in the bar above the tabs),
grey not connected yet.

**Connection drops.** A dropped or failed Remote connection reconnects automatically
(after 1, 2, 4, 8, 16, then every 30 s; back to 1 s after a connection that lasted 30 s;
Esc stops it; off with *Reconnect dropped connections automatically* in Settings → Terminals).
Detaching tmux, ending the remote session or *Disconnect* does not reconnect; press Enter
when you want to.

**Folders.** Group sessions into folders such as *Work* and *Private*: "+" →
*New folder…*, then put sessions in it from the session dialog, with *Move to
folder…*, or by dragging a session onto the folder header; *Move to folder…* →
*No folder* takes it out again. Right-click a folder (or its ⋯) for *New session in this
folder…*, rename, move up/down or delete (deleting keeps its sessions). Sessions without
a folder are listed first.

Right-click a session (or its ⋯) for its menu. The top line names how the agent host gets at
the files (*Files: shared folder*, *live share (sshfs), mounted* / *not mounted*, or
*working copy (sync)*; hover it for a short explanation; not shown for a session that cannot
work); then *Edit…* (name, folder, start mode, colors, own commands, recording, shell history),
*Move up*, *Move down*, *Move to folder…*, *Use mono colors (no colors)* / *Use full colors*,
*Record the Remote tab* / *Stop recording the Remote tab*, *Open recordings folder*,
*Disconnect*, *End remote session…* and *Delete session…* (optionally ending its tmux
session too, and for a working copy deleting its saved agent copy; its notes are kept). Working-copy sessions also have *Send to agent…*,
*Get from agent…* and the full transfers (see *Working copies*), live-share sessions *Stop sharing*. Drag sessions to reorder
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
`agent-<id>/<branch>` and lists every agent branch (up to 200) with the commits and files it
brings: one the agent made (*new on the agent*) and one you can fast-forward (*the agent has N
more commits*) are ticked; *Take over ticked branches* creates the new ones here and
fast-forwards the others (you stay on your branch). Where both sides have commits it shows the
`git checkout … && git merge …` to run; a branch name with characters a shell would act on is
never taken over (an example: [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md#c-working-copy--sync-implemented)). Until the first Send the Remote
tab says there is no working copy yet and offers *Send to agent…*. Create the session for
the repository's top folder: a subdirectory is sent without `.git`, so commits cannot come
back (Send and Get say so). Uses rsync where it is installed; without it (Windows) a
built-in transfer over the same ssh connection (tar on the agent host) gives the same
result. Details: [docs/CONNECTIVITY.md](docs/CONNECTIVITY.md).

**Full transfers** (working copies; **Full ▾** next to the tabs, and the session menu), for the
end of a work cycle, for experiments without git, and for an agent host that goes away:

- *Save agent copy (everything, with .git)…* copies the whole agent copy — committed or not,
  untracked, the files `.gitignore` ignores, and `.git` — to
  `working-copies/<session id>/files/` in the data folder (see *Where the app keeps things*),
  replacing the last saved copy (files gone on the agent host go there too). Your session
  directory is not touched. *Open saved copy folder* shows it.
- *Take all files from the agent…* copies the agent copy's files into your session directory,
  with a preview: new and changed files, never `.git` (at any depth). Tick *Also delete files
  here that the agent copy does not have* for an exact copy. Files with uncommitted changes
  here are only overwritten or deleted after you tick a box. Unlike Get these files arrive unreviewed:
  check them before running anything (a `Makefile`, scripts, `.envrc`, editor settings).
- *Send all (ignored files too)…* is Send including the files `.gitignore` ignores (build
  output, `.env`…), also when Send would refuse; the agent copy becomes identical, ignored
  files there are deleted too. Unfetched commits or uncommitted changes there need a tick.
- *Restore saved copy to the agent…* sends the saved copy (with its `.git`) to the session's
  agent copy: after the agent host went down, point the host at a new machine (*Edit host…*),
  then restore and carry on where the agent was.

A saved copy stays when its session is deleted, unless you tick *Also delete its saved agent
copy* in the delete dialog.

**Without git.** For a session directory that is not a git repository (a quick experiment)
there are no commits to fetch: **⇣ Get** then takes the agent copy's files, with *Also delete
files here…* ticked, so you get a full copy of the other side. Send works as usual.

**Claude backups.** The app copies each agent host's Claude Code data — `~/.claude`
(settings, CLAUDE.md, memory, conversations, skills, agents, history, checkpoints) and
`~/.claude.json` — to this computer with rsync over ssh. It checks a minute after it
starts and then every minute; a host is backed up when `claude_backup_every` (default an
hour; Settings → Backups and recordings: on/off and how often) has passed since its last attempt, also across
restarts, and at most every 15 minutes after a failure. Hover a host header to see the
last backup; ⚠ means the last one failed. The host's menu has *Back up Claude data now*,
*Restore Claude data to this host…* (for a new or reinstalled agent host: copies everything
back, keeping files that exist there) and *Open Claude backup folder*. A backup only grows:
files deleted on the agent host stay in it. Left out are claude's login
(`.credentials.json`; log in to claude once after a restore) and caches and downloads
claude recreates. Backups are kept per host *name*: keep the name when the machine behind
it changes. Uses rsync where it is installed (only changes are copied); without it (Windows)
a built-in transfer over the same ssh connection copies everything each time. Restoring a crashed agent host, step by step:
[docs/CONNECTIVITY.md](docs/CONNECTIVITY.md#claude-backups).

**Recording the Remote tab.** Off by default. Settings → Backups and recordings → *Record the Remote tabs of all
sessions* records every session from that moment; a session's own setting (*Edit…* →
*Record the Remote tab*: default / on / off, or the session menu's *Record the Remote tab* /
*Stop recording the Remote tab*) overrides it. While a tab is recorded, the session bar
shows a red **● recording**. A recording is readable text, not a byte dump: colors and
cursor codes are removed and a progress bar ends as its last state; what you type is
written as a timestamped `>` line, and what you type after a password prompt (a line
containing password, passphrase, passcode, PIN, token or secret and ending in `:` or `?`,
also when tmux redraws its status bar after it) as `[hidden]`. The remote side's echo
setting cannot be seen through ssh and tmux, so other secrets you type are recorded, and so
is anything the remote side echoes. Full-screen programs (claude's own screen, vim) come out
as fragments of their redraws, and tmux's status line and screen redraws (blank lines when
you attach) appear too.

```
# workbench (vibe: /home/me/proj) — recording from 2026-10-19 11:30:02
2026-10-19 11:30:05 > git status
me@vibe:~/proj$ git status
On branch main
nothing to commit, working tree clean
2026-10-19 11:30:20 > [hidden]
[sudo] password for me:
# connection ended at 2026-10-19 11:41:07
# workbench (vibe: /home/me/proj) — recording resumed 2026-10-19 11:42:10
…
# continued in 20261019-1200.log
```

A reconnect within the segment writes the *resumed* line into the same file; at the segment's
end the file says where it continues.

Files start every hour by default (Settings → Backups and recordings: 15 minutes to a day; `record_every` in the
config: 5m to 24h), at clock times counted from local midnight (6h gives 00:00, 06:00, 12:00
and 18:00, also on a DST day). The first file is named after when recording starts, later ones
after their segment: `logs/<host>/<session path>/20261019-1130.log`, then `…-1200.log`, in the
data folder (`~/.local/share/session-workbench/` on Linux), or in the folder set there.
A new file only starts when something is recorded. A reconnect within the same segment
continues its file (while the app runs); after a restart, or switching off and on, a new file
starts. Switched on while the Remote tab is not connected, recording starts (and the marker
shows) when it connects. Changing the folder or the segment length starts a new file. A file
that cannot be written is reported once and tried again every minute. Files are owner-only;
nothing removes old ones. The session menu's *Open recordings folder* opens a session's
folder.

Agent and X11 forwarding are always forced off on every ssh connection. The
agent host needs `tmux` on the PATH of a login shell; the start command (`claude` by default)
runs in an interactive login shell, so it is found on the same PATH as when you log in by hand
(`~/.profile` and `~/.bashrc`).

### Config file

`~/.config/session-workbench/config.toml` (Linux), `%AppData%\session-workbench\config.toml`
(Windows), `~/Library/Application Support/session-workbench/config.toml` (macOS);
override with `-config <file>`. The app reads it at start and rewrites it on every change;
hand edits are fine while it is not running (comments are not kept). Unknown keys are
rejected: if the file cannot be parsed the app shows why and saves nothing, so your file
is never overwritten; a value of the wrong type (`font_size = 13.5`, `auto_reconnect = "yes"`)
makes the file unreadable too. A setting with an invalid value, or a number out of its range,
gets its default. Write Windows paths in single quotes (`local_prefix = 'C:\Users\me\share'`):
in double quotes a backslash starts an escape. Hosts and sessions
are checked with the dialogs' rules when the file is read, and problems are listed at the
top of the sidebar; nothing in the file is changed. A session that cannot work (an invalid
host, an unknown start mode, a relative `local_path`, an id used twice) gets a red dot and
the reason above its tabs; for the rest (an unknown color mode, `record` or `history` value, a link that
is not http(s), a host defined twice, a folder not listed in `groups`) the app uses the
default, skips the value, ignores the second host or shows the folder as *(missing)*; an empty
name, a directory with two sessions on one host and a folder listed twice or empty in `groups`
are only reported. An invalid `[[ui.commands]]` or `[[ui.start_commands]]` list (a name used
twice, a command of more than one line…) is reported and kept; the menu leaves out bad entries
and Settings won't save until they are fixed. A session that cannot work does not
reconnect its Remote tab by itself; the tab shows the reason.

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
spell_check = true                        # underline misspelled words in the notes
spell_language = "en_US"                  # built in; others: Hunspell files in dictionaries/
spell_words = ["kubectl", "Zim"]          # your words (Add to dictionary)
ctrl_copy_paste = true                    # Ctrl+C copies a selection, Ctrl+V pastes (Linux/Windows)
notifications = true                      # desktop notification when claude waits (bell)
private_history = true                    # each session's shells keep their own history
claude_backup = true                      # back up ~/.claude of the agent hosts
claude_backup_every = "1h"                # 5m … 168h
record_remote = false                     # record every session's Remote tab (as text)
record_every = "1h"                       # a new recording file every 5m … 24h
record_dir = "~/sw-logs"                  # optional: default <data dir>/logs
tab_order = ["local", "files", "remote", "notes", "web"]
hidden_tabs = []                          # e.g. ["web"]; at least one tab stays visible

[[ui.commands]]                           # the command menu (Settings → Commands)
name = "fetch all"
command = "git fetch --all"               # typed into the terminal, then Enter
[[ui.commands]]
name = "rebase on main"
command = "git rebase origin/main"
run = false                               # only typed: check it, then press Enter yourself

[[ui.start_commands]]                     # what start_mode can name; a list here replaces the default
                                          # three below, so keep the ones your sessions use
name = "claude"
command = "claude"
[[ui.start_commands]]
name = "claude-continue"
command = "claude --continue || claude"   # a new conversation when there is none yet
[[ui.start_commands]]
name = "shell"
command = ""                              # empty: just the login shell
[[ui.start_commands]]
name = "opus"
command = "claude --model opus --continue || claude --model opus"

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
id = "s-7f3a"                             # tmux session cw-s-7f3a; [a-z0-9-], not starting with -, at most 63 characters
name = "workbench"
host = "vibe"
local_path = "~/share-local/DEV/github.com/maarten-boot/multi-session-ssh-dev"
start_mode = "claude-continue"            # the name of one of [[ui.start_commands]]
group = "Work"                            # optional: sidebar folder (listed in groups)
color_mode = "mono"                       # optional: overrides [ui] color_mode
record = "on"                             # optional: "on" / "off" overrides [ui] record_remote
history = "off"                           # optional: "on" / "off" overrides [ui] private_history
local_init = "git status"                 # optional: typed into the Local tab
links = [{ name = "Staging", url = "https://staging.example.com" }]   # optional: Web tab
commands = [{ name = "test", command = "make test" }]                 # optional: before [[ui.commands]] in the menu
env = { GIT_PAGER = "less -R" }           # optional: Local tab environment
# trusted_git = "…"                       # written by "Mark as reviewed"; don't set by hand
```

### Where the app keeps things

| What | Linux | macOS | Windows |
|---|---|---|---|
| config (`config.toml`, its `.lock`, `notes/`, `dictionaries/`) | `~/.config/session-workbench/` | `~/Library/Application Support/session-workbench/` | `%AppData%\session-workbench\` |
| Remote tab recordings (`logs/<host>/<session path>/`) | `~/.local/share/session-workbench/` | `~/Library/Application Support/session-workbench/` | `%LocalAppData%\session-workbench\` |
| Claude backups (`claude-backups/<host>/`) | `~/.local/share/session-workbench/` | `~/Library/Application Support/session-workbench/` | `%LocalAppData%\session-workbench\` |
| saved agent copies (`working-copies/<session id>/`) | `~/.local/share/session-workbench/` | `~/Library/Application Support/session-workbench/` | `%LocalAppData%\session-workbench\` |
| window state | `~/.local/share/session-workbench/` | WebKit's default store | `%AppData%\session-workbench.exe\` |
| Local tab shell history (`history/`) | `~/.local/share/session-workbench/` | `~/Library/Application Support/session-workbench/` | — (not used) |
| window size and position (`window.json`) | `~/.local/share/session-workbench/` | `~/Library/Application Support/session-workbench/` | `%LocalAppData%\session-workbench\` |

`$XDG_CONFIG_HOME` / `$XDG_DATA_HOME` move them on Linux; the lock and `notes/` follow
`-config`. Window state is what this computer remembers outside `config.toml`: the active
session, the last tab per session, *Hidden files* and the sort order per session, collapsed headers, the sidebar width, the narrow sidebar, the switcher's
recent order and the last host and start mode used for a new session. The window's size,
position and whether it was maximised are saved in `window.json` when the window closes — also
on SIGTERM (e.g. at logout) or Ctrl+C in the terminal that started it — and the next start
opens there. A minimised or fullscreen window changes nothing. A position that is no longer on
a screen (a monitor was unplugged) is not used, and on Wayland, where apps cannot see or place
their windows, the position is neither saved nor set: the compositor decides. Delete the file to get the default (1280×800, placed by the
window manager) back. Nothing is stored in
your projects; on the agent hosts only the tmux sessions, the Remote tab shell history
(`~/.local/state/session-workbench/history/`) and, for working copies and live
share, the directories under `remote_workdir` (deleting a session leaves them).

## Layout

```
main.go, app.go        Wails entry point and frontend bindings
notify.go              desktop notifications when claude waits
menu.go, proc_*.go     macOS menu bar; hiding console windows on Windows
internal/terminal      pty processes + batched output events
internal/pathmap       host path -> agent-host path
internal/remote        ssh/tmux command lines (quoting, forced options)
internal/config        TOML config load / atomic save, data directory
internal/session       hosts + sessions: validation, mapping, ordering
internal/notes         notes pages: a tree of Markdown files per session and for the app
internal/spell         spelling dictionaries: built-in en_US (dict/, SCOWL), Hunspell files found
internal/forge         Web tab: links from the git remote, GitHub runs via gh
internal/fsbrowse      directory listing for the Files tab (confined to the session directory)
internal/opener        open / reveal with the OS, refusing executables
internal/gitstatus     git status markers and the repository check for the Files tab
internal/syncer        working copies: send (rsync or built-in), git fetch back, full transfers
                       (save, take files, send all / restore)
internal/share         live share: confined SFTP server + sshfs mount manager
internal/backup        Claude backups: ~/.claude of the agent hosts (rsync or built-in), restore
internal/xfer          built-in transfer without rsync: tar over ssh, confined unpacking
internal/instance      one instance per config file (lock)
internal/winstate      window size and position across restarts (window.json in the data dir)
internal/reclog        Remote tab recordings: readable text, hidden passwords, time segments
frontend/src           vanilla JS UI: main.js (app), terminal.js, files.js, notes.js (page tree),
                       editor.js and markup.js (notes editor: CodeMirror, formatting, lists),
                       spelling.js (word finder and checker: nspell), web.js,
                       appnotes.js and prompts.js (notes for all sessions, prompts menu), ui.js,
                       appearance.js (themes, mono filter), keys.js, fuzzy.js,
                       format.js (Files tab size/date columns and sort order), cmdmenu.js (command menu
                       entries), commands.js (editors for the command and start-command
                       lists), style.css; *.test.mjs are the node --test unit tests
build/                 app icon, Windows/macOS resources for Wails
packaging/             .desktop file, icons, man page, copyright, nfpm config for .deb/.rpm,
                       gen-docs.sh (changelog and man page per version)
docs/                  CONNECTIVITY.md: agent host setup, file access modes, security
scripts/deps.sh        make check / make deps
scripts/gui-smoke*.sh  headless GUI tests; scripts/smoke-lib.sh holds their shared checks,
                       scripts/fake-notifyd.py stands in for the desktop's notification service
.github/workflows      CI: test + build on Linux/Windows/macOS, release on v* tags
```
