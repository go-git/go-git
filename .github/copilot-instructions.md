# Copilot review instructions

When reviewing pull requests in this repository, focus on correctness, maintainability, test quality, and compatibility with upstream Git behavior.

## Review priorities

- Prefer actionable review comments that explain the risk and suggest a concrete fix or follow-up.
- Flag correctness, security, compatibility, or maintainability issues over minor style preferences.
- Avoid low-value style comments unless they affect readability, idiomatic Go, or long-term maintenance.

## Tests

- Prefer table-driven tests.
- Tests should focus on meaningful use cases and edge cases rather than duplicating implementation details.
- Flag tests that introduce unnecessary boilerplate, excessive setup, or avoidable test-code bloat.
- When behavior is added or changed, check that relevant failure cases and boundary cases are covered.

## Git compatibility

- Any Git-specific behavior must be checked against the upstream Git implementation, `git/git`.
- Flag PRs where Git behavior appears to be assumed rather than verified.
- Highlight any gaps, ambiguities, or behavioral differences so a human reviewer can double-check them against upstream Git.

## Repository contents

- PRs should not add large files or binaries to the repository.
- Flag newly added binaries, generated artifacts, vendored blobs, archives, or unusually large files.
- Watch for files added in one commit and removed in a later commit within the same PR, since they still enter the repository history.
- Highlight cases where large files or binaries may be concealed by later deletion in the same PR.

## Go APIs

- New APIs and changes to existing APIs should align with idiomatic Go.
- Flag APIs that use unclear names, unnecessary abstractions, non-idiomatic error handling, avoidable global state, or surprising behavior.
- Check whether exported APIs have clear semantics and appropriate documentation.

## Encoding and decoding

- Any new encoding or decoding feature must include fuzz tests.
- Flag encoding/decoding changes that lack fuzz coverage, especially when they parse untrusted, malformed, or external input.
- Check that fuzz tests cover malformed input, boundary cases, round-trip behavior, and compatibility expectations where relevant.

## Resource management

- **Storage cleanup**: Storage instances must be closed by whoever creates them. The general rule: if a function creates Storage internally, the caller must close it.
  - **Functions that create storage internally** (`PlainClone`, `PlainInit`, `PlainOpen`, `PlainOpenWithOptions`, `worktree.Worktree.Open()`): These create storage internally, so the returned repository's storage must be closed with `defer func() { _ = git.CloseStorage(r) }()` immediately after error checking.
  - **Direct storage creation** (`filesystem.NewStorage`, `transactional.NewStorage`): If you create a storage, you must close it with `defer func() { _ = sto.Close() }()`.
  - **Functions that receive storage** (`Init(storage)`, `Open(storage, worktree)`, `Clone(storage, ...)`): Do NOT close the repository's storage, since the caller owns it.
  - Flag any repository creation where the instance is discarded with `_`. These must assign to a variable and close the storage.
  - Rationale: Prevents file handle leaks that cause intermittent Windows test failures.
  - Leak detection is available via `-tags leakcheck` which will panic with a clear message if storages are garbage collected without calling `Close()`.
- **File handle cleanup**: All file `Open()` calls should have corresponding `defer Close()` calls, using `defer func() { _ = f.Close() }()` to avoid errcheck violations.
- **Other closeable resources**: Flag leaked connections, file descriptors, and other resources that implement `io.Closer`.
