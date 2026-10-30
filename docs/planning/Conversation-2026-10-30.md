# Conversation log — 2026-10-30

- **Status.** Clean, in sync with origin, v0.9.3 the latest tag and release.
- **Global session-documentation rules.** Decision: keep PLAN.md and SPEC.md at the top of the
  repository; add a daily conversation log and Tests.md (each test with the SPEC items it covers)
  in docs/planning/.
- **Branch rule.** A separate branch for log, plan and spec only in repositories not originally
  ours (so investigating doesn't pollute an existing branch). In our own repositories work on the
  normal branch and check for conflicts before implementing.
- **Cross-check of Tests.md** against the test functions: the Go mappings were partly guessed
  from package names; corrected (session also covers R2, L4, T5, W1, B6; config covers settings,
  not keys; private history is tested in remote, not terminal; fsbrowse has no B4 test;
  gitstatus covers the remote URL for W1; syncer covers F12 full transfers). README points to
  Tests.md.
