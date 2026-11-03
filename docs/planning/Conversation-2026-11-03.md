# Conversation 2026-11-03

- **User:** "What is next ?" — PLAN "Next": echo/v4 (mid-November), output folding (postponed).
  Offered small follow-ups: the Edit… dialog's command column (squeezed by Where), quitting
  while a native file dialog is open, untested paths (.tar.gz import, a Remote command from
  the Local tab, starting the other tab when it is not running).
- **User:** "do the Edit… dialog layout and the extra tests"
  - Edit… dialog: group, name, run, where and ↑ ↓ ✕ on one line, the command on its own full
    line below (CSS grid areas; the dialog keeps its width, so no test coordinates move).
  - gui-smoke-commands.sh: a `where = "remote"` command chosen on the Local tab runs in the
    Remote shell and the Local tab keeps the keys; the Local shell is ended (`exit`) before a
    Local command is chosen on the Remote tab, which starts it again. Settings' + Add moved one
    row down (one more command in the test config).
  - gui-smoke-export.sh: the same export as .tar.gz (Format in the dialog), imported into a
    third directory. GTK's file dialogs complete paths while they are typed: names that
    share a start with another file in the folder get garbled (`s.zip` next to `s.tar.gz`
    became `s..zip`) — the test uses `t.tar.gz` and removes old archives first. 33 checks pass.
- **User (while that ran):** "currently the app saves its data at: ~/.config/session-workbench,
  what if i want to use the app for 2 complete separate experiences: the default for AI
  supported development, but a second location for a simple ssh switcher for remote logins.
  Could we add a cli option so save all data in a different name (default session-workbench)
  but optional ssh-switcher ? Either via cli param or perhaps via a environment variabebel ?"
  - Decided (questions): the name replaces session-workbench in the folder names; both
    `-profile` and SESSION_WORKBENCH_PROFILE (the flag wins); on the agent hosts the tmux
    sessions and the history folder carry the name; the title names the setup.
  - Built (v1.1.4, SPEC F14): internal/profile; config folders, tmux `cw-NAME-<id>`, history
    `~/.local/state/NAME/history`, fish history names, containers `sw-NAME-<id>` (the shared
    login volume stays shared), the container helper gets `-profile`, the title and About,
    Linux WM_CLASS. My additions beyond the answers: containers carry the name too (same
    clash as tmux); the browser storage keys get a `sw:NAME:` prefix — on Linux WebKit's
    storage folder does not follow Wails' program name (WebKit picks it before Wails sets
    it), so the storage stays shared and the keys keep the setups apart (also on macOS and
    Windows); I dropped a WebView2 folder option for Windows as unneeded.
  - Tests: profile, remote, config and container unit tests; gui-smoke-profile.sh (variable,
    flag wins, folders, title, tmux name, bad name exits 2) passes.
- **User (while the profile was tested):** "note: when using the app only for ssh remote
  sessions there is no need to share the local directory with the remote side so we should
  probabely make it optional (defauly yes)" — decided (questions; in PLAN "Next"): per host
  with a per-session override; the Remote tab starts in home or an optional remote folder;
  tabs Remote, Notes, Web, Q&A; the name defaults to the host's name. Not built yet.
- Regression after the profile: badges, window, sessions, history, instance, layout,
  switcher, dockerwin and container GUI tests pass; 60 frontend tests, go test, cross vet.
- **Committed** 1b8401b (v1.1.4: profiles, Edit… layout, tests); pushed.
- **User:** "pushed, start" — built plain ssh login sessions (still v1.1.4, not tagged):
  config `local_dir` (host, session) and `remote_dir`; session.Resolved.Plain/Dir/LogPath;
  the backend refuses directory actions for logins; Local/Files tabs hidden; the session and
  host dialogs; sidebar `ssh login: <folder>`; export/import keep a login a login.
  - My choices beyond the answers: a new plain login preselects a start command without a
    command (a shell), not the last mode (which stays for directory sessions); the local
    directory choice is fixed once the session is made (like host and directory);
    recordings and the Q&A log of a login go under `<logs>/<host>/logins/<id>/`.
  - Found in the GUI test: the sidebar line "ssh login: ~" showed as "~ :ssh login" (the
    list shows paths right-to-left to cut them at the start) — fixed with a closing mark.
  - Tests: TestPlainLogin, gui-smoke-login.sh (9 checks) pass.
