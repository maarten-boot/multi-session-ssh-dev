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
