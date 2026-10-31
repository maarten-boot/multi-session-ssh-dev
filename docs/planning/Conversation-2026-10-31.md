# Conversation log — 2026-10-31

- **v0.9.4 CI** (tagged 2026-10-30; the polling stopped when the previous session ended):
  checked again — success on macos-universal, linux-amd64, windows-amd64 and release; all five
  release files. First release with the Anthropic Go SDK.
- **Cross-check** (code ↔ SPEC/README/Tests.md, after v0.9.4): no differences. Checked: Q1 in
  SPEC (model, effort, cache, fallbacks, what is sent back, store, log, SVG, Stop) against
  internal/qa and qa.go; README Q&A section, shortcut (Ctrl+Shift+6 = 6th tab by position),
  config example, data locations; version 0.9.4 everywhere; Tests.md lists every Go package,
  frontend test file (all in `npm test`) and GUI test script. Note only: `QAInfo.busy` is
  sent but the tab keeps its own busy state (its view lives as long as the session), so it
  is unused — harmless.
- **PLAN "Testing" brought up to date** (missed by the cross-check): 34 GUI test scripts (not
  29), and the Q&A tests in the Go, frontend and GUI lists.
- **Q&A without a token, model choice, copy/paste (v0.9.5).** Request: allow token-less AI
  agents in the Q&A tab; let the model be changed (the bar only showed it); a copy/paste
  popup on selected text like the Notes tab. Decisions: Claude Code CLI (its own login) and
  any CLI command; the model list live from the API (Claude Code: its aliases). Built: engine
  and model selectors kept per session (`qa_engine`, `qa_claude`, `[[ui.qa_commands]]`),
  changing the engine starts a new conversation; Claude Code runs `claude -p` with no tools,
  no settings/hooks/MCP, in `<data dir>/qa/work`, resuming its session; right-click menus in
  the turns and the question box. Tests: Go (fake claude/command via the test binary, model
  list), qaparse, gui-smoke-qa.sh extended; one real Claude Code round trip (haiku). A first
  GUI run passed falsely (nothing selected, the check matched an older question): fixed to
  select a word that is never asked.
- **Cross-check of v0.9.5.** README's Q&A intro still said "through the Claude API" and that
  each question costs money on the API account: now names the three engines and what each
  costs; package list likewise. The empty Q&A tab said "Ask Claude" (also shown for own
  commands): now "Ask a question … to the engine chosen above". Own commands got stopped or
  failed partial answers in their transcript, unlike the API engine: now left out (tested),
  SPEC says so. Checked as matching: SPEC Q1 flags and folders, model lists, default engine
  rule, data locations, config example (tested), Tests.md and PLAN.
- **Dependabot: vite 6.4.4 → 6.4.5** (one alert, frontend/package-lock.json). Bumped locally
  (`npm install -D vite@6.4.5`), `npm audit` clean, frontend tests, build and GUI tests pass;
  the Dependabot PR becomes unnecessary.
- **v0.9.5 CI** (tagged): success on macos-universal, linux-amd64, windows-amd64 and release;
  all five release files. The new Q&A engine tests (fake claude and command through the test
  binary) pass on all three platforms.
- **Cross-check after v0.9.5:** no differences. Since the last cross-check only vite changed
  (not named in the docs); version 0.9.5 everywhere, 34 GUI test scripts as PLAN says, local
  main equal to GitHub.
- **Output folding:** not now; when built, the reading view's text comes from the terminal
  scrollback (noted in PLAN "Next").
- **Dependabot alerts** (readable after a token with "Dependabot alerts: Read-only"; the first
  token pasted into the conversation was revoked): 3 open, medium. #8 vite: fixed on main
  (6.4.5), waits for Dependabot's rescan. #6/#7 echo/v4 (≤ 4.16.2, no fix yet): indirect via
  Wails' dev server only, not in the release binary, the affected proxy/rate-limiter
  middleware unused — dismiss or wait for a fixed echo.
- **Cross-check:** no differences; since the last one only PLAN's output-folding note and
  this log changed, and they agree.
