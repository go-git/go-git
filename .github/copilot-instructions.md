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

- **Repository cleanup**: All `Repository` instances created with `PlainClone`, `PlainInit`, `PlainOpen`, `Clone`, or `Open` must have a corresponding `defer func() { _ = repo.Close() }()` immediately after error checking.
  - Flag any repository creation where the instance is discarded with `_`. These must assign to a variable and add `defer Close()`.
  - Rationale: Prevents file handle leaks that cause intermittent Windows test failures.
  - Leak detection is available via `-tags leakcheck` which will panic with a clear message if repositories are garbage collected without calling `Close()`.
- **Storage cleanup**: All `Storage` instances created with `filesystem.NewStorage` must have a corresponding `defer func() { _ = storage.Close() }()` immediately after creation, **except** when the storage is passed to a repository creation function.
  - **Repository takes ownership**: When storage is passed to `Init`, `Open`, `PlainInit`, `PlainOpen`, `Clone`, `PlainClone`, or `newRepository`, do NOT add a separate `defer storage.Close()`. The repository takes ownership and will close the storage when `repo.Close()` is called.
  - **Other uses require explicit close**: When storage is passed to types like `Remote` (via `NewRemote`) or used directly without creating a repository, you MUST add `defer func() { _ = storage.Close() }()`.
  - Flag any storage creation that lacks a deferred Close() call, unless it's passed directly to a repository creation function.
  - Rationale: Same file handle leak prevention as repositories.
- **File handle cleanup**: All file `Open()` calls should have corresponding `defer Close()` calls, using `defer func() { _ = f.Close() }()` to avoid errcheck violations.
- **Other closeable resources**: Flag leaked connections, file descriptors, and other resources that implement `io.Closer`.
