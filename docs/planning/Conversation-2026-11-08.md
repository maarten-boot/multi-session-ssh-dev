# Conversation 2026-11-08

- Earlier (2026-11-05): v1.1.9 CI success, all five release files (logged in
  Conversation-2026-11-05.md, not committed yet).
- **User:** "can we add a feature "Diactivate session" ? some sessions remain in this app but are
  there only for holding until further development is required. Thta means a inactive session
  does not need to reconnect to the remote (and to claude) when the app is restarted.
  Provbabely near the Disconnect entry in the session popup menue." — read as "Deactivate",
  "That", "Probably", "menu".
  - Found first: with connect_on_start = lazy (the default) a session connects when it is
    opened (the last one open at start, any one clicked or stepped through); with eager, all
    at start. So "inactive" has to cover opening it too.
  - Decided (questions): Deactivate disconnects and asks about the rest (end tmux / stop the
    container, unticked); an inactive session opens normally, its Local and Remote tabs show an
    Activate button instead of connecting; it stays in place, marked; no live share, no
    notifications, no remote history fetch.
  - Built (v1.1.10, SPEC F15): config `inactive`; session.SetActive, App.SetSessionActive (stops
    the live share); StartLocal/StartRemote/ensureShare refuse; no share mounted at start;
    Notify skips it; export's Remote history says it is inactive; frontend: the notice, menu
    items, the list mark, pastes and commands into it refused with a reason.
  - Tests: TestInactive; gui-smoke-inactive.sh (18 checks). The new menu row moved Delete
    session…: gui-smoke-hosts, -sync (keys d d d), -container, -compose (y 637) adjusted; all
    pass, as do reconnect and sessions.
- **User (while that was tested):** "in the levt columnt we show the remote hosts on the top can
  you group them ina foder named Hosts please." — decided (question): a heading *Hosts* above
  them in the list only. My choice: only when there are folders (without folders the whole
  list is hosts, and the heading would only push every row down). Collapses like a folder,
  takes dropped sessions out of their folder, right-click gives the list menu.
  gui-smoke-folders and -listmenu adjusted (a row lower while the heading shows); layout,
  subfolders and commands pass unchanged.
- **User (also while testing):** "if we would want to suuport development on different hosts (in
  the same project: like i work on one host at work and on another at home, is the current
  packup restore sufficient for that? …)" — answered: no (whole-host backups, restore
  overwrites all projects' Claude state on the other host, transcripts and memory filed by
  the project's path, one-way). Decided (questions): one laptop, two agent hosts; move this
  project's Claude conversations and memory, and the code by git; a requirements list per
  project checked on the host. In PLAN "Next" with the open points; not built yet.
- **Committed** 2ba3c15 (v1.1.10); pushed, GitHub main = 2ba3c15.
- **Cross-check after 2ba3c15:** version 1.1.10 everywhere; 40 GUI scripts in the Makefile and
  Tests.md; F15 and the Hosts heading (F5) match the code with their tests. Fixed: SPEC F1's
  session menu list (deactivate / activate), F3 (inactive sessions never connect at start),
  README's connect_on_start comment and Settings' "all at startup" option (not inactive
  sessions).
- **Committed** e57ac67 (cross-check docs); pushed and tagged v1.1.10.
- **v1.1.10 CI** (tagged on e57ac67): success on windows-amd64, macos-universal, linux-amd64
  and release; all five release files. The first release with inactive sessions and the Hosts
  heading.
