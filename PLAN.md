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

## Testing
- Go unit tests for every internal package (pathmap, remote command quoting executed through a
  real shell, config, session, fsbrowse, opener, terminal incl. a ConPTY test on Windows) —
  `make test`, and in CI on Linux, Windows and macOS.
- Frontend unit tests (`node --test`): mono color filter, fuzzy matching, keybindings.
- ssh/tmux round trip against a real host: `make test-ssh`.
- Headless GUI tests driving the real app under Xvfb with screenshots (`make smoke-all`):
  terminal fidelity, sessions/dialogs/restart, Files, colors, switcher, settings/keys,
  reconnect, badges, terminal extras (find, unicode widths, copy on select), folders, git markers.

## Notes on working in this setup
- All development, builds and tests run on the agent host (Claude's side).
- GUI verification there runs headless under Xvfb with screenshots; the "Local" tab then is a
  VM-local shell, which is fine for testing. Real use: copy the built binary to the host.
- Linux build deps: npm, libgtk-3-dev, libwebkit2gtk-4.1-dev, build-essential, pkg-config;
  for headless testing: xvfb, xdotool, imagemagick.
