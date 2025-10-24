# go-git RFCs

Many changes, including bug fixes and documentation improvements should be implemented and reviewed via the normal GitHub pull request workflow.

For more "substantial" changes, and we ask that these be put through a bit of a design process and produce a consensus among the go-git maintainers and the community.

The "RFC" (request for comments) process is intended to provide a consistent and controlled path for new features to enter the project.

## When you need to follow this process

You should consider using this process if you intend to make "substantial" changes to go-git. What constitutes a "substantial" change is evolving based on community norms, but may include the following:

- A new public API or significant changes to existing APIs
- The removal of public APIs or features
- Changes to the internal architecture that could affect performance or compatibility
- Changes to storage interfaces or backends
- New plumbing or porcelain operations
- Protocol-level changes or additions
- Significant changes to error handling or behaviour
- Changes that would require migration guides for users

Some changes do not require an RFC:

- Rephrasing, reorganising or refactoring
- Addition or removal of warnings
- Additions that strictly improve objective, numerical quality criteria (speedup, better error messages)
- Additions only likely to be _noticed by_ other implementors-of-go-git, invisible to users-of-go-git

If you submit a pull request to implement a new feature without going through the RFC process, it may be closed with a polite request to submit an RFC first.

## Before creating an RFC

A hastily-proposed RFC can hurt its chances of acceptance. Low quality proposals, proposals for previously-rejected features, or those that don't fit into the near-term roadmap, may be quickly rejected, which can be demotivating for the unprepared contributor. Laying some groundwork ahead of the RFC can make the process much smoother.

Although there is no single way to prepare for submitting an RFC, it is generally a good idea to pursue feedback from other contributors beforehand, to ascertain whether the RFC may be desirable: having a consistent impact on the project requires concerted effort towards consensus-building.

The most common preparations for writing and submitting an RFC include talking the idea over on our [Discord server] or discussing the topic via [GitHub Issues].

As a rule of thumb, receiving encouraging feedback from long-standing project contributors, and particularly maintainers, is a good indication that the RFC is worth pursuing.

## What the process is

In short, to get a major feature added to go-git, one must first get the RFC merged into the RFC repository as a markdown file. At that point the RFC is 'active' and may be implemented with the goal of eventual inclusion into go-git.

1. Fork the go-git repo: https://github.com/go-git/go-git
2. Copy `rfcs/0000-template.md` to `rfcs/0000-my-feature.md` (where 'my-feature' is descriptive. Don't assign an RFC number yet).
3. Fill in the RFC. Put care into the details: RFCs that do not present convincing motivation, demonstrate understanding of the impact of the design, or are disingenuous about the drawbacks or alternatives tend to be poorly-received.
4. Submit a pull request. As a pull request, the RFC will receive design feedback from the larger community, and the author should be prepared to revise it in response.
5. Build consensus and integrate feedback. RFCs that have broad support are much more likely to make progress than those that don't receive any comments.
6. Eventually, maintainers will decide whether the RFC is a candidate for inclusion in go-git.
7. An RFC can be modified based upon feedback from the maintainers and community. Significant modifications may trigger a new final comment period.
8. An RFC may be rejected by the maintainers after public discussion has settled and comments have been made summarizing the rationale for rejection. One maintainer should then close the RFC's associated pull request.
9. An RFC may be accepted at the close of its final comment period. One maintainer will merge the RFC's associated pull request, at which point the RFC will become 'active'.

## The RFC life-cycle

Once an RFC becomes active, then authors may implement it and submit the feature as a pull request to the go-git repo. Becoming 'active' is not a rubber stamp, and in particular still does not mean the feature will ultimately be merged; it does mean that the maintainers have agreed to it in principle and are amenable to merging it.

Furthermore, the fact that a given RFC has been accepted and is 'active' implies nothing about what priority is assigned to its implementation, nor whether anybody is currently working on it.

Modifications to active RFCs can be done in follow-up PRs. We strive to write each RFC in a manner that it will reflect the final design of the feature; but the nature of the process means that we cannot expect every merged RFC to actually reflect what the end result will be at the time of the next major release; therefore we try to keep each RFC document somewhat in sync with the language feature as planned, tracking such changes via follow-up pull requests to the document.

## Implementing an RFC

The author of an RFC is not obligated to implement it. Of course, the RFC author (like any other contributor) is welcome to post an implementation for review after the RFC has been accepted.
RFC authors may provide an off-tree implementation of the RFC, specially when they believe it would be helpful to others in understanding and testing it.

[Discord server]: https://discord.gg/8hrxYEVPE5
[GitHub Issues]: https://github.com/go-git/go-git/issues
