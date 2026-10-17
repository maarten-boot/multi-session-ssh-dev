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
  pause without local rsync, `PgDn`/`PgUp` in `[keys]`.

## Testing
- Go unit tests for every internal package (pathmap, remote command quoting executed through a
  real shell, config, session, notes, forge, fsbrowse, opener, gitstatus incl. the repository
  check, syncer, share, backup, instance, terminal incl. a ConPTY test on Windows) —
  `make test`, and in CI on Linux, Windows and macOS (race detector on Linux).
- Frontend unit tests (`node --test`): mono color filter, fuzzy matching, keybindings.
- Round trips against a real host (`make test-ssh`): ssh/tmux, working copies, live share,
  Claude backup and restore.
- Headless GUI tests driving the real app under Xvfb with screenshots (`make smoke-all`, 23
  scripts): terminal fidelity; sessions/dialogs/restart, add host, start-mode restart, one
  instance per config, Claude backups; Files; colors, switcher, settings/keys, reconnect,
  terminal extras (find, unicode widths, copy on select), folders, git markers, repository
  check, working copies, live share, notes and tab order, Web tab, layout, desktop notifications
  (fake notification service), notes for all sessions and copy/paste, badges; working copies and backups also with the built-in
  transfer.
  Each test checks its results (terminal output, saved config, files, window title, screenshot
  pixels for the layout) and fails on a wrong one; colors are judged from the screenshots.
  CI runs neither vet, `test-ssh` nor the GUI tests.

## Development setup
- All development, builds and tests run on the agent host (Claude's side).
- GUI verification there runs headless under Xvfb with screenshots; the "Local" tab then is a
  VM-local shell, which is fine for testing. Real use: copy the built binary to the host.
- Linux build deps: npm, libgtk-3-dev, libwebkit2gtk-4.1-dev, build-essential, pkg-config;
  for the GUI tests: xvfb, xdotool, imagemagick, tmux, rsync, sshfs, git, jq, dbus, python3-dbus, and
  `ssh localhost` without a password.