- **User:** "start the two hosts feature, ask the questions" — checked first: Claude Code files a
  project's conversations and memory in `~/.claude/projects/<path with every non-alphanumeric
  character as ->/`, its trust and allowed tools under `projects["<path>"]` in ~/.claude.json;
  and since the app runs on one laptop, the project directory is there: shared folder and live
  share hosts need no code copied (a correction to the earlier "by git" answer, which the user
  accepted). Decided (questions): one session, several hosts (*Switch host…*); the code by the
  file mode; Claude state merged, newer wins; a requirements list with optional versions,
  checked on demand and before a switch.
  - Built (v1.1.11, SPEC F16): internal/handover (both sides fetched with the built-in
    transfer, newer files sent, the folder renamed for the new path, ~/.claude.json merged and
    replaced only if unchanged since fetched), internal/requirements, session.SetHost /
    ResolveOn / CanSwitch and `hosts`, switch.go, the Switch host… and Requirements… windows.
  - Found: `go --version` prints a usage error and `tmux` wants `-V` — the check now takes the
    first of --version, version, -V that succeeds, never an error text. My new window's id
    `dlg-switch` clashed with the quick switcher's: the window did not open and nothing said
    so; renamed, and unexpected errors in actions now show as a message.
  - Tests: TestSSHSwitch (two homes on localhost), the requirements and session tests,
    gui-smoke-switch.sh (9 checks; it touches only its own folders in ~/.claude/projects and
    never writes ~/.claude.json). Container and compose (Delete one row lower), record, colors,
    inactive, switcher, hosts pass; export and sync failed once while the container builds ran
    alongside (the menu did not open in time) and pass on their own.

## Cross-check v1.1.11 (user: "pushed, do a crosscheck")
- SPEC F16, README, PLAN and the code agree, except for inactive sessions: SPEC said "not for an
  inactive session", but the menu still offered *Switch host…* (the backend refused only at
  the switch, after the dialog had checked the requirements over ssh), and CheckRequirements
  went to the host of an inactive session (only the Requirements window guarded it), against
  F15's "fetches nothing from its agent host".
- Fixed: the menu hides *Switch host…* for an inactive session; CheckRequirements refuses it
  (ErrInactive). SPEC F15/F16 and README (Inactive sessions) say so. go test, vet, frontend
  tests, gui-smoke-inactive and gui-smoke-switch pass.

## CI v1.1.11 (user: "pushed and tagged v1.1.11, check the CI build")
- The tag points at 693df14. The main build of 75f29db failed on Windows: TestSetHost added a
  container host, which SaveHost refuses there ("containers as agent hosts need Linux or macOS
  for now"); the tag build runs the same test.
- Fixed in the test: the container host only off Windows (there "box" is an unknown host, still
  refused). go test passes; the test package builds for Windows.
- CI: linux and macOS passed, windows failed (TestSetHost), release skipped: no v1.1.11 release.
- User: "yes bump to v1.1.12, commit, i push" — version 1.1.12 (wails.json, SPEC, README,
  CONNECTIVITY, PLAN).

## CI v1.1.12 (user: "pushed and tagged v1.1.12, check the CI build")
- The tag points at 00a620e. Run 42519959124: linux, windows, macOS and release passed; the
  release has the five assets (rpm, deb, linux tar.gz, macOS zip, Windows zip).

## Stopping an idle claude (v1.1.13, SPEC F17)
- User: "can we detect when the claude process on the remote is ready for next input but no
  input is coming for a configurable time (say 5 minuts) would it then make sense to halt the
  claude process and resume when the user switches back to the session ?"
- Answer: yes, by Claude Code hooks; pausing (SIGSTOP) gains nothing (memory stays), ending and
  `claude --resume` frees ~300–400 MB per claude (measured: 7 here). Recommended opt-in.
- User: "yes build it, ask the questions". Answers: global with a per-session override; off by
  default; resume when the session is opened; a 💤 mark in the list.
- Checked first with a real claude in a private tmux server: hooks via --settings fire, $PPID is
  claude, $TMUX_PANE set, SessionStart carries the conversation id, an idle claude stops
  redrawing after ~25 s, respawning the pane ends it (SessionEnd); without a prompt there is no
  transcript (nothing to --resume: the start command again).
- Built: internal/idle (hooks, start command rewrite, check/stop/resume scripts), idlestop.go
  (a check per host every minute, SessionShown, IdleStopped, events), config `idle_stop` (ui and
  session), Settings → Terminals, Edit…, the 💤 mark, frontend/src/idle.js.
- Found while testing: (1) after the respawn the start command's interactive shell survived
  and ran `|| claude` outside the pane (an orphan claude) — the shells are frozen before the
  respawn and killed after; (2) a session that had been on screen for a long time was stopped
  as soon as another was opened — idle now counts from leaving the screen too.
- Tests: idle unit tests and TestSSHIdle (fake claude, real tmux; 6 runs in a row), config and
  session tests, idle.test.mjs, gui-smoke-idle.sh (18 checks); settings, inactive, commands,
  layout, listmenu, switch GUI tests pass. Once by hand with a real claude: stop left nothing
  running, resume with --resume worked (its `--continue` picked this very conversation in the
  repo folder; no prompt was sent).

## Cross-check v1.1.13 (user: "pushed, do a crosscheck")
- SPEC F17, README, Settings and Edit… agree with the code (hooks, the idle rules, the session
  on screen and the one that left it, stop, resume, the 💤 mark, the limits).
- Gap: importing a session (F13) did not carry its own `idle_stop`, while record and history
  come along. Fixed in export.go (an invalid value is dropped with a note); SPEC F13 lists the
  idle time; gui-smoke-export.sh checks it (37 checks pass).

## CI v1.1.13 (user: "pushed and tagged v1.1.13, check the CI build")
- The tag points at 925aab9. Run 42528238582: linux, windows, macOS and release passed; the
  release has the five assets (rpm, deb, linux tar.gz, macOS zip, Windows zip).
