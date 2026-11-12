# Session Workbench — Tests

Every test, with the SPEC.md items it covers. PLAN.md "Testing" says how the tests work;
README.md "Development" lists the make targets.

| Run                                 | What                                                     |
|-------------------------------------|----------------------------------------------------------|
| `make test`                         | Go unit tests (race detector) + frontend unit tests      |
| `make test-ssh SSH_TARGET=localhost`| Go tests against a real ssh/tmux host                    |
| `make smoke-all`                    | all headless GUI tests (xvfb + xdotool), each checks its results |

## Go unit tests (`internal/…`)

| Package     | SPEC items                                                              |
|-------------|-------------------------------------------------------------------------|
| handover    | F16 (TestEncode: Claude Code's folder names; TestChanges: missing or older goes, newer stays; TestMergeConfig: the entry under the new path, allowedTools joined, trust kept, other keys untouched, numbers as written, no file / no entry / bad JSON; TestSSHSwitch with SW_SSH_TARGET: two homes, different paths, conversations, tool results and memory copied, newer and only-there files kept, times kept, ~/.claude.json merged 0600, a second switch sends nothing) |
| cost        | F18 (TestDollars: prices per model, cache writes and reads, fast, a long prompt, longest prefix, own and overriding prices, a model without a price; TestParseReply: model/id from the head, usage/time from the tail, not from text or tool input in the content; TestScript, sh here: the project folder (a symlink) and the hooks' transcript folder, subagent transcripts, a reply's later line replaces it, a line being written waits, only new bytes read, nothing new reads nothing, reset (today stays), a shrunk transcript keeps its counts, saved and loaded; also passes with busybox tools only; TestRealTranscripts with SW_COST_REAL: this machine's transcripts, checked against a Python count); TestScript also: the status line helper installed (executable) and the saved usage read with its age |
| idle        | F17 (TestProcsWithoutPS: the process helpers read /proc where there is no ps; TestSettings: the hooks JSON, matchers, safe in any shell's single quotes; TestWithHooks: every claude command word, not in arguments or quotes; TestResumeCommand: the first claude with its options, conversation options replaced by --resume, the start command without a conversation; TestParseAndIdle: idle only after Stop/SessionStart, quiet, alive, nothing running, own tmux session; TestScripts: ids and panes checked; TestSSHIdle with SW_SSH_TARGET: a fake claude in real tmux — hooks, busy with a Bash tool command, stop leaves nothing running (no fallback claude), notice in the pane, resume with --resume, a shell taken over by hand is left alone, a stale mark goes, a busy claude stopped by force leaves nothing running); F18 TestStatusLine: the status line only with usage, the data saved only when it has rate_limits, the usage shown without an own status line, the user's own command (~/.claude/settings.json) run with the same data, no files left |
| requirements | F16 (TestParse: the template, operators with and without spaces, bad lines with their numbers; TestCompare; TestEvaluate: found, too old, missing, unreadable version, go's two-step output; TestScript: sh here) |
| session     | F17 (TestUpdateIdleStop: own idle time saved, 10s refused, left out kept), F16 (TestSetHost: the path on the other host without saving, container/plain/unknown hosts refused, hosts after a switch and back, a renamed host renamed in hosts), F15 (TestInactive: deactivate, state, Resolve, the file, kept by Edit…, activate, unknown session), F2b (tmux: host default, overrides stored only when different, Edit host/yes/no/left out, bad value), F2a (plain logins: host default and both overrides, home or remote_dir, names, Edit, no local_path, no directory problems, Dir refused, container hosts refuse), C1 (container host rules, resolving to sw-<id>, own volume kept), F0–F3, F5 (folder paths, subtrees), R2 start modes, L4, T5 (also where: local, remote, nothing else), W1 links, B6 reviewed repository, §4.7 config problems |
| config      | F18 (TestPrices: own prices read, a negative or incomplete one dropped, show_cost on by default, cost_per_session), F17 (TestIdleStop: durations 1m–24h, off, session over global, an invalid global reset), §4.7 (file places, load errors, defaults, invalid values reset, README example); settings of F4, F7 (also BacksUp: per host, off for plain-login hosts), F10, F12, N1b; F14 (a profile's config and data folders); TestPrices also: the warning limits (dollars, percent) checked, invalid ones reset |
| remote      | F2b (PlainArgs: no tmux, the folder, history, bad folder or target), R1, R2, R4, R5, L4, C4 (container options for scripts and kill, not attach), F14 (tmux, history and fish names with a profile)                                                      |
| terminal    | L1, L2, T4                                                              |
| fsbrowse    | B1                                                                      |
| opener      | B2, B3                                                                  |
| gitstatus   | B5, B6, W1 (remote URL)                                                 |
| notes       | N1, N1c, N2                                                             |
| spell       | N1b                                                                     |
| forge       | W1                                                                      |
| qa          | Q1 (streaming, history sent back, refusals, errors, stop, store, log, model list; fake API; Claude Code and commands: arguments, resume, errors, stop, transcript, UTF-8) |
| backup      | F7 (also: the agent host's "no rsync" recognised; with SW_SSH_TARGET a fake rsync that reports none: backup and restore through the built-in transfer, noted and remembered) |
| container   | F17 (TestRealIdle with SW_CONTAINER_TEST: a fake claude in a real container's tmux — hooks, Stop claude now, nothing left, resume with --resume), F14 (names with a profile; the shared volume stays), C2–C6 (build and run arguments, keys and pinned host key, extra CAs, Claude Code binary, which Dockerfile, state/port/stop/remove with a fake engine), C7 (TestComposeDoc: the app's dev service; TestComposeFile: which file, the compose arguments; TestCheckDev: what dev must keep; TestCheckCompose: compose config next to the file, its error naming it; TestComposeEnv: Docker nothing, Podman's own socket, else one the app starts; TestComposeProject: stop/remove reach the project's containers, networks and, with data, volumes; TestSetEnv; TestNetnsDenied); with SW_CONTAINER_TEST=podman or docker a real build, ssh, ids, mount owner, stop/start, and TestRealCompose (a service by name, dev's environment in the ssh login, a relative path in the file, a second Up keeps the port, Stop stops all, Remove leaves nothing); on Windows the tests that need a mountable path skip (container hosts are Linux/macOS; the session test checks Windows refuses them) |
| instance    | F8                                                                      |
| xfer        | F9                                                                      |
| profile     | F14 (default names, a name's folder, tag and title, bad names refused and not taken) |
| sessionarchive | F13 (.zip and .tar.gz round trip, existing files kept, empty directories back; hostile archives: "..", absolute names, symlinks, no manifest, newer format, not an archive; project files: tracked, untracked, .git with its directories, not ignored; without git all; executable bits kept, skipped on Windows) |
| reclog      | F10                                                                     |
| syncer      | F11, F12 (full transfers)                                               |
| autosave    | F12                                                                     |
| savedcopy   | F12                                                                     |
| share       | live share (`files = "sshfs"`, docs/CONNECTIVITY.md), §6                |
| pathmap     | live share and working copies (path prefixes, §4.7)                     |
| winstate    | §4.7 window size and position                                           |

## Frontend unit tests (`frontend/src/*.test.mjs`)

| File                 | SPEC items                         |
|----------------------|------------------------------------|
| appearance.test.mjs  | T3 (themes, color modes, font size) |
| cmdmenu.test.mjs     | T5 (also Back to the session directory: quoting, control characters; Local/Remote sections: which list, sent where, disabled, unknown where left out) |
| dockerfile.test.mjs  | C6 (toolchain blocks before the needed part, valid continuations) |
| services.test.mjs    | C7 (service blocks: `services: {}` becomes a section, the volume under volumes:, existing sections and what follows kept, twice changes nothing, an empty file) |
| cost.test.mjs        | F18 (dollars, the top bar figure and tooltip, ≥ per amount with a model without a price, the session's balloon line); the usage in the figure (its age after 2 min), a reset window at 0%, warnings per limit, the usage sentence, the Cost page rows and sum |
| idle.test.mjs        | F17 (idle times in words, the choices of the Settings and Edit… selects, a time set in config.toml kept) |
| qaparse.test.mjs     | Q1 (text, code and SVG parts, engine names, token line, Markdown parsing, copying as Markdown) |
| prompts.test.mjs     | T6, N2                             |
| keys.test.mjs        | §4.6                               |
| fuzzy.test.mjs       | §4.6 quick switcher                |
| format.test.mjs      | B1 (sizes, dates)                  |
| lists.test.mjs       | N1a (lists, check lists)           |
| markup.test.mjs      | N1a                                |
| spelling.test.mjs    | N1b                                |

## Headless GUI tests (`scripts/gui-smoke-*.sh`)

| Script                    | SPEC items | Checks                                                       |
|---------------------------|------------|--------------------------------------------------------------|
| gui-smoke.sh              | T1, R1     | terminal fidelity (colors, Unicode, full-screen programs)     |
| gui-smoke-sessions.sh     | F1, F2, F3, §4.6 | sidebar, new-session dialog, switching keys, restart    |
| gui-smoke-hosts.sh        | F0, F2, R4 | empty config, add host, session menu, move, delete + end tmux |
| gui-smoke-startmode.sh    | R2         | changing the start mode restarts the remote tmux session     |
| gui-smoke-folders.sh      | F5         | new folder, move into it (the session under the Hosts heading), numbering, rename/reorder/delete |
| gui-smoke-subfolders.sh   | F5         | nested folders: subfolder, move session and folder, delete   |
| gui-smoke-listmenu.sh     | F5, N2     | right-click menu on the empty session list, and on an empty folder's line below the Hosts heading |
| gui-smoke-description.sh  | F1         | description balloon and editing                              |
| gui-smoke-badges.sh       | F4         | bell/activity badges, window title (count + active session)  |
| gui-smoke-notify.sh       | F4         | desktop notifications                                        |
| gui-smoke-layout.sh       | F6, T3     | font size everywhere, current tab, narrow/resized sidebar    |
| gui-smoke-window.sh       | §4.7       | window size and position remembered                          |
| gui-smoke-instance.sh     | F8         | one instance per config                                      |
| gui-smoke-backup.sh       | F7         | Claude backups                                               |
| gui-smoke-record.sh       | F10        | Remote tab recordings                                        |
| gui-smoke-sync.sh         | F11, F9    | working copies: Send, Get, branches                          |
| gui-smoke-autosave.sh     | F12        | automatic agent copy saves, save at quit                     |
| gui-smoke-share.sh        | live share, §6 | sshfs mount, git through it                              |
| gui-smoke-reconnect.sh    | R3         | disconnect overlay, reconnect                                |
| gui-smoke-history.sh      | L4         | private shell history                                        |
| gui-smoke-files.sh        | B1–B4      | Files tab tree, sort, open                                   |
| gui-smoke-git.sh          | B5         | git markers                                                  |
| gui-smoke-repocheck.sh    | B6         | repository check, mark as reviewed                           |
| gui-smoke-notes.sh        | N1, N1a    | tab order, page tree, autosave, formatting                   |
| gui-smoke-spell.sh        | N1a, N1b   | check lists, bullets, spelling suggestions                   |
| gui-smoke-todo.sh         | N1c        | TODO marks                                                   |
| gui-smoke-appnotes.sh     | N2, T2, T6 | notes for all sessions, prompts page, copy/paste             |
| gui-smoke-web.sh          | W1         | Web tab links, build runs, tab order                         |
| gui-smoke-qa.sh           | Q1         | Q&A against a fake API: answer, SVG, restart, Stop, log; model list, engine switch to a fake Claude Code (session resumed), right-click Copy/Paste; Markdown drawn, copied as Markdown (part of a bold word, Ctrl+C of everything); Settings → Q&A (Claude Code's model saved and used) |
| gui-smoke-inactive.sh     | F15        | connect_on_start = eager: the active session connects, the inactive one not; opening it (Local, Remote) starts no tmux session and no Local shell; its notice's Activate session connects it and clears inactive in config.toml; Deactivate session… with the tick ends its tmux session and stores inactive; after a restart (it was the last one open) nothing connects; Deactivate without the tick leaves tmux running and ends its Local shell; Activate session from the menu; deactivating the session on screen does not scroll the window (its notice was put below the terminal); the tmux tick starts ticked |
| gui-smoke-lists.sh        | F5, F15    | with folders: Hosts and Groups headings; Active and Inactive collapsed at first, opened: sorted by name, no numbers; a row in Active opens its session (its Local tab); the balloon in Inactive adds host and folder; a row in Inactive opens it and Activate session moves it to Active (Inactive left out when empty); opened headings stay open after a restart |
| gui-smoke-cost.sh         | F18        | own -profile (the real usage file untouched); made-up transcripts in the session's real project folder and a usage file (removed afterwards), checked every 3 s: the helper installed, the figure with the usage, the session's balloon, the subagent transcript, a new reply, a model without a price (≥); the figure opens Settings → Cost (table, sum, Top bar row, usage); a daily and a 5-hour limit saved and shown with ⚠; Reset the total… from the page; turned off (show_cost = false): the figure goes, a new reply is still counted (ledger, Settings → Cost) |
| gui-smoke-idle.sh         | F17        | idle_stop = 1m, checks every 3 s, a fake claude: the session not on screen is stopped after a minute (nothing left running, notice in the pane, 💤 in the list), the one on screen is not; opening it resumes it with --resume <conversation>, not --continue; the session that left the screen is not stopped at once; Stop claude now on the session on screen (stopped at once, stays stopped while on screen), Resume claude, a busy claude stopped only after the question (and the command it ran ends); Settings → Terminals saves never |
| gui-smoke-switch.sh       | F16        | two hosts on localhost with different path mappings: Requirements… checks the project's list (✓ sh, ✗ a missing tool); Switch host… shows the target's path and requirements, ends tmux, moves the session (host, hosts in config.toml), the conversation and memory go to the folder for the new path, the old host keeps its copy; switching back brings a conversation written at home along; only the test's own folders in ~/.claude/projects, removed after |
| gui-smoke-login.sh        | F2a, F2b, F7 | a host with local_dir = false: its session has no Local tab (the Local key leaves the keys in Remote), the shell starts in remote_dir; New session there is a login named after the host, starting a shell in the home folder, no local_path in the config; Add host… with Sessions on this host (4th field) = plain logins and tmux = no saves local_dir = false and tmux = false, and no backup line (it follows the logins: off); a host with tmux = false: a plain login in its remote_dir without $TMUX, no tmux session |
| gui-smoke-profile.sh      | F14        | SESSION_WORKBENCH_PROFILE=ssh-switcher without -config: its own config folder, the title names it, its tmux session cw-ssh-switcher-<id>, window.json in its data folder, nothing in the session-workbench folders; -profile wins over the variable; a bad name exits 2 with the reason |
| gui-smoke-export.sh       | F13, C7, F17 | the session's own idle time (idle_stop) comes along; the project's compose file in the archive (compose.yaml, compose_in_repo); also the same as .tar.gz (Format in the dialog) imported into a third directory (working repository, notes); session menu → Export… (project ticked) → .zip via the save dialog: manifest, notes, Q&A and its log, recordings, both histories, project with .git, not git-ignored files, no keys; + → Import session… → new id, new directory with a working repository, notes, Q&A without Claude Code ids, recordings, Local history, own commands, local_init left out; the Remote history pushed when the Remote tab connects |
| gui-smoke-dockerwin.sh    | C6         | the Dockerfile window (no container made): dragging the heading moves it, the corner makes it larger, both kept when opened again; the right-click menu shows inside the modal window — Copy then Paste through it puts a line in twice, Save writes it |
| gui-smoke-compose.sh      | C7         | real Podman/Docker with compose: the Services window's Redis button and a variable for dev, Save writes the file, a renamed dev container is refused; the Remote tab starts dev and Redis, the shell has the variable and reaches redis by name; Save and restart services adds a service with a data volume without making dev again; Stop container stops all, Reconnect starts all; delete with the data tick leaves no container, network or volume |
| gui-smoke-container.sh    | C1–C6      | real Podman/Docker: build in the Remote tab, you and the path inside, Claude Code there, ssh only on 127.0.0.1, own volume; Dockerfile window save, Save and rebuild, Stop and Reconnect, delete removes container, image, volume, files |
| gui-smoke-terminal-extras.sh | T1, T2  | find bar, emoji widths, copy-on-select                       |
| gui-smoke-colors.sh       | T3         | themes and color modes                                       |
| gui-smoke-settings.sh     | §4.6, §4.7 | configurable keys, settings dialog                           |
| gui-smoke-switcher.sh     | §4.6       | quick switcher: MRU, fuzzy filter, descriptions, resize      |
| gui-smoke-commands.sh     | T5, T6     | command menu (groups), start commands, prompts menu; a `where = "local"` command chosen on the Remote tab runs in the Local shell (started again after `exit`) and the Remote tab keeps the keys; a `where = "remote"` one chosen on the Local tab runs in the Remote shell and the Local tab keeps the keys; Settings → Commands (with Where); Local: Back to the session directory after `cd /` |
| tutorial-shots.sh         | T3, docs   | the screenshots of docs/TUTORIAL.md (made-up host and sessions on localhost, profile "tutorial", nothing left behind); the empty window's *Add host…* button is blue (white text on light grey before v1.1.19) |

When a feature is added, add or extend a test here and name the SPEC item it covers.
