# AGENTS.md

This file provides guidance for AI agents and tools working inside the
go-git repository. It complements [AI_POLICY.md](AI_POLICY.md), which
governs human accountability expectations. Everything here is designed to
help an AI agent produce contributions that meet the project's quality bar
on the first attempt.

## Project Overview

go-git is a highly-capable Git implementation in pure Go. It exposes both
plumbing (low-level) and porcelain (high-level) APIs and is used in
production by a wide range of tools and services.

Key properties that must never regress:

- **Correctness** — behaviour must match the reference `git` implementation.
- **Compatibility** — public APIs evolve carefully; breaking changes require
  an RFC and target `main` (v6) only.
- **Testability** — every behavioural change needs tests.

## Repository Layout

```
plumbing/           # Low-level Git primitives
  format/           #   Object/pack/config format parsers
  object/           #   Git object types (commit, tree, blob, tag)
  protocol/         #   Git wire protocol
  transport/        #   Transport implementations (HTTP, SSH, git://)
  storer/           #   Storage interfaces
storage/
  filesystem/       # On-disk storage backend
  memory/           # In-memory storage backend
config/             # Git configuration types
internal/           # Internal helpers (not part of the public API)
_examples/          # Runnable usage examples
rfcs/               # RFCs for major changes
```

## Branches

| Branch          | Purpose                                      |
|-----------------|----------------------------------------------|
| `main`          | Active development — targets v6              |
| `releases/v5.x` | Maintenance — bug fixes and CVE patches only |

All new features and non-critical fixes target `main`. Backports to
`releases/v5.x` must be motivated by a bug or security issue, and agreed with a maintainer.

## Code Conventions

- **Go version**: stay within the range declared in `go.mod`.
- **Formatting and linting**: no warnings from `golangci-lint` (config: `.golangci.yaml`), which includes formatting checks.
- **Style**: idiomatic Go — no unnecessary abstractions, no premature
  generalization, no helper functions used only once.
- **Error handling**: return errors; do not panic in library code.
- **No global state**: the library strives to be safe to use from multiple
  goroutines via separate instances, all new code should aim to align with that goal.

## Commit Message Format

```
<package>: <subpackage>, <what changed>. [Fixes #<issue-number>]
```

Examples:

```
plumbing: transport, Add HTTP/2 support. Fixes #456
storage: filesystem, Fix config.worktree overlay for linked worktrees.
```

- The title line is NOT followed by a blank line before the body.
- The body starts on the very next line after the title.
- A blank line separates the body from trailers.

### AI-Assisted Commits

All commits produced with AI assistance MUST include an `Assisted-by`
trailer identifying the agent:

```
plumbing: object, Fix delta offset decoding for large pack files.
Ensure the offset calculation correctly handles the variable-length
encoding used for offsets > 2 GiB.

Assisted-by: Claude Sonnet 4.6 <noreply@anthropic.com>
```

Adjust name and address to match the actual tool used (e.g.
`Assisted-by: Copilot <noreply@github.com>`).

## Git Behaviour Compliance

go-git tracks the reference `git` implementation. Before proposing any
change that affects Git semantics:

1. Verify the expected behaviour by running `git` locally.
2. Link to the relevant upstream source or documentation in the PR
   description (e.g. `git/git` source, Git protocol spec, or man page).
3. Add a test that would fail without the fix and passes with it.

AI tools frequently produce plausible-looking but subtly incorrect Git
semantics. Do not trust generated logic for protocol, pack format, or
ref handling without cross-checking against the spec or reference
implementation.

## Testing

- Bug fixes require a regression test that fails on `main` before the fix.
- New features require unit tests and, where applicable, integration tests.
- Use the in-memory filesystem (`fixtures.WithMemFS()`) for fast,
  hermetic tests; use real on-disk fixtures only when necessary.
- Tests that do not actually exercise the changed behaviour may be
  rejected during review, or deprioritised.
- **Prefer legible test code over comments.** Use descriptive names so
  that the flow of a test is self-evident. Reserve comments for genuine
  Git domain insight that cannot be expressed in code — never for narrating
  what the code obviously does (e.g. `// Create repository`).

### Running Tests

```bash
# All tests
make test

# Single package
go test ./plumbing/...

# With race detector
go test -race ./...
```

The `plumbing/transport/git` tests require a local `git daemon` and will
fail in environments without one — this is a known infrastructure
limitation and is not a signal that other changes are broken.

## Pull Requests

- Open an Issue before a PR for any non-trivial change.
- For substantial API or architectural changes, follow the
  [RFC process](rfcs/README.md) before writing code.
- One focused PR is better than a large omnibus change.
- The PR description must explain the _why_, not just the _what_.
- If AI assisted in producing the contribution, say so in the PR
  description (see [AI_POLICY.md](AI_POLICY.md)).

## What AI Agents Should Not Do

- **Do not** submit changes that cannot be explained line-by-line.
- **Do not** add features not present in the reference `git` implementation.
- **Do not** modify `releases/v5.x` unless the change is a bug fix or CVE
  patch that was first applied to `main`.
- **Do not** introduce global state, `init()` side effects, or `panic` in
  library code.
- **Do not** open multiple PRs in rapid succession.
- **Do not** apply review feedback by re-running the prompt — understand the
  feedback and respond to it directly.
