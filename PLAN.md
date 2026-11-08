# Session Workbench — Implementation Plan

See SPEC.md for requirements. Each milestone ends in something runnable.
All milestones are done; v0.6.0 was the first release (SPEC v1.0) and v1.0.0 (2026-10-31)
the first release version. Notes in *italics* record where the
implementation differs from the original plan.

## M0 — Skeleton (½ day) ✅
- `wails init` with vanilla JS template; Go module; Makefile (`dev`, `build`, `test`, …).
- Verify build on Linux; note Windows/macOS build prerequisites in README.

## M1 — One local terminal (1 day) ✅
- `internal/pty`: start process in PTY, read loop → batched events, Write, Resize, Close.
  *Named `internal/terminal`.*
- Frontend: single xterm.js + fit addon wired to backend.
- Done when: bash/zsh runs fully (vim, htop, colors, resize) on Linux.

## M2 — Remote terminal via ssh + tmux (1 day) ✅
- `internal/pathmap`: local→remote mapping + tests (incl. Windows paths).
- Remote command builder with quoting + tests; forced `ForwardAgent=no`/`ForwardX11=no`.
- Done when: tab runs `claude` in tmux on the agent host; closing/reopening the app re-attaches.

## M3 — Sessions + config (1–2 days) ✅
- `internal/config`: TOML load/save (atomic), defaults, per-OS location.
- `internal/session`: CRUD, ordering, start modes, lazy terminal creation.
- Frontend: sidebar, per-session tab bar (Remote/Local/Files), new-session dialog (dir picker
  under workspace roots, start mode).
- Done when: 5+ sessions persist across restarts and switching is instant.

## M4 — Files tab (1 day) ✅
- `internal/fsbrowse`: lazy ListDir (sorted, dirs first, hidden filter).
- `internal/opener`: xdg-open / open / start; refuse executables. *Windows uses ShellExecute.*
- Frontend tree, context menu (open, reveal, copy local/remote path, refresh).

## M5 — Keyboard + polish (1 day) ✅
- Keybinding layer (configurable, per-OS defaults), quick switcher.
- Bell/activity badges; disconnect overlay + reconnect; kill remote session.
- Theme/font settings: light theme (black on white) + dark; `color_mode` full (with
  minimum-contrast correction) / mono (no colors), global + per-session, live switch (SPEC T3).
  App chrome (sidebar, dialogs) follows the theme too.

## M6 — Cross-platform (1–2 days) ✅
- Windows: ConPTY, pwsh default, path mapping, `start`. macOS: Cmd bindings, `open`.
- CI build matrix (GitHub Actions) producing Linux/Windows/macOS artifacts.
- *Also: release on `v*` tags, version stamping, macOS app/Edit menu. Windows and macOS are
  tested and built in CI; the apps have not yet been tried interactively on those systems.*

## After SPEC v1.0 (app v0.6.0 to v1.0.0)
- Session folders (SPEC F5), keyboard-driven context menus.
- Git status in the Files tab (SPEC B5).
- Terminal find bar, Unicode 11 widths, copy on select (SPEC T1/T2, found missing in the
  docs/code cross-check).
- Repository check (SPEC B6); live share over ssh and working copies (docs/CONNECTIVITY.md
  modes B and C).
- Tab order Local, Files, Remote, Notes; Notes tab (SPEC N1); new sessions follow Settings.
- Web tab (SPEC W1); configurable tab order and hidden tabs; stronger current-session highlight.
- Linux `.deb`/`.rpm` packages (nfpm) with desktop entry and an own app icon; verified by
  installing in clean Ubuntu 24.04 and Fedora 44 containers.
- Keystrokes kept in order (one terminal write in flight); GUI tests check their results.
- claude from an interactive login shell (R2); app-wide font size; current tab marked like the
  current session; narrow sidebar (F6); black text everywhere (T3).
- `claude --continue` falls back to a new conversation; restart offer on a start-mode change.
- Claude backups (F7); one instance per config (F8); tab shortcuts in tooltips.
- Resizable sidebar; hosts and sessions checked when the config loads; more risky git keys;
  working copies and backups without rsync (Windows, F9); desktop notifications when claude
  waits (F4); signed commits and tags (ssh key).
