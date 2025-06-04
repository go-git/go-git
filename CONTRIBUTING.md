# Contributing Guidelines

The go-git project is [Apache 2.0 licensed](LICENSE) and accepts
contributions via GitHub pull requests. This document outlines some of the
conventions on development workflow, commit message formatting, contact points,
and other resources to make it easier to get your contribution accepted.

## Support Channels

The official support channels, for users are:

- [StackOverflow go-git tag] for user questions.
- GitHub [Issues]* for bug reports and feature requests.

*Before opening a new issue or submitting a new pull request, it's helpful to
search the project - it's likely that another user has already reported the
issue you're facing, or it's a known issue that we're already aware of.

In addition to the channels above, contributors are also able to join the go-git [discord server].

## How to Contribute

Pull Requests (PRs) are the main and exclusive way to contribute to the official go-git project.
In order for a PR to be accepted it needs to pass a list of requirements:

- You should be able to run the same query using `git`. We don't accept features that are not implemented in the official git implementation.
- The expected behavior must match the [official git implementation].
- The actual behavior must be correctly explained with natural language and providing a minimum working example in Go that reproduces it.
- All PRs must be written in idiomatic Go, formatted according to [gofmt], and without any warnings from [go vet].
- They should in general include tests, and those shall pass.
- If the PR is a bug fix, it has to include a suite of unit tests for the new functionality.
- If the PR is a new feature, it has to come with a suite of unit tests, that tests the new functionality.
- In any case, all the PRs have to pass the personal evaluation of at least one of the maintainers of go-git.

### Branches

The development branch is `main`, where all development takes place.
All new features and bug fixes should target it. This was formely known
as `v6-exp` or `v6-transport`. This branch contains all the changes for
`v6` - the next major release.
From time to time this branch will contain breaking changes, as the API
for `v6` is being refined.

The `releases/v5.x` branch is the branch for changes to the `v5` version,
which is now in maintaince mode. To avoid having to divert efforts from `v6`,
we will only be accepting bug fixes or CVE related dependency bumps for the
`v5` release.

Bug fixes that also impact `main`, should be fixed there first, and then backported to `v5`.

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
[gofmt]: https://golang.org/cmd/gofmt/
[go vet]: https://golang.org/cmd/vet/
