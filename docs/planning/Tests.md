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
| session     | F0–F3, F5 (folder paths, subtrees), R2 start modes, L4, T5, W1 links, B6 reviewed repository, §4.7 config problems |
| config      | §4.7 (file places, load errors, defaults, invalid values reset, README example); settings of F4, F7, F10, F12, N1b |
| remote      | R1, R2, R4, R5, L4                                                      |
| terminal    | L1, L2, T4                                                              |
| fsbrowse    | B1                                                                      |
| opener      | B2, B3                                                                  |
| gitstatus   | B5, B6, W1 (remote URL)                                                 |
| notes       | N1, N1c, N2                                                             |
| spell       | N1b                                                                     |
| forge       | W1                                                                      |
| qa          | Q1 (streaming, history sent back, refusals, errors, stop, store, log, model list; fake API; Claude Code and commands: arguments, resume, errors, stop, transcript, UTF-8) |
| backup      | F7                                                                      |
| instance    | F8                                                                      |
| xfer        | F9                                                                      |
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
| cmdmenu.test.mjs     | T5                                 |
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
| gui-smoke-folders.sh      | F5         | new folder, move into it, numbering, rename/reorder/delete   |
| gui-smoke-subfolders.sh   | F5         | nested folders: subfolder, move session and folder, delete   |
| gui-smoke-listmenu.sh     | F5, N2     | right-click menu on the empty session list                   |
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
| gui-smoke-qa.sh           | Q1         | Q&A against a fake API: answer, SVG, restart, Stop, log; model list, engine switch to a fake Claude Code (session resumed), right-click Copy/Paste; Markdown drawn, copied as Markdown (part of a bold word, Ctrl+C of everything) |
| gui-smoke-terminal-extras.sh | T1, T2  | find bar, emoji widths, copy-on-select                       |
| gui-smoke-colors.sh       | T3         | themes and color modes                                       |
| gui-smoke-settings.sh     | §4.6, §4.7 | configurable keys, settings dialog                           |
| gui-smoke-switcher.sh     | §4.6       | quick switcher: MRU, fuzzy filter, descriptions, resize      |
| gui-smoke-commands.sh     | T5, T6     | command menu (groups), start commands, prompts menu          |

When a feature is added, add or extend a test here and name the SPEC item it covers.
