# Session Workbench — Implementation Plan

See SPEC.md for requirements. Each milestone ends in something runnable.

## M0 — Skeleton (½ day) ✅
- `wails init` with vanilla JS template; Go module; Makefile/Taskfile (`dev`, `build`, `test`).
- Verify build on Linux; note Windows/macOS build prerequisites in README.

## M1 — One local terminal (1 day) ✅
- `internal/pty`: start process in PTY, read loop → batched events, Write, Resize, Close.
- Frontend: single xterm.js + fit addon wired to backend.
- Done when: bash/zsh runs fully (vim, htop, colors, resize) on Linux.

## M2 — Remote terminal via ssh + tmux (1 day)
- `internal/pathmap`: local→remote mapping + tests (incl. Windows paths).
- Remote command builder with quoting + tests; forced `ForwardAgent=no`/`ForwardX11=no`.
- Done when: tab runs `claude` in tmux on the agent host; closing/reopening the app re-attaches.

## M3 — Sessions + config (1–2 days)
- `internal/config`: TOML load/save (atomic), defaults, per-OS location.
- `internal/session`: CRUD, ordering, start modes, lazy terminal creation.
- Frontend: sidebar, per-session tab bar (Remote/Local/Files), new-session dialog (dir picker
  under workspace roots, start mode).
- Done when: 5+ sessions persist across restarts and switching is instant.

## M4 — Files tab (1 day)
- `internal/fsbrowse`: lazy ListDir (sorted, dirs first, hidden filter).
- `internal/opener`: xdg-open / open / start; refuse executables.
- Frontend tree, context menu (open, reveal, copy local/remote path, refresh).

## M5 — Keyboard + polish (1 day)
- Keybinding layer (configurable, per-OS defaults), quick switcher.
- Bell/activity badges; disconnect overlay + reconnect; kill remote session.
- Theme/font settings.

## M6 — Cross-platform (1–2 days)
- Windows: ConPTY, pwsh default, path mapping, `start`. macOS: Cmd bindings, `open`.
- CI build matrix (GitHub Actions) producing Linux/Windows/macOS artifacts.

## Testing
- Go unit tests: pathmap, command quoting, config round-trip, fsbrowse.
- Manual checklist per milestone (terminal fidelity, reconnect, restart restore).

## Notes on working in this setup
- All development, builds and tests run on the agent host (Claude's side).
- GUI verification there runs headless under Xvfb with screenshots; the "Local" tab then is a
  VM-local shell, which is fine for testing. Real use: copy the built binary to the host.
- Linux build deps: npm, libgtk-3-dev, libwebkit2gtk-4.1-dev, build-essential, pkg-config;
  for headless testing: xvfb, xdotool, imagemagick.