- Notes and prompts for all sessions (N2); Ctrl+C/Ctrl+V and a right-click menu in terminals (T2).
- Docs/code cross-check (v0.8.11): shell-quoted fix commands in the repository check, Files tab
  refuses symlinks leading out of the session directory, failed live shares retried, backups
  paused without local rsync (until the built-in transfer of v0.8.18), `PgDn`/`PgUp` in `[keys]`.
- Docs/code cross-check (v0.8.22): live share crash on a retried mount fixed, no symlinks
  through linked parents (live share, built-in transfer), built-in Send for subdirectory
  sessions, injection-safe missing-directory message, repository check hardened (FIFO hooks,
  unreadable config, `config.worktree`, aliases with options, `git -C / config --file` fixes),
  opener checks link targets, config ranges, safer paste (bracketed paste only), key conflict
  rule, docs brought in line.
- Files tab: modification date column and sorting by name or date; window size and position
  remembered across restarts (SIGTERM closes the window the normal way).
- Notes as page trees (like Zim) per session and for the app, Markdown with formatting shown
  in a CodeMirror editor, B I U S toolbar and keys; prompts from the Prompts page.
- Remote tab recordings (F10): global and per session, readable text, hidden passwords,
  files per time segment.
- Docs/code cross-check (v0.8.30): recording, notes and window fixes; menus above the notes
  page; docs in line.
- v0.8.32: the live share no longer crashes on appending writes (git reflogs and lock files
  through sshfs: new branches arrived only after this); Get lists every agent branch and takes
  ticked ones over (F11); the session menu shows the file mode.
- Docs/code cross-check (v0.8.34): live share file modes and write offsets, unsafe agent branch
  names, docs in line.
