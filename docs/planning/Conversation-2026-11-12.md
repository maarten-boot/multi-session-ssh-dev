# Conversation 2026-11-12

## Tutorial (user: "Ok we should start working on a Tutorial: …; did i miss someting important ?")
- The user's outline: download; preparing shared data (NFS/CIFS), KVM virtio sharing, how
  Docker sharing works; the first start (no hosts), settings; hosts and their sharing; a new
  session and the first claude (only claude is supported, claude must be installed on the
  remote, the first time there the login may need to be confirmed); the remote has no GitHub
  write access by design, gh there with a limited token (with an example); the tabs, in
  particular Web and Notes (TODO alert); the global commands and prompts.
- Read "(ho hosts)" as "no hosts". Suggested additions: agent host requirements (ssh key,
  tmux, claude, rsync), live share and working copies, the review step and repository check,
  Files and Q&A tabs, cost/usage and idle stop, organising the list (groups, Active/Inactive,
  deactivate), notifications and badges, backups and recordings, switch host and export,
  keys/contrast/profiles/where files are, troubleshooting. Asked: where (docs/TUTORIAL.md), one
  file or chapters, screenshots, the token's rights.
- User: "yes add all, one file with screenshots, read-only token, super and as we near a
  release also aa pdf export if the tutorial as a final goal".
- User (while working): Markdown readability: a new sentence on a new line; a sentence longer
  than ~80 characters may continue after , ; : on a new line with a 2-space indent. Read it so
  and saved as a memory; the tutorial is written that way.
- User (while working): "you can use my command list and my prompt sessting as example".
  This machine is the agent host: the workstation's config and notes are not reachable here
  (only share-local is shared). Asked the user to copy the [[ui.commands]] part of config.toml
  and notes/_app/Prompts.md into the share; until then the tutorial uses made-up examples.
- Done: docs/TUTORIAL.md (18 sections), 20 screenshots in docs/tutorial/ (1.3 MB) by
  scripts/tutorial-shots.sh (`make tutorial-shots`), the PDF by scripts/tutorial-pdf.sh
  (`make tutorial-pdf`, build/session-workbench-tutorial.pdf, 31 pages); README links the
  tutorial; version 1.1.19.
- Screenshot fixes on the way: tmux's status line showed the real host name (now off in the
  demo's start command); tooltips drew as black boxes over menus (the mouse moves into the
  menu); the dialogs showed the demo's start command (restarted with the default ones).
- Found by the first screenshot: the empty window's *Add host…* button was white text on light
  grey: "#empty button" (an id rule) set the grey background over button.primary's blue, the
  white text stayed. Fixed in style.css; tutorial-shots checks the color (fails without the
  fix, passes with it).
- PDF: weasyprint and markdown-it-py in a venv under /tmp/multi-session-ssh-dev (nothing
  installed on the system). Python-Markdown was tried first and rendered lists after a line
  and code blocks in list items as text; markdown-it-py follows CommonMark like GitHub.
- Not yet: the PDF as a release asset in CI.

## Examples and the hidden cost (user: "2 example file in tmp/ one for propmts and one for commands ; also add that the cost in the settings menu is always updated even if we dont show it anywhere else.")
- tmp/example-settings.toml (the [ui] settings with 11 commands in groups Claude, Git, Text)
  and tmp/example-prompts.md (7 prompts): nothing private in them; copied to
  docs/tutorial/examples/ and used by tutorial-shots (commands menu, prompts menu, the
  Prompts page, Settings → Commands). The tutorial links them and describes the Claude and
  Text groups.
- "always updated even if we dont show it": checked the code first — it was not: with
  show_cost = false costRound returned at once and claude got no usage status line (the
  user's own settings have show_cost = false). Changed: the counting always runs, show_cost
  only hides the figure; cost_per_session works on its own; the hidden figure still sends
  the warnings' notifications. The Cost page's note says so; README, SPEC F18 and the
  tutorial too. gui-smoke-cost checks a reply counted after hiding (and its Save/Reset
  positions moved 16–18 px with the longer note). npm test 72 pass, go test pass.
- User (while working): "we could even consider adding them to the default release (after
  we correct speilling and accuracy ;-) )". Noted in PLAN Next; spelling proposals listed in
  the chat for the user to confirm.
- User: "yes commit, i push; use architecture/Crosscheck-YYYY-MM-DD-nn.md." The CrossCheck
  example prompt now names its file once, in that form. The spelling proposals wait for the
  user's OK.

## Cross-check (user: "pushed, do a crosscheck")
- 4bd8b06 is on GitHub; its CI run 43299227017 was queued.
- Versions agree: wails.json 1.1.19, SPEC status app v1.1.19, PLAN entry v1.1.19.
- Fixed: SPEC F17 still said the usage status line comes "with the cost shown" (now always);
  README's status line said v1.1.14 (now v1.1.19, with the cost and the tutorial); the
  tutorial's keys section gave Linux keys only (now says macOS uses ⇧⌘, see the README table).
- Checked against the code and README: no remaining show_cost-stops-counting text, no
  CostNow/usageOn left; the tutorial's shortcuts, defaults (backups hourly, idle stop off,
  start commands) and menus match; the PDF builds.
