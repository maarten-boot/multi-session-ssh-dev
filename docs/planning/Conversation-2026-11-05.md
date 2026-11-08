# Conversation 2026-11-05

- **Committed** da19054 (cross-check docs); pushed, GitHub main = da19054.
- **User:** "pushed and tagged v1.1.8, check the CI build" — the main build of b02e800 had
  already failed on windows-amd64 (linux and macOS passed): TestComposeDoc and TestCheckDev
  built the expected JSON by pasting t.TempDir() paths into strings; on Windows the
  backslashes are escaped in JSON. A test bug (container hosts are not on Windows). Fixed with
  json.Marshal for the paths; checked locally with backslashes in the test's project path.
  The memory note on Windows paths in tests has this case now.
- **User:** "yes bump to v1.1.9, commit, i push" — v1.1.9.
- **Committed** 69e7329 (v1.1.9). The first v1.1.9 tag pointed at da19054 (the commit had
  not been pushed); the user pushed main and moved the tag; the wrong build was cancelled.
- **v1.1.9 CI** (tagged on 69e7329): success on windows-amd64 (the compose tests pass),
  macos-universal, linux-amd64 and release; all five release files. The first release with
  services next to a container session (Compose).
