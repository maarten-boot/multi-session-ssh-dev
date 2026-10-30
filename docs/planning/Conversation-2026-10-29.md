# Conversation log — 2026-10-29

- **Nested folders.** Request: multilevel folders in the sidebar (was Folder → host → session).
  Done as folder paths ("Work/Client A") in `groups` and a session's `group`; the sidebar shows a
  tree; New subfolder, Move folder to, rename/delete take the subtree along. New GUI test
  gui-smoke-subfolders.sh. Commit 9d64509, released as v0.9.1.
- **Cross-check.** The session dialog's folder list now shows full paths; README names the "/" in
  folder names and the folder path problems. Commit 63e56a2.
- **Dependabot (critical): github.com/labstack/echo/v4.** Only in Wails' dev server, not in release
  builds; updated to 4.16.1 with the golang.org/x modules it needs; Go 1.26+. Commit f8b136b,
  released as v0.9.2. CI for v0.9.1 and v0.9.2 passed (4 jobs, 5 release files each).
- **Window title.** Request: the current session's name after the app name. Now
  `(2) Session Workbench — alpha`; gui-smoke-badges.sh checks it. Commit 49b5415, version 0.9.3.
- **Cross-check** of the title change: nothing to fix.
- **v0.9.3** tagged; CI passed (4 jobs, 5 release files).
