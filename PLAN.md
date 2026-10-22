# Session Workbench — Implementation Plan

See SPEC.md for requirements. Each milestone ends in something runnable.
All milestones are done; v0.6.0 is the first release. Notes in *italics* record where the
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

## After v1 (v1.1)
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
  saved also when one signal arrives twice (Wails' and the app's handler).

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
- **Several dated saved copies** instead of only the last one: `working-copies/<id>/<date>/`,
  keep the last N (setting), Restore offers which one; rsync `--link-dest` against the
  previous copy keeps unchanged files as hard links (the built-in transfer copies in full).

## Testing
- Go unit tests for every internal package (pathmap, remote command quoting executed through a
  real shell, config, session, notes, spell (dictionaries, encodings), autosave (scheduling,
  one save at a time, saving all at quit), forge, fsbrowse, opener, gitstatus incl. the repository
  check, syncer incl. the built-in transfer, xfer, share, backup, instance, winstate, reclog, terminal incl. a ConPTY test on Windows) —
  `make test`, and in CI on Linux, Windows and macOS (race detector on Linux).
- Frontend unit tests (`node --test`): mono color filter, fuzzy matching, keybindings, size/date format, notes formatting, lists, spelling (with the built-in dictionary), prompts and the prompts menu, command menu entries
  on the notes page.
- Round trips against a real host (`make test-ssh`): ssh/tmux, working copies, live share,
  Claude backup and restore; working copies and backups also with the built-in transfer
  (`SW_NO_RSYNC=1`).
- Headless GUI tests driving the real app under Xvfb with screenshots (`make smoke-all`, 29
  scripts): terminal fidelity; sessions/dialogs/restart, add host, start-mode restart, one
  instance per config, remembered window size and position, Claude backups; Files; colors, switcher, settings/keys, reconnect,
  terminal extras (find, unicode widths, copy on select), folders, git markers, repository
  check, working copies (Get: a fast-forward and a new branch; save, take all files, restore after
  the agent copy is gone; Get without git; delete with the saved copy; saved automatically, at
  quit with progress, Quit now), live share (commits and
  branches through the mount), notes and tab order, Web tab, layout, desktop notifications
  (fake notification service), notes for all sessions and copy/paste, Remote tab recordings, command menu, prompts menu (also in the right-click menu) and start commands, private shell history, lists and spelling in the notes, badges; working copies and backups also with the built-in
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