- v0.8.35: command menu in the Local and Remote tabs (T5: right-click section and Commands ▾; global
  list plus the session's own, run or only type), editable start commands for the Remote tab
  (R2), Settings in pages behind a tab bar.
- Docs/code cross-check (v0.8.36): start command fallback outside the command's shell line,
  removed start modes, Settings save with a hidden bad value.
- v0.8.37: private shell history per session for the Local and Remote tabs (L4).
- Docs/code cross-check (v0.8.38): closing a tab hangs up the shell first (SIGHUP), so bash
  saves its history; tmux session environment for new windows; owner-only history folders on
  the agent host; docs in line.
- v0.8.39: lists and check lists in the notes (buttons, keys, Enter continues, clickable check
  boxes), spelling in the notes (in-app checker with corrections in the right-click menu, English
  built in, Hunspell dictionaries for other languages, own words; Settings → Notes), and a
  *Prompts ▾* menu in the Local and Remote tabs with the session's own prompts and those for
  all sessions (N1a, N1b, T6).
- Docs/code cross-check (v0.8.40): notes editors leave the spelling registry when a session is
  deleted, Enter in a code block is a plain new line, a missing dictionary says spelling is off;
  examples for lists, session prompts and a dictionary folder.
- v0.8.40: full transfers for working copies (F12): save the whole agent copy here, take all
  its files, send all (ignored files too), restore the saved copy to a (new) agent host; Get
  takes the files for a session directory without git.
- Docs/code cross-check (v0.8.41): Take all files counts uncommitted changes relative to the
  session directory (also in a repository's subdirectory) and asks before deleting such files
  too; the saved copy's file count is kept with it (Get no longer walks it); a key the notes
  editor acts on (Ctrl+Shift+8/9) is not also an app key; a worked example of restoring to a
  new agent host; saved copies are kept when a session is deleted.
- v0.8.42: prompts in the terminal's right-click menu, after the commands (T6); "Remove from
  dictionary" on an added word in the notes (N1b); deleting a session can delete its saved
  agent copy too (F1, F12); agent copies saved automatically every hour and at quit, with a
  progress overlay and Quit now (F12, `internal/autosave`); quitting waits for the notes to be
  saved also when one signal arrives twice (Wails' and the app's handler); dated saved copies
  (`internal/savedcopy`: every copy of the last 24 hours and one per day for 14 days, unchanged
  files hard-linked with rsync), Restore picks one.
- Docs/code cross-check (v0.8.43): a working-copy session never sent has nothing to save (no
  failure at quit, no error in the bar); ssh gets ConnectTimeout=20, so an agent host that is
  off fails a save (or reconnect) in 20 s instead of ~2 minutes; the quit overlay counts
  skipped sessions; no doubled "ssh:" in errors; GUI test of a failed save at quit; docs: the
  config values reset to defaults, the ssh options, what happens when a save fails.
- v0.8.44: a right-click on the empty space of the session list: the + menu (new session,
  folder, host) and Notes / Prompts for all sessions (F1, N2; the Prompts page made if missing).
- Docs/code cross-check (v0.8.44): the list menu also on the space between hosts and folders
  (and an empty folder's line), not only below the list; the notes docs name it as a way in.
- v0.8.45: a session description (New session / Edit dialog, `description` in config.toml),
  shown with name and path in a balloon on the session in the list after `description_delay`
  seconds (Settings → Appearance, default 1); it replaces the plain tooltip (F1).
- Docs/code cross-check (v0.8.45): an empty delay field saves the default (not 0); the balloon
  also goes when the list is scrolled with its scrollbar; a hand-edited description over 2000
  characters is reported; the Settings page summaries name the delay.
- v0.8.46: the quick switcher can be resized (the size is kept), finds sessions by text in
  their description and shows its first line; *Quick switcher…* in the session list's menu.
- Docs/code cross-check (v0.8.46): the as-typed description match moved into `rank`
  (frontend/src/fuzzy.js) with a unit test; the GUI test list in the README names the session
  list's menu, descriptions and switcher resizing.
- v0.9.0 (0.8.47 and 0.8.48 were not tagged): TODO marks (N1c): a line with the word TODO in
  a session's notes puts a red ▲ on the session in the list, its Notes tab and the page in the
  tree (`notes.Todos`, `NotesTodos`).
- v0.9.0: command groups (T5): `group` on a command puts it in a submenu of the command menus
  (menus can have submenus: hover, →, ← / Esc); the notes' + offers a subpage of the open page.
- Docs/code cross-check (v0.9.0): no code changes; the README names → / ← for submenus in the
  menu keys and the command groups in the GUI test list.
- v0.9.1: nested folders (F5): a folder path ("Work/Client A") in `groups` and `group`; the
  sidebar is a tree; *New subfolder…* and *Move folder to…* in the folder menu; rename, move
  and delete take the subfolders along (a deleted folder's sessions go one folder up).
- Docs/code cross-check (v0.9.1): the session dialog's folder list shows full paths (the closed
  list showed an indented name without its parent); the README names a / in a folder name and
  the folder path problems the config check reports.
- v0.9.2: github.com/labstack/echo/v4 4.15.3 → 4.16.1 (Dependabot, critical; only in Wails'
  dev server, not in release builds), with the golang.org/x modules it needs; Go 1.26+.
- v0.9.3: the window title ends with the active session's name: `(2) Session Workbench — alpha`.
- v0.9.4: Q&A tab (Q1) after Web: questions to Claude through the Claude API (Go SDK,
  streamed; model `qa_model`, default claude-opus-5-5; cached conversation, server-side
  fallbacks), one saved conversation per session, every question and answer logged in the
  session's log folder, SVG answers drawn as images. Credentials from ANTHROPIC_API_KEY or
  `ant auth login` only. Tested against a fake Claude API (internal/qa tests,
  scripts/fake-claude-api.py, gui-smoke-qa.sh).
- v0.9.5: Q&A without an API key: the engine is chosen in the tab's bar — Claude API, Claude
  Code (`claude -p` with its own login, no tools, its session resumed) or own commands
  (`[[ui.qa_commands]]`); the model list next to it (the API's live list, Claude Code's
  aliases, a command's `models`), both kept per session (`qa_engine`, `qa_claude`). Right-click
  menus with copy/paste in the turns and the question box. Tested with a fake claude and
  command (the test binary itself), the fake API's model list, gui-smoke-qa.sh; one real
  Claude Code round trip (haiku, session resumed).
- v0.9.6: Q&A answers shown as Markdown (headings, bold/italic/struck, code, lists, task
  lists, quotes, tables, links opened in the browser; DOM built with textContent only);
  copying (⧉, Ctrl+C, the right-click menu) gives the Markdown, also of a partial selection.
  Tests: qaparse.test.mjs (parser, copy back with a fake DOM), gui-smoke-qa.sh (rendering,
  partial and full copies). The Edit dialog shows the session id (read-only) and what it names.
- v0.9.7: Settings → Q&A: the defaults — who answers (`qa_engine`), the Claude API model
  (`qa_model`, the API's models suggested), the Claude Code model (new `qa_claude_model`) and
  program (`qa_claude`, with where it is found). Tested in gui-smoke-qa.sh.
- v1.1.11 (2026-11-08): one project on several agent hosts (SPEC F16; the user: "i work on one host
  at work and on another at home, is the current backup restore sufficient for that?" — no).
  Decided (questions): one laptop with the app; one session, several hosts, *Switch host…*;
  the code by the file mode (shared folder and live share: nothing to copy; a working copy: Get
  from agent first, Send on the new host), no git by the app; Claude's conversations and memory
  merged with newer wins, nothing deleted; a requirements list per project
  (`.session-workbench/requirements`, commands with optional versions) checked on demand and
  before a switch. My choices: the switch ends the old host's tmux session (ticked by default),
  container hosts and plain logins take no part, the project's `~/.claude.json` entry is merged
  and the file replaced only if claude did not change it meanwhile. Also: an unexpected error in
  an action now shows as a message (it did nothing before). Tests: handover (TestEncode,
  TestChanges, TestMergeConfig, TestSSHSwitch via make test-ssh), requirements (TestParse,
  TestCompare, TestEvaluate, TestScript), session (TestSetHost), gui-smoke-switch.sh; the new
  menu row moved Delete session… in gui-smoke-container and -compose. Cross-check: no Switch host…
  and no requirements check for an inactive session (F15).
- v1.1.10 (2026-11-08): inactive sessions (SPEC F15; the user: "some sessions remain in this app
  but are there only for holding until further development is required … does not need to
  reconnect to the remote (and to claude) when the app is restarted"). Decided (questions):
  Deactivate disconnects and asks about the rest (end tmux / stop the container, unticked);
  an inactive session opens with Notes, Files, Web and Q&A, its Local and Remote tabs show an
  Activate button; it stays in place in the list, marked ⏸ and "inactive" in italics; no live
  share, no notifications, nothing fetched from its host. Also the Hosts heading (F5; the
  user: "group them in a folder named Hosts"; decided: a heading in the list only) — my
  choice: shown only when there are folders (without folders the whole list is hosts).
  Tests: session (TestInactive), gui-smoke-inactive.sh; the menus' new row moved Delete
  session… in gui-smoke-hosts, -sync, -container and -compose; the heading moved a row in
  gui-smoke-folders and -listmenu.
- v1.1.9 (2026-11-05): v1.1.8's Windows build failed on two compose tests (TestComposeDoc,
  TestCheckDev built expected JSON with Windows paths unescaped; container hosts are not on
  Windows, the app was fine); the tests encode the paths. No release of v1.1.8; v1.1.9 is the
  first release with services (Compose).
- v1.1.8 (2026-11-04): services next to a container session's dev container (SPEC C7; the
  user: "would adding a docker compose edit window be helpful … specify the environment it
  needs (postgres, nginx, etc.)"). Decided (questions): the app writes the dev service and the
  session's file adds the rest; the file in the project (`.session-workbench/compose.yaml`,
  like the Dockerfile); the services start and stop with the dev container; an own window with
  service buttons. Found while building: the dev service's `environment` reaches only sshd, not
  the logins (passed with sshd `-o SetEnv`); Podman's compose needs its API socket (the app
  starts one when podman.socket does not run); Ubuntu 26.04's AppArmor refuses Podman's signal
  to pasta when the last container of a network stops (Stop and Remove check and carry on).
  Tests: container (TestComposeDoc, TestComposeFile, TestCheckDev, TestCheckCompose,
  TestComposeEnv, TestComposeProject, TestSetEnv, TestNetnsDenied; TestRealCompose with Podman
  and Docker), services.test.mjs, gui-smoke-compose.sh, gui-smoke-export.sh (the compose file
  in the archive).
- v1.1.7 (2026-11-03): Claude backups and ssh-only hosts (the user: "a vmware has no rsync so a
  remote backup can never be done and as it is a ssh only what would we need to replicate?" —
  nothing: no claude there). A host's *Back up Claude data from this host* (`backup`; off by
  default for plain-login hosts); a backup or restore whose agent host lacks rsync falls back to
  the built-in transfer and remembers it. Tests: config (TestBacksUp), backup (TestRemoteNoRsync;
  TestSSHNoRemoteRsync with a fake rsync, via make test-ssh), gui-smoke-login.sh (Add host…
  leaves backup to follow the logins).
- v1.1.6 (2026-11-03), also: sessions without tmux (SPEC F2b; the user: "make also a option for
  plain ssh logins not to have a tmux (vmware hosts have no tmux)"): a host's `tmux = false` and a
  per-session override, for any session; remote.PlainArgs; no End remote session, no tmux in the
  delete dialog, no restart offer. Tests: remote (TestPlainArgs), session (TestNoTmux),
  gui-smoke-login.sh (a no-tmux host: plain login in its folder, no tmux session; Add host…
  saves tmux = false).
- v1.1.6 (2026-11-03): Add/Edit host asks *Sessions on this host* right after Name, Kind and SSH
  target (the user: "move the sessions on this host as 4th item"); *are plain ssh logins*
  disables File access and the path fields. Tested in gui-smoke-login.sh.
- v1.1.5 (2026-11-03): v1.1.4's Windows build failed — TestPlainLogin made a session with a
  directory on a host without path mapping, which Windows paths need; the test host now has
  prefixes. v1.1.4 has no release; v1.1.5 is the first with -profile and plain logins.
- v1.1.4 (2026-11-03), also: plain ssh login sessions (SPEC F2a; the user's note "when using
  the app only for ssh remote sessions there is no need to share the local directory"): a host's
  `local_dir = false` and a per-session override; no local path, the shell in home or
  `remote_dir`; tabs Remote, Notes, Web, Q&A; named after the host; a shell by default. The
  backend refuses directory actions for them (Resolved.Dir); recordings under logins/<id>.
  Tests: session (TestPlainLogin), gui-smoke-login.sh.
- v1.1.4 (2026-11-03): separate setups (SPEC F14; asked by the user: "the default for AI supported
  development, but a second location for a simple ssh switcher"): `-profile NAME` or
  SESSION_WORKBENCH_PROFILE (the flag wins) puts config and data under NAME instead of
  session-workbench, tags tmux sessions (`cw-NAME-<id>`), the agent hosts' history folder,
  fish history names and containers, prefixes the browser storage's keys, and names the setup in
  the window title. Found: WebKit's storage folder on Linux does not follow Wails' program name
  (WebKit picks it first), hence the key prefix. Also: the Edit… dialog's commands on two lines
  (the command below, full width; the Where column had squeezed it); more GUI tests: a Remote
  command chosen on the Local tab, a Local command starting the ended Local shell again, export
  and import as .tar.gz. Tests: profile, remote, config, container units; gui-smoke-profile.sh.
- v1.1.3 (2026-11-02), also: Local and Remote commands (asked by the user while the export was
  built): `where` on a command ("" the tab it is chosen in, "local", "remote"); the menu lists
  the other tab's commands in their own section, and choosing one types it into that tab
  (started in the background when needed) while the tab on screen stays and keeps the keys.
  Settings → Commands and Edit… got a Where column. Tests: session (validation), cmdmenu
  (sections, sending, disabled), gui-smoke-commands.sh (a `where = "local"` command chosen on
  the Remote tab runs in the Local shell; the Remote tab still gets the keys).
- v1.1.3 (2026-11-02): export a session as a `.zip` or `.tar.gz` and import it as a new session
  (SPEC F13; decided with the user 2026-11-02): session menu → Export… asks the parts each time
  (always settings, notes, Dockerfile; ticked Q&A, recordings, shell history with the Remote one
  fetched over ssh; unticked saved agent copies and the project without git-ignored files);
  never keys, logins, Claude backups. + → Import session… makes a new session (new id): host
  suggested or added from the archive, project files only into an empty or new directory,
  env/local_init/trusted_git not imported, Claude Code session ids dropped, the Remote history
  pushed when the Remote tab connects. New package internal/sessionarchive (safe reading through
  os.Root; directories kept, so a fresh repository's empty .git/refs comes back). xfer now
  uses the target's own ssh options too (container sessions). Tests: unit tests (both formats,
  hostile archives, project file choice, modes, empty directories) and gui-smoke-export.sh.
- v1.1.2 (2026-11-02): the Dockerfile window can be moved (drag its heading) and resized (its
  corner), and keeps both while the app runs; its text and the build output have the right-click
  Cut/Copy/Paste/Select all menu (the Q&A question box's menu, now shared as `textMenu` in ui.js).
  Fixed on the way: a right-click menu opened inside a modal window was drawn behind it (a modal
  dialog is in the browser's top layer); the menu now goes into the open dialog. Tested in the new
  gui-smoke-dockerwin.sh (part of `make smoke-container`).
  Also in v1.1.2: the Local tab's command menu (right-click and Commands ▾, now always shown
  there) starts with *Back to the session directory* (`cd '<local_path>'`, Enter; `cd "<path>"`
  on Windows). Unit tests in cmdmenu.test.mjs (quoting, control characters refused); GUI test in
  gui-smoke-commands.sh.
- v1.1.1 (2026-11-02): v1.1.0's Windows build failed — three container tests check paths a mount
  cannot carry, and Windows temp paths have `C:`; they are skipped on Windows (container hosts are
  Linux and macOS only). v1.1.0 has no release; v1.1.1 is the first with container hosts.
- v1.1.0 (2026-11-02): containers as agent hosts (SPEC §4.2a, C1–C6; docs/CONNECTIVITY.md mode
  D): a "Containers (this computer)" host, each session in its own container (Podman or Docker,
  Settings → Containers) built from `.session-workbench/Dockerfile` or a template copy in the data
  folder; ssh into it on 127.0.0.1 with the app's key and a pinned host key; the directory
  mounted at the same path as you (rootless Podman: keep-id); the Claude Code login in the shared
  `sw-claude` volume or the session's own; the Dockerfile window with toolchain buttons, Save and
  rebuild with the build output; Stop container; delete removes container, image and own volume.
  Found while building: a company HTTPS proxy breaks downloads in the image — the app passes your
  extra CAs as a build context; claude.ai's installer is blocked there too — the app puts your
  own Claude Code (Linux) into the image. Tests: internal/container units (fake engine via the
  test binary), a real build/run/ssh test (SW_CONTAINER_TEST=podman|docker, both pass),
  gui-smoke-container.sh with Podman.
- v1.0.0 (2026-10-31): the first release version — the features of v0.9.7, plus (from the
  cross-check) *Claude Code's default* chosen in a session's Q&A bar stays its default when
  Settings name a Claude Code model.

## Next (ideas, not started)
- **Output folding in the Remote tab.** Collapse long output blocks (large code blocks) and jump
  between interactive prompts.
  - Frontend: hook into `frontend/src/terminal.js`. Explore xterm.js markers
    (`term.registerMarker`) or an overlay gutter that tracks the line numbers of Markdown
    boundaries (```` ``` ```` blocks, `###` headings); toggle visibility or heights with CSS/DOM
    without breaking the buffer's synchronisation.
  - Alternative: a switchable "interactive view" that renders the session's text with the notes
    Markdown parsing (`frontend/src/markup.js`, `editor.js`) into collapsible HTML
    `<details>`/`<summary>` blocks.
  - Decided 2026-10-31: postponed; when built, the reading view takes its text from the
    terminal's scrollback (instant, offline), not from tmux history.
- **echo/v4 v4.16.3** (noted 2026-11-02): fixes Dependabot alerts #6 and #7 (only Wails' dev
  server uses echo; not in the release binary). Update around mid-November, once the
  release is two weeks old, unless Dependabot opens a PR first.

## Testing
- docs/planning/Tests.md lists every test with the SPEC items it covers.
- Go unit tests for every internal package (pathmap, remote command quoting executed through a
  real shell, config, session, notes, spell (dictionaries, encodings), autosave (scheduling,
  one save at a time, saving all at quit), savedcopy (which copies are kept, the old layout), forge, qa (fake Claude API, fake Claude Code and command), container (fake engine; a real
  Podman/Docker build with SW_CONTAINER_TEST), fsbrowse, opener, gitstatus incl. the repository
  check, syncer incl. the built-in transfer, xfer, share, backup, instance, winstate, reclog, terminal incl. a ConPTY test on Windows) —
  `make test`, and in CI on Linux, Windows and macOS (race detector on Linux).
- Frontend unit tests (`node --test`): mono color filter, fuzzy matching, keybindings, size/date format, notes formatting, lists, spelling (with the built-in dictionary), prompts and the prompts menu, command menu entries, Q&A answers (text, code, SVG), Dockerfile toolchain blocks
  on the notes page.
- Round trips against a real host (`make test-ssh`): ssh/tmux, working copies, live share,
  Claude backup and restore; working copies and backups also with the built-in transfer
  (`SW_NO_RSYNC=1`).
- Headless GUI tests driving the real app under Xvfb with screenshots (`make smoke-all`, 38
  scripts): terminal fidelity; sessions/dialogs/restart, add host, start-mode restart, one
  instance per config, remembered window size and position, Claude backups; Files; colors, switcher, settings/keys, reconnect,
  terminal extras (find, unicode widths, copy on select), folders, git markers, repository
  check, working copies (Get: a fast-forward and a new branch; save, take all files, restore after
  the agent copy is gone; Get without git; delete with the saved copy; saved automatically, at
  quit with progress, Quit now), live share (commits and
  branches through the mount), notes and tab order, Web tab, layout, desktop notifications
  (fake notification service), notes for all sessions and copy/paste, Remote tab recordings, command menu, prompts menu (also in the right-click menu) and start commands, private shell history, lists and spelling in the notes, badges and window title, Q&A tab (fake Claude API and Claude Code: engines, models, right-click menus); container hosts
  (real Podman or Docker: build, connect, Dockerfile window — also moving, resizing and its
  right-click menu — rebuild, stop, delete); exporting and importing a session (.zip and .tar.gz); a separate setup (-profile); plain ssh logins; working copies and backups also with the built-in
  transfer.
  Each test checks its results (terminal output, saved config, files, window title, screenshot
  pixels for the layout) and fails on a wrong one; colors are judged from the screenshots.
  CI runs neither vet, `test-ssh` nor the GUI tests.

## Development setup
- All development, builds and tests run on the agent host (Claude's side).
- GUI verification there runs headless under Xvfb with screenshots; the "Local" tab then is a
  VM-local shell, which is fine for testing. Real use: copy the built binary to the host.
- Linux build deps: npm, libgtk-3-dev, libwebkit2gtk-4.1-dev, build-essential, pkg-config;
  for the GUI tests: xvfb, xdotool, imagemagick, tmux, rsync (optional), sshfs, git, jq, dbus, python3-dbus, python3-gi, libglib2.0-bin (gdbus),
  `ssh localhost` without a password, and bash as the login shell (history test).