- **Committed** 9f6eb62 (plain ssh logins); pushed, GitHub main = 9f6eb62.
- **Cross-check after 9f6eb62:** version 1.1.4 everywhere; all 38 GUI tests in the Makefile
  and Tests.md; F2a and F14 match the code with their tests. Fixed: SPEC F1 (the list line of
  a login) and F2 (the dialog's Local directory step), README's recordings row (logins go to
  logs/<host>/logins/<id>/) and Edit… list (a login's remote folder), and CONNECTIVITY now
  says plain logins share no files.
- **v1.1.4 CI** (tagged): failed on windows-amd64 (TestPlainLogin: "host "jump" has no path
  mapping and "C:\…\proj" is not a POSIX path" — a test bug, not the app); linux and macOS
  passed; no release. Fixed: the test host gets prefixes. **User:** bump to v1.1.5.
- **v1.1.5 CI** (tagged): success on macos-universal, windows-amd64 (TestPlainLogin passes),
  linux-amd64 and release; all five release files. The first release with -profile and
  plain ssh logins.
- **Cross-check after v1.1.5:** only the test fix and the version changed since the last
  cross-check; 1.1.5 everywhere, PLAN explains v1.1.4. No differences.
- **User:** "when adding a host after name and login move the sessions on this host as 4th
  item as when we say just ssh we can gray out the local folder"
  - Done (v1.1.6): *Sessions on this host* right after Name, Kind and SSH target; *are plain ssh
    logins* disables File access, the prefixes, Browse… and the working folder (the app's
    disabled style: dashed border, text stays black — no grey text). They stay visible, as a
    session on that host can still choose a directory.
  - gui-smoke-login.sh: Add host… → Sessions on this host = plain logins → Save writes
    local_dir = false (Enter on a select does not submit: the test clicks Save). Passes, as do
    gui-smoke-hosts and gui-smoke-sessions.
- **User:** "make also a option for plain ssh logins not to have a tmux (vmware hosts have no
  tmux)" — decided (questions): per host with a per-session override; for any session;
  Reconnect starts a fresh shell (no confirmation on close).
  - Built (still v1.1.6, SPEC F2b): config `tmux` (host, session); remote.PlainArgs (ssh -t,
    cd or the missing-directory exit, history env, the start command); KillRemote does nothing
    without tmux; no End remote session…, no tmux tick in the delete dialog, no restart offer;
    the path line says "no tmux"; Add/Edit host *Keep sessions in tmux on this host*, New
    session / Edit… *Keep it in tmux* (default, yes, no).
  - Tests: TestPlainArgs, TestNoTmux; gui-smoke-login.sh (16 checks) passes: a tmux = false
    host gives a plain login in /var/tmp with an empty $TMUX and no tmux session; Add host…
    saves tmux = false.
- **Committed** 0968c94 and c235069 (v1.1.6); pushed, GitHub main = c235069.
- **Cross-check after c235069:** version 1.1.6 everywhere; F2a/F2b match the code with their
  tests. Fixed: SPEC R2 still said tmux is mandatory (now: the default, `tmux = false` runs
  without), R4 (no End remote session without tmux), the SPEC status line (F2a, F2b), and the
  README's session menu list (End remote session… not for sessions without tmux).
- **v1.1.6 CI** (tagged): success on windows-amd64, macos-universal, linux-amd64 and release;
  all five release files. The first release with sessions without tmux.
- **Cross-check after the v1.1.6 release:** nothing changed since d0db5c4 (only this log);
  1.1.6 everywhere, PLAN has its entries. No differences.
- **User:** "similar item with rsync a vmware has no rsync so a remote backup can never be done
  and as it is a ssh only what would we need to replicate ?"
  - Answer: nothing — the Claude backup copies only Claude Code's data (~/.claude,
    ~/.claude.json), and an ssh-only host has no claude; the session's notes, Q&A and
    recordings are on this computer. Today the backup tried every ssh host anyway: rsync
    failed there (no rsync on the host), the built-in transfer would stop at "no ~/.claude".
  - Decided (questions): a per-host switch, off for plain-login hosts; and when an agent host
    lacks rsync, fall back to the built-in transfer automatically.
  - Built (v1.1.7): config `backup` on hosts and config.BacksUp; the backup runner skips hosts
    that are off; the header tooltip says so, no ⚠, no backup items in the host menu; Add/Edit
    host *Back up Claude data from this host* (follows Sessions on this host until changed,
    stored only when different); internal/backup recognises "rsync: (command )?not found",
    runs the built-in transfer for backup and restore, notes it and remembers the target.
  - Tests: TestBacksUp, TestRemoteNoRsync, TestSSHNoRemoteRsync (passes with
    SW_SSH_TARGET=localhost), gui-smoke-login.sh.
