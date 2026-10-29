# Session Workbench — Specification

Status: v1.8 (2026-10-29, app v0.9.3) — v1.0 shipped in v0.6.0. Since then: session folders (F5),
narrow and resizable sidebar (F6), Claude backups (F7), one instance per config (F8), working
copies and backups without rsync (F9), desktop notifications (F4), git status and the
repository check in the Files tab (B5, B6), Notes and Web tabs (N1, W1), notes for all sessions
(N2), terminal copy/paste (T2), live share and working copies (docs/CONNECTIVITY.md), Linux
packages, Files tab date column and sort order (B1), window size and position remembered
(§4.7), notes as page trees with a formatting editor (N1, N1a), Remote tab recordings (F10),
Get listing and taking over every agent branch (F11), the file mode in the session menu (F1)
appending writes through the live share (§6), the command menu (T5), editable start commands
(R2), Settings in pages (§4.6), private shell history per session (L4), lists, check lists and
spelling in the notes (N1a, N1b), the prompts menu (T6) and full transfers for working copies (F12).
Sections describe the implemented behaviour.

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
| **Notes**  | Notes as a tree of Markdown pages, shown formatted (N1, N1a)                    |
| **Web**    | Build runs and web links of the repository (W1)                                 |

A session reopens on the tab used last; new sessions start on Local. The order of the tabs and
which are shown is a setting (`tab_order`, `hidden_tabs`; Settings → Appearance); tabs added in a newer
version are appended to a saved order. The current tab is marked like the current session in
the sidebar (tinted background, thick accent bar, bold label).

Switching sessions and tabs is instant and keyboard driven.

### Non-goals (v1)
- No built-in code editor, diff viewer, or git UI (only the Markdown notes editor, N1a).
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
description = "Port to the new API"    # optional, shown in the sidebar balloon (F1)
host       = "vibe"
local_path = "/home/mboot/share-local/workspace/terminal-app"
start_mode = "claude-continue"         # a name from [[ui.start_commands]] (R2)
color_mode = "mono"                    # optional, overrides the global color mode (T3)
local_init = "git status"              # optional, typed into the Local tab (L2)
env        = { FOO = "bar" }           # optional, Local tab environment (L2)
group      = "Work"                    # optional, sidebar folder listed in `groups`, e.g. "Work/Client A" (F5)
links      = [{ name = "Staging", url = "https://staging.example.com" }]   # optional, Web tab (W1)
commands   = [{ name = "test", command = "make test" }]   # optional, command menu (T5)
record     = "on"                      # optional, "on"/"off" overrides record_remote (F10)
history    = "off"                     # optional, "on"/"off" overrides private_history (L4)
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
  under their host. The session menu (right-click or ⋯) starts with the host's file mode
  (shared folder / live share (sshfs), mounted or not / working copy (sync)). Drag (or menu Move up/down) to reorder within its folder and host. Edit
  (rename, folder, start mode, colors, own commands (T5), recording (F10), shell history (L4)), disconnect, end remote session, delete (optionally ending
  the tmux session and, when there is one, deleting the saved agent copy of F12; both unticked).
  A red ▲ marks a session whose notes have TODO lines (N1c).
  Resting the mouse on a session shows a balloon with its name, local path and description
  (optional, set in the New session / Edit dialog; at most 2000 characters, line ends kept,
  trimmed; a longer hand-edited one is shown and reported) after `description_delay` seconds (0–10, default 1, Settings → Appearance); it goes
  when the mouse leaves, a button is pressed, the list scrolls or changes, or a menu opens.
  A right-click on the empty space of the list (below or between hosts and folders): the + menu (new session, new folder, add host)
  and *Notes for all sessions…* / *Prompts for all sessions…* (N2, on its Home or Prompts page;
  the Prompts page is made if missing), *Quick switcher…*.
  The quick switcher (`quick_switcher` key or that menu) ranks sessions by a fuzzy match on name
  (weight 2), folder, host and local path, plus a description that contains the query as typed
  (case-insensitive; no fuzzy match in long text); empty query: most recent first, the current
  session last. Rows show the description's first line and a tooltip with the full path and
  description. It can be resized from its corner; the size is kept (web view storage).
