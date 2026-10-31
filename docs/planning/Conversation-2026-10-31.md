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
