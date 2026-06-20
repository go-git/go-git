# Contributing Guidelines

The go-git project is [Apache 2.0 licensed](LICENSE) and accepts
contributions via GitHub pull requests. This document outlines some of the
conventions on development workflow, commit message formatting, contact points,
and other resources to make it easier to get your contribution accepted.

## Support Channels

The official support channels for users are:

- [StackOverflow go-git tag] for user questions.
- GitHub [Issues]* for bug reports and feature requests.

*Before opening a new issue or submitting a new pull request, it's helpful to
search the project - it's likely that another user has already reported the
issue you're facing, or it's a known issue that we're already aware of.

In addition to the channels above, contributors are also able to join the go-git [discord server].

## AI-Assisted Contributions

If you use AI tools as part of your contribution workflow, please read the
[AI Contribution Policy](AI_POLICY.md) before opening a PR.

## Sustainability

The majority of the work on go-git comes from **individual contributors** volunteering
their own time. This limits the amount of capacity available for activities like backporting
bug fixes and security patches (including CVE-related dependency bumps) to `v5`, triaging issues, and expanding test coverage.

If your company relies on go-git, please consider contributing engineering hours to
help sustain the project. Some high-impact areas where help is especially welcome:

- **Testing** — expanding integration and regression test suites, especially tests that verify alignment with upstream git behaviour.
- **Reviewing PRs** — testing implementations locally and providing constructive feedback.
- **Backporting** — porting eligible bug fixes and CVE-related dependency bumps from `v6` back to the `v5` branch, in line with the documented branch policy.
- **Documentation** — improving examples, compatibility docs, and migration guides.
- **Issue triage** — reproducing bug reports and labelling issues.

Please reach out via [discord server] in case you want to support with any of the above.

## How to Contribute

### RFC Process for Major Changes

For substantial changes to go-git's public APIs, architecture, process, or functionality, please consider using our [RFC (Request for Comments) process](rfcs/README.md). This includes:

- New public APIs or significant changes to existing APIs
- Changes to storage interfaces or backends
- New plumbing or porcelain operations
- The processes around merging PRs or releasing changes
- Changes that would require migration guides for users

The RFC process helps ensure that major changes are well-designed and have community consensus before implementation begins.

### Pull Requests

Pull Requests (PRs) are the main and exclusive way to contribute to the official go-git project.
In order for a PR to be accepted it needs to pass a list of requirements:

- You should be able to run the same operation using `git`. We don't accept features that are not implemented in the official git implementation.
- The expected behavior must match the [official git implementation].
- The actual behavior must be correctly explained with natural language and providing a minimum working example in Go that reproduces it.
- All PRs must be written in idiomatic Go and pass [golangci-lint] with no warnings (this includes formatting checks).
- They should in general include tests, and those shall pass.
- If the PR is a bug fix, it has to include tests that cover the regression.
- If the PR is a new feature, it has to come with a suite of unit tests that cover the new functionality.
- In any case, all the PRs have to pass the personal evaluation of at least one of the maintainers of go-git.

## Code Review Checklist

When reviewing code (whether you're a human reviewer or using AI tools to assist), please verify:

### Resource Management
- **Repository cleanup**: All `Repository` instances must have `defer func() { _ = repo.Close() }()` after creation.
- **Storage cleanup**: All `Storage` instances must have `defer func() { _ = storage.Close() }()` after creation, **except** when passed to repository creation functions (`Init`, `Open`, `PlainInit`, etc.) where the repository takes ownership.
- **File handles**: All file `Open()` calls must have `defer func() { _ = f.Close() }()`.
- **Leak detection**: Run tests with `-tags leakcheck` to detect unclosed resources:
  ```bash
  go test -tags leakcheck ./...
  ```

For detailed review guidelines, see [.github/copilot-instructions.md](.github/copilot-instructions.md).

### Branches

The development branch is `main`, where all development takes place.
All new features and bug fixes should target it. This was formerly known
as `v6-exp` or `v6-transport`. This branch contains all the changes for
`v6` - the next major release.
From time to time this branch will contain breaking changes, as the API
for `v6` is being refined.

The `releases/v5.x` branch is the branch for changes to the `v5` version,
which is now in maintenance mode. To avoid having to divert efforts from `v6`,
we will only be accepting bug fixes or CVE related dependency bumps for the
`v5` release.

Bug fixes that also impact `main` should be fixed there first, and then backported to `v5`.

### Developer Certificate of Origin

go-git requires all commits to be signed off with a [Developer Certificate of Origin (DCO)](https://developercertificate.org/) sign-off. This is a lightweight way for contributors to certify that they wrote, or have the right to submit, the code being contributed.

The sign-off is a single line added to the end of each commit message:

```
Signed-off-by: Jane Smith <jane.smith@example.com>
```

Git makes this easy — pass `-s` (or `--signoff`) when committing:

```sh
git commit -s -m "plumbing: packp, fix capability parsing"
```

To sign off commits you have already made:

```sh
git commit --amend --signoff --no-edit  # amend the last commit
git rebase --signoff HEAD~N             # sign off the last N commits
```

DCO sign-off is verified automatically on every pull request. PRs with unsigned commits will not be merged.

### Format of the commit message

Every commit message should describe what was changed, under which context and, if applicable, the GitHub issue it relates to:

```
plumbing: packp, Skip argument validations for unknown capabilities. Fixes #623
```

The format can be described more formally as follows:

```
<package>: <subpackage>, <what changed>. [Fixes #<issue-number>]
```

[discord server]: https://discord.gg/8hrxYEVPE5
[StackOverflow go-git tag]: https://stackoverflow.com/questions/tagged/go-git
[Issues]: https://github.com/go-git/go-git/issues
[official git implementation]: https://github.com/git/git
[golangci-lint]: https://github.com/golangci/golangci-lint