- F2. "New session" dialog: pick host profile → pick directory (Browse… starts at the first
  workspace root; any path under `local_prefix`) → name → description (optional) → folder → start mode (one of the start commands,
  R2; the default list is claude, claude-continue, shell). It previews the remote path or why the directory can't be
  used; one session per directory and host. Host and directory are fixed afterwards.
- F3. Sessions persist in config; on app start all sessions are restored (terminals re-attach lazily
  on first view, or eagerly — configurable).
- F4. Activity indicator: a session gets a badge when a non-visible terminal (or any terminal
  while the window is in the background) rings the bell, or produces output after 10 s idle (at
  least max(512 bytes, 2 × columns) within 2 s; redraws within 1.5 s of a resize don't count). The
  tab that caused it is marked too; the window title counts sessions with badges and ends with
  the active session's name (`(2) Session Workbench — alpha`). claude waiting for input → bell;
  the user sets Claude Code's `preferredNotifChannel` to `terminal_bell`.
  A bell in such a terminal also shows a desktop notification (`notifications`, default on; Wails
  runtime notifications: freedesktop D-Bus on Linux, toasts on Windows, Notification Center on
  macOS, which may refuse an unsigned app): "Claude is waiting" for a Remote tab, "Bell in
  <tab>" otherwise, with the session name and host; at most one per session per minute, and
  none again until the session was looked at, which withdraws it and resets the minute. A click
  brings the window forward and opens that session's tab that rang. Without a notification service nothing is shown.
- F5. Folders: sessions can be put in folders (e.g. "Work", "Private"); `groups` in the config
  gives their order, `group` on a session its folder. Folders nest by path: "Work/Client A" is
  the folder Client A in Work (parts trimmed, none empty; siblings in the order of `groups`).
  The sidebar shows ungrouped sessions first, then each folder (collapsible, indented by depth,
  with a count that includes its subfolders), each with its own sessions split by host and
  then its subfolders. Folders can be created (also as a subfolder, or as a path: missing
  folders on the way are added), renamed (sessions and subfolders follow), reordered among
  their siblings, moved into another folder or the top ("Move folder to…", not into itself)
  and deleted together with their subfolders (their sessions stay, in the folder above).
  Sessions move between folders via the session dialog, "Move to folder…", or drag onto a
  folder header. A folder whose parent is not in `groups` is reported and shown under a
  "(missing)" parent.
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

- F10. Remote tab recordings (off by default): `record_remote` (Settings → Backups and recordings) records every
  session's Remote tab from the moment it is switched on; a session's `record` ("on"/"off",
  Edit… or the session menu) overrides it; switching off stops at once. The terminal manager
  taps output and input (internal/reclog): output as readable text (escape sequences dropped;
  CR, BS, erase-in-line, column moves and character insert/delete applied to the current line;
  a move to another row or a screen clear ends the line; lines at most 10 000 characters),
  input as `YYYY-MM-DD HH:MM:SS > line` per Enter (editing keys applied, arrows, function
  keys and mouse reports dropped, a lone Esc dropped, ^C/^D/^Z shown, each line of a paste a
  line), input typed while the output line ends in a password prompt (`password`,
  `passphrase`, `passcode`, `PIN`, `token`, `secret` followed by `:` or `?`), or after such a
  prompt line that a cursor move rather than a newline ended (tmux redrawing its status bar),
  as `[hidden]` (`[hidden]^C` if ended by ^C/^D/^Z; ^C alone stays `^C`; once a line of a
  paste is hidden, the rest of it is too). Files per segment (`record_every`, 5m–24h, default
  1h; bounds are clock times counted from local midnight, so a DST day has one longer or
  shorter segment, and when the length does not divide 24h the day's last segment ends at
  midnight), the first named `YYYYMMDD-HHMM.log` after the moment recording starts, later
  ones after their segment's start, in `<record_dir or data dir/logs>/<host>/<local path>`
  (path elements made safe for every OS; Windows reserved names such as `CON` or `nul.txt`
  get a `_`), owner-only, each starting with a `#` line (session, host, path, time) and
  ending with `# continued in …`, `# recording stopped at …` or `# connection ended at …`
  (the last line goes into the current file even after its segment ended). A new file only
  starts when something is recorded. A reconnect within the segment continues the file
  (within one app run). Recording switched on while the Remote tab is not connected starts
  when it connects. A red "● recording" in the session bar; the session menu opens the
  folder. Failing to open or write a file is shown as a message, once per folder and segment
  setting; a file that fails while recording is tried again once a minute. A changed
  `record_dir` or `record_every` ends the running file (`# recording stopped at …`) and
  starts a new one. Output and input are queued for a writer and never wait for the disk;
  what does not fit is left out with a `# N bytes not recorded` line, and the next input line
  is hidden. Terminals register with the recorder before they start (`terminal.Spec.OnStart`),
  so even a connection that ends at once is recorded and closed.

- F11. Working copies (`files = "sync"`, docs/CONNECTIVITY.md mode C): Send mirrors the
  directory to the agent copy (preview; refused while the agent copy has commits not fetched
  yet; uncommitted agent changes only overwritten after confirming). Get fetches the agent's
  branches into `refs/remotes/agent-<id>/` (`--prune`, `transfer.fsckObjects`) and lists each
  (at most 200; more is said) next to the host's branch of the same name: new, ahead, up to
  date, behind, diverged, or unsafe (a name outside `[A-Za-z0-9._/+@-]` or starting with `-`:
  never taken over or put in a suggested command), with its commits (≤20) and changed files
  (≤200, names and status only) and whether it is checked out here or on the agent. New and
  ahead branches are ticked; Take over creates new ones (`git branch --no-track`) and
  fast-forwards the others (`git branch --force` after an ancestry check, refusing a branch
  checked out in another worktree; the checked-out branch with `git merge --ff-only`, so its
  files follow and the host repository's own hooks run). Nothing ticked, or nothing taken
  over, keeps the dialog open with the reason. For a diverged branch the shell-quoted
  `git checkout … && git merge …` is shown.
- F12. Full transfers for working copies (*Full ▾* next to the tabs and the session menu; each
  in the sync dialog with a preview where one exists):
  - Save agent copy: the whole agent copy (with `.git`, untracked and ignored files) to a new
    dated copy `<data dir>/working-copies/<id>/<YYYYMMDD-HHMMSS>` (local time;
    `internal/savedcopy`), written as `<stamp>.new` and renamed when complete (a failed or
    stopped save removes it; leftovers go at the next prune) (`rsync -a --delete --safe-links
    --no-devices --no-specials --link-dest=<the newest copy>`: unchanged files are hard links;
    built-in: one tar stream unpacked through `os.Root`, everything copied); `<stamp>.json` next
    to it records when, from where and how many files (counted once, when saved). After each
    save the copies are pruned: kept are all of the last `saved_copies_hours` (1–720, default
    24), the newest of each earlier day for `saved_copies_days` days back from today (0–365,
    default 14) and always the newest copy. A copy from before dated copies (`files` +
    `saved.json`) is renamed to its date. Kept when the session is deleted unless the delete
    dialog's box for them is ticked (`RemoveSaved`: `working-copies/<id>/`, the id checked
    first). Exit 24
    (files vanished) is a success with a note. Refused while there is no agent copy.
  - Automatic saves (`internal/autosave`): `save_agent_copies` (default on) saves every
    working-copy session without an error (one never sent has no agent copy: nothing to save,
    no failure, tried again after the interval) every `save_agent_copies_every` (5m–24h, default 1h;
    first round 1 minute after start, then checked every minute; due when the interval has
    passed since the last save or attempt, after a failure every 15 minutes at most; at most 2 h
    a save), the last save of an earlier run read from `saved.json`. One save per session at a
    time: a manual Save while one runs says so, a scheduled one is skipped. Progress: rsync 3.1+
    (`--info=progress2 --no-inc-recursive`, not openrsync) reports a percentage; else none. The
    bar shows *⟳ saving N%*, *saved HH:MM* (date when not today; the note in the tooltip) or
    *⚠ not saved* (the error in the tooltip).
  - At quit (`save_agent_copies_at_quit`, default on): after the notes are saved the page calls
    `QuitSaving`, which saves every working-copy session one after another (a save already
    running is waited for and counts) while an overlay over the window shows *n of N: name —
    pct%* with a progress bar; the app quits when all worked, without a time limit. When one
    failed the overlay lists it and waits for *Quit*. *Quit now (stop saving)* cancels the
    saves and quits. Until then a close request (window ✕, SIGTERM, Ctrl+C: one signal reaches
    Wails' handler and the app's, so two come at once) is refused; a page that does not call
    `QuitSaving` within 3 s does not keep the app open; a second Ctrl+C kills it.
  - Take all files: agent copy → session directory, new and changed files, `--exclude=.git`
    (any depth), optionally `--delete` (excluded paths stay); preview by dry run; the number of
    changed or deleted files with uncommitted changes here (`git diff --name-only --relative`,
    staged and not, tracked only; paths relative to the session directory) needs a
    confirmation. Built-in: `cksum` listing on the agent host without `.git`, compared here,
    then a tar stream of which only the wanted files (and their folders) are unpacked through
    `os.Root`; deletions through `os.Root`.
  - Send all / Restore saved copy: `rsync -a --delete` without the `.gitignore` filter, from
    the session directory or a saved copy picked in the dialog (newest first; the preview
    follows the pick); not blocked by unfetched commits or uncommitted
    changes, which the preview reports and the dialog requires a tick for. An empty source is
    refused (it would empty the agent copy). Built-in: as F9 with every file on both sides.
  - Get on a session directory that is not a repository's top (no git, or a subdirectory)
    opens Take all files with *delete* ticked: a full copy of the agent's side.

### 4.2 Remote tab
- R1. Implemented by spawning the system `ssh` client inside a local PTY (simplified; quoting
  per §6):
  `ssh -t -o ForwardAgent=no -o ForwardX11=no -o ForwardX11Trusted=no -o ClearAllForwardings=yes
  -o RemoteCommand=none -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -o ConnectTimeout=20
  -- <target>
  'exec "$SHELL" -lc "cd <remote_path> || { printf '%s\n' 'session directory <remote_path> does
  not exist on this host' >&2; exit 3; }; exec tmux new-session -A -s cw-<id> -c \"\$PWD\" <pane cmd>"'`
  (with private history, L4, the pane command starts with the history exports and
  `\; set-environment …` follows it) (each part quoted as one shell word, so nothing in the path expands)
  (a leading `~/` in the path is expanded through `"$HOME"`). Everything runs in the remote user's
  login shell so that PATH includes `~/.local/bin` (claude) or Homebrew (tmux). A missing remote
  directory exits 3 and the Remote tab says so (for a working copy it offers *Send to agent…*)
  instead of starting in `$HOME`.
- R2. **tmux is mandatory on the agent host** — it makes sessions survive app restarts, closed
  windows and network drops (not agent-host reboots). Reopening the tab re-attaches (`-A`); the
  start command only runs when the tmux session is first created. The start commands are a list
  (`[[ui.start_commands]]`, Settings → Commands; name + one-line command, names unique
  ignoring case, at least one); a session's `start_mode` names one, and an unknown name is the
  session's error. The default list is `claude` (`claude`), `claude-continue`
  (`claude --continue || claude`: `--continue` exits 1 without a conversation in the directory,
  quitting claude exits 0) and `shell` (empty: just the login shell). When the command ends,
  however it ends (exit, Ctrl+C, a syntax error), the pane falls back to a shell: pane cmd =
  `"$SHELL" -lic '<cmd>'; exec "$SHELL" -l` (the fallback is outside the command's own shell
  line, so a `#` comment, a trailing `\` or an unbalanced quote in it cannot swallow it), or
  `exec "$SHELL" -l` for an empty command. A start command is one line without control
  characters and cannot start with `-` (the shell would read options). Any
  `[[ui.start_commands]]` list replaces the default three; a missing or empty one gets them,
  and they are written to the file at the next save. Names match ignoring case. Only a changed
  start mode is checked against the list, so a session whose start command was removed can still
  be edited (it cannot start until it gets one that exists). Changing a session's start mode
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
- L4. Private shell history (`private_history`, default on; a session's `history` "on"/"off"
  overrides it; Settings → Terminals, Edit… → Shell history). Local tab: the shell gets
  `HISTFILE=<data dir>/history/<id>.history` (folder owner-only) and
  `fish_history=session_<id with - as _>`; the session's own `env` wins; not on Windows
  (PowerShell, cmd take no history file from the environment). Remote tab: the pane command
  starts with `mkdir -p "$HOME/.local/state/session-workbench/history"; chmod 700 "…/history";
  export HISTFILE="…/history/<id>"; export fish_history=session_<id with - as _>;` (understood by
  sh, bash, zsh and fish), and tmux's session environment gets the same two variables
  (`set-environment`), so new windows and panes use them too; it applies to tmux sessions
  created afterwards. A `HISTFILE` set in the user's shell startup files wins. Closing a Local
  or Remote tab hangs up its process first (SIGHUP, then SIGKILL after 0.5 s; on Windows at
  once), so shells write their history on the way out.

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
- N1. Notes per session as a tree of pages (like the Zim desktop wiki): page tree on the left
  (Home always first, then by name; pages without text of their own in italics; expanded
  folders and the open page remembered per session), editor on the right. Context menu / +:
  new page, new subpage (+: of the open page), rename or move (a path with `/`; subpages follow; the target must be
  free, not inside the page (ignoring case) and keep the subpages within 10 levels; a
  case-only rename only of the page itself), delete (with subpages, confirmed). Page `Design/API` is
  `notes/<session id>/Design/API.md` next to the config file (owner-only folders and files,
  atomic writes, at most 4 MB a page, through `os.Root`), not in the project: notes stay out
  of git and are not visible to the agent host. Names: 1–100 characters per element, at most
  10 deep (at most 5000 pages listed per tree), none of `/ \ : * ? " < > |`, no leading dot, no leading/trailing space or trailing
  dot, not ending in `.md`, no Windows device names. An empty page removes its file and
  folders left empty. Saved automatically (shortly after typing stops, on opening another
  page, on leaving the tab, when the window loses focus); reloaded when shown if changed
  outside the app (unless there are unsaved edits); kept when the session is deleted. The old
  `notes/<id>.md` is moved to the Home page on first use (unless Home exists).
- N1a. Editor: CodeMirror 6 with Markdown (GFM); the text keeps its markers, shown in
  italics, and is drawn formatted: headings bold (`#` 1.4×, `##` 1.2×), `**bold**`, `_italic_`,
  `~~strike~~`, `<u>underline</u>`, quotes in italics, links underlined; the page uses the
  terminal font (Settings → Appearance). Toolbar B I U S and
  Mod+B / Mod+I / Mod+U / Mod+Shift+X toggle the markers around the selection: no selection
  inserts a pair (or removes an empty one around the cursor); one line keeps outer whitespace
  outside and removes markers inside or just outside the selection, else adds them; several
  lines are toggled line by line (removed only if every non-blank line has them).
  Lists: toolbar • ☐ and Mod+Shift+8 / Mod+Shift+9 make the selected lines (the cursor's
  line without a selection; blank lines stay) `- ` or `- [ ] ` items after their indent
  (other list markers and check boxes are replaced), or plain lines when all already are.
  A task marker (`[ ]`, `[x]`, `[X]`) is drawn as a check box (black border); a click or
  Mod+Enter ticks it (selected lines: all ticked when any is not, else all unticked). In a list
  item (`-`, `*`, `+`, `1.`, `1)` plus a space, after the cursor's marker) Enter starts the next
  item with the same indent and marker (numbers +1, check box empty) and carries the text after
  the cursor (not inside a code block: there it is a plain new line); in an empty item it outdents by two spaces (or a tab), at the left edge it clears
  the marker (ends the list). Tab / Shift+Tab indent / outdent a list item by two; elsewhere
  Enter is a plain new line and Tab inserts two spaces (with a selection: indents its lines by
  two; Shift+Tab outdents them). Mod+C/X/V copy, cut and paste; a right-click menu
  offers Cut, Copy, Paste (system clipboard), Select all, the four formats, the two lists and
  Tick / untick; a right-click outside the selection moves the cursor there first.
  A key the editor acts on is not also an app key there (Mod+Shift+8 / 9 are `tab_8` / `tab_9`
  elsewhere).
- N1b. Spelling (`spell_check`, default on): misspelled words on screen get a red wavy
  underline (the web view's own checker is off: it has no suggestions in our menu and is off in
  WebKitGTK). Words are runs of letters with inner apostrophes (' or ’); skipped: one letter,
  a capital after the first letter (CamelCase, ACRONYM), next to a digit, `_ / \ @ # $ % & = ~`
  or a backtick, or followed by `.` and a letter (file names), and inside inline code, code
  blocks, URLs, autolinks, HTML and link labels. The word being typed is checked once the
  cursor leaves it. Right-click a misspelled word: up to 6 corrections (closest first:
  Damerau–Levenshtein, same letters first among equals; the word's capital kept) and *Add
  “word” to dictionary*, which adds it to `spell_words` (sorted, once each, without spaces, up
  to 100 characters; Settings → Notes edits the list). Right-click an added word (also
  capitalized at the start of a sentence): *Remove “word” from dictionary* takes it out again
  (`RemoveSpellWord`; the built-in tool words cannot be removed). The checker is nspell with a Hunspell
  dictionary from the backend (`internal/spell`), loaded when the first notes editor opens:
  `spell_language` (default `en_US`, built in from SCOWL); other languages are `<lang>.aff` +
  `<lang>.dic` in `dictionaries/` next to the config file, then (Linux) `~/.local/share/hunspell`,
  `/usr/share/hunspell`, `/usr/share/myspell(/dicts)`, `/usr/local/share/hunspell`, (macOS)
  `~/Library/Spelling`, `/Library/Spelling`; the first found wins. Files are converted to UTF-8
  from their `SET` encoding (default ISO8859-1). A dictionary that cannot be loaded (removed,
  unknown encoding) turns checking off with a message, once per language. A language name is letters, digits and
  `_ @ . -`, not starting with `.` or `-`; an invalid one in the file becomes `en_US`. A few
  tool names (`TOOL_WORDS` in frontend/src/spelling.js: claude, tmux, ssh, git…) are always known.
- N1c. TODO marks: a line of a page with the word `TODO` (capitals, not part of a longer word:
  not `TODOs`, `MYTODO`, `TODO_LIST`; `notes.CountTodos`) is a TODO. A session with any gets a
  red ▲ in the sidebar (F1), on its Notes tab and on each such page in the tree (a folded page
  also for its subpages); tooltips and the sidebar balloon give the count and the pages. Checked
  for every session at start and when the window gets the focus (`NotesTodos`, reading the
  files, so edits outside the app count), and after every save in the Notes tab. The notes for
  all sessions mark their tree the same way.
- N2. Notes for all sessions: a page over the main area (sidebar ✎, `app_notes` key, the session
  list's menu of F1) with the same
  page tree and editor for `notes/_app/` (`_` keeps it apart from session ids; the old
  `notes/_app.md` becomes the page *Prompts*). Prompts come from the page *Prompts* and its
  subpages (tree order; without that page the list offers to create it). Every `## name`
  heading there starts a prompt (its text runs to the next `#`/`##` heading); the page lists them with Copy (clipboard)
  and Paste into Remote (bracketed paste into the active session's Remote tab once connected and
  the program there has turned bracketed paste on; a multi-line prompt is refused until then, so
  its lines cannot run one by one; no Enter). Headings inside fenced code blocks (``` or ~~~, closed by a fence of the same kind and at least the same length) don't count; a heading starts in column 0 with a space after the `#`s. Esc, ✕, the key or selecting a session closes it.

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
  - Configurable globally (Settings → Appearance: the default for every session), overridable per session
    in Edit… (Colors) or with the session menu (choosing the global value removes the override);
    switchable at runtime without restarting the terminal.
- T4. Resize propagates to the PTY (and thus through ssh to tmux).
- T6. Prompts menu: a *Prompts ▾* button after *Commands ▾* in the Local and Remote tabs lists
  the session's own prompts (the page *Prompts* of the session's notes and its subpages, read
  when the menu opens; the open page's unsaved text counts) under *This session*, then those of
  N2 under *All sessions* (hovering shows the text). Choosing one pastes it into that tab as N2
  does for the Remote tab (bracketed paste once the program there turns it on; no Enter).
  The terminal's right-click menu has the same entries after its *Commands* section, under
  *Prompts (this session)* / *Prompts (all sessions)* (no section without prompts); the menu
  opens once the session's prompts are read.
- T5. Command menu in the Local and Remote tabs: a *Commands* section at the end of the
  right-click menu and a *Commands ▾* button next to the tabs (shown when there are commands).
  The session's own commands (`commands` on the session, Edit…) come first, then
  `[[ui.commands]]` (Settings → Commands): name, one-line command, `run` (default true: typed,
  then Enter; false: only typed), `group` (optional). Commands without a group come first; each
  group is a submenu (in the order groups first appear; a session's and the global commands of
  one group share it, the session's first; a blank group is none). Submenus open on hover, →,
  Enter or a click; ← and Esc close them. At most 100 per list; names non-empty, without leading or
  trailing spaces, unique within their group ignoring case, up to 60 characters; a group the
  same, without control characters; commands non-empty, one line
  without control characters, up to 2000 characters; `run = true` is not written to the file.
  Entries a hand-edited file got wrong are left out of the menu. Disabled while the terminal is not running. Invalid lists
  in a hand-edited file are listed as problems; Settings refuses to save them.

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
action becomes unbound), otherwise the first in this order keeps it (the other is disabled):
next/prev session, the tabs by name, new session, quick switcher, settings, find, narrow sidebar, notes for all sessions, session 1–9, tab 1–9 (`ACTIONS` in frontend/src/keys.js). Binding the terminal's
copy/paste/interrupt keys (Ctrl+C/V, Ctrl+Shift+C/V) is allowed with a warning. Read at start (restart after editing); Settings → Keys lists the current bindings.

Settings is one dialog with pages behind a tab bar (the last one used opens again):
Appearance (theme, colors, fonts, description delay, tab order), Terminals (scrollback, connecting, reconnecting,
badges, copy/paste, notifications, private shell history), Commands (command menu, start commands), Backups and
recordings, Notes (spelling on/off, language, own words), Keys.

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
  Remote tab. Otherwise the app falls back: unknown color mode, `record` or `history` value → the global
  one, a non-http(s)
  link is not shown, the second of two hosts with one name is ignored; an empty name, a
  directory with two sessions on one host, a folder (or a subfolder's parent) not in `groups`, a
  duplicate or empty folder name, a folder path with an empty part or spaces around a `/` are
  only reported. A Remote tab that cannot start for such an error does not reconnect
  automatically; it shows the reason. Every `[ui]` value out of range or unknown gets its
  default: `font_size` (6–48), `scrollback` (1–1 000 000), `min_contrast` (1–21), `theme`,
  `color_mode`, `connect_on_start`, `claude_backup_every` (5m–168h), `record_every` (5m–24h),
  `record_dir` (absolute or under `~`), `spell_language`, `save_agent_copies_every` (5m–24h),
  `saved_copies_hours` (1–720), `saved_copies_days` (0–365), `description_delay` (0–10); `spell_words` are tidied (N1b). The command lists are not reset: invalid ones are
  reported (T5, R2) and Settings refuses to save them until fixed; an empty `start_commands` gets
  the default three.
- `<config>.lock` next to the file keeps a second instance from using it (F8); `notes/` and
  `dictionaries/` (own spelling dictionaries, N1b) are there too.
- Data that is not configuration lives in the data dir: `$XDG_DATA_HOME/session-workbench`
  (Linux, default `~/.local/share`), `%LocalAppData%\session-workbench` (Windows),
  `~/Library/Application Support/session-workbench` (macOS): Claude backups, saved agent copies (F12), the window size
  and position (`window.json`: width, height, pos (x, y known), x, y, maximised; written
  atomically when the window closes, also on SIGTERM/Ctrl+C, which close it the normal way (a
  second signal kills the app); a minimised or fullscreen window changes nothing (Windows
  reports a minimised window as not maximised); while maximised the last normal size and
  position are kept; a missing file or a size outside 640×400…16384 gives 1280×800). The
  position is restored once, in the background, when the page is loaded: Wails sets it
  relative to the current monitor but reports screen coordinates, so the move is read back and
  corrected (at most 3 times, 100 ms apart); it is skipped when less than 100 pixels of the
  window, or its title bar, would be on a screen (only the primary monitor's place is known,
  the others count as possibly on any side). On Wayland (`WAYLAND_DISPLAY` set, `GDK_BACKEND`
  not x11) the position is neither saved nor set: the compositor places windows.
  On Linux the data dir also holds WebKit's storage. Window state (active session, last tab
  per session, Hidden files and sort order per session, collapsed headers, sidebar width,
  narrow sidebar, switcher order and size, last host and start mode for a new session) is in the web view's
  storage: there on Linux, `%AppData%\session-workbench.exe\` on Windows (WebView2 default),
  WebKit's default store on macOS. Notes stay next to the config file.

## 5. Architecture

```
┌──────────────────────── Wails app (host) ────────────────────────┐
│ Frontend (plain JS + xterm.js + CodeMirror, no framework)         │
│   sidebar · tab bar · xterm instances · file tree · dialogs       │
│        ▲  events: term:data:<id> (base64), term:exit:<id>         │
│        │  calls:  TermWrite/Resize, StartLocal/Remote, ListDir,   │
│        │          OpenPath, session/host/folder CRUD, Sync*, Backup*│
│ Backend (Go)                                                      │
│   config · session manager · pty manager · pathmap · remote ·     │
│   fsbrowse · opener                                               │
│   gitstatus · notes · forge · syncer · xfer · share · backup ·    │
│   instance · winstate · reclog · notify                           │
└───────────┬───────────────────────────────┬───────────────────────┘
            │ PTY: local shell              │ PTY: ssh → agent host → tmux → claude
```

- **Language/framework**: Go 1.26+ + Wails v2.16 (v3 is still beta). On Ubuntu 24.04+ build with
  `-tags webkit2_41` (libwebkit2gtk-4.1). Frontend is vanilla JS with xterm.js (terminals) and
  CodeMirror 6 (notes editor) installed via npm and bundled by Vite 6 (Node 18+); no frontend framework.
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
internal/syncer            working copies (rsync or built-in out, git fetch back, full transfers)
internal/autosave          agent copies saved regularly and at quit
internal/xfer              built-in transfer without rsync (tar over ssh, cksum)
internal/share             live share (confined SFTP server, sshfs mounts)
internal/notes             notes pages (a Markdown tree per session and for the app)
internal/spell             spelling dictionaries (built-in en_US, Hunspell files found, UTF-8)
internal/backup            Claude backups (rsync or built-in copy of ~/.claude, restore)
internal/instance          one instance per config file (lock)
internal/winstate          window size and position across restarts
internal/reclog            Remote tab recordings
frontend/src               main.js, terminal.js, files.js, notes.js, editor.js, markup.js,
                           spelling.js, appnotes.js, prompts.js,
                           web.js, ui.js, appearance.js, keys.js, fuzzy.js, format.js, cmdmenu.js,
                           commands.js, style.css
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
- Full transfers (F12): a saved agent copy holds the agent's `.git` (hooks, config) owner-only
  in the data dir and is never used by git here; Take all files brings agent-written files into
  the session directory unreviewed (never `.git`), so the dialog warns to check them before
  running anything; Send all sends ignored files such as `.env`.
- Remote tab recordings (F10, off by default) contain what was typed and shown, owner-only;
  input after a recognised password prompt is hidden, but the remote echo state is not
  visible, so other secrets typed there are recorded.
- Private shell history (L4, on by default) keeps what was typed at the shell prompt, as the
  shell's usual history does, including secrets given on a command line: owner-only folders
  `<data dir>/history/` and, on each agent host, `~/.local/state/session-workbench/history/`.
- Fix commands the repository check suggests are shell-quoted: key names and paths come from a
  repository the agent host may have written.
- Notes: the book is a validated session id or `_app`; page names are checked (no `..`,
  separators, reserved characters or Windows device names) and files are read and written
  through `os.Root` on the book's folder (no escape through `..` or symlinks out of it),
  owner-only, atomic, at most 4 MB a page; notes are rendered with `textContent` only.
- Spelling dictionaries: the language name is checked (letters, digits, `_ @ . -`; no
  separators or leading dot), so only `<name>.aff` / `<name>.dic` in the listed folders are
  read; their text goes to the checker only, never into the page.
- Live share: the SFTP server is confined with `os.Root`; symlinks only with relative targets
  inside the directory, never created through a symlinked parent (a rename re-checks moved
  links); hard links refused; ownership changes ignored; "append" is not passed to the file
  system: writes go to the offset the client sends (sshfs sends the end of the file for
  appending writes; writing at the end ourselves would reorder data, as pkg/sftp runs writes
  on parallel workers); the mode asked for at open is used when it comes alone (4 bytes, as
  sshfs and OpenSSH send it: pkg/sftp drops the attribute flags), else 0644; a panic in a
  handler, or in a file or listing it returned, becomes an error for that request instead of
  ending the app. Working copies: the built-in transfer
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
