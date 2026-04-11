# AI Contribution Policy

go-git is a highly-capable Git implementation in pure Go, used in production
by a wide range of tools and services. Correctness, compatibility with the
reference Git implementation, and long-term maintainability are non-negotiable
properties of the project.

This policy sets clear expectations for AI-assisted contributions. It is not
an anti-AI stance — maintainers and contributors alike use AI tools in their
daily workflows, and we encourage you to do the same. AI can accelerate
learning, improve documentation, generate test scaffolding, and help explore
design alternatives. We welcome contributors who use AI as a productivity
amplifier, not as a substitute for understanding.

**AI tools are welcome in the go-git contributor workflow. The human contributor
is always accountable for every line submitted.**

## Contribution Guidelines

The following rules apply to all contributions, regardless of how they were
produced:

- **Own your changes.** You must be able to explain every change you submit.
  "The AI generated it" is never an acceptable answer during review.
- **Design before coding.** For non-trivial changes, open an Issue or RFC
  before a PR. See the [RFC process](rfcs/README.md) for substantial API or
  architectural changes. PRs that ignore established patterns will be closed.
- **Match git behaviour.** go-git tracks the reference Git implementation.
  AI tools may produce plausible-looking but subtly incorrect Git semantics.
  Verify correctness against `git` before submitting. Provide links to upstream code or documentation whenever possible.
- **Quality over quantity.** One well-understood, well-tested PR is worth more
  than many AI-assisted drive-by fixes. A flood of low-effort PRs exhausts
  maintainer attention and delays everyone in the queue.
- **Tests are required.** Bug fixes need regression tests; new features need
  unit and integration tests. AI-generated tests that do not actually exercise
  the relevant behaviour will be rejected.
- **Legal compliance.** go-git is [Apache 2.0 licensed](LICENSE). Contributions
  must ensure:
  - No third-party copyrighted material has been reproduced without a compatible
    open source license and proper attribution.
  - When AI tools are used, their terms do not impose restrictions incompatible with Apache 2.0.

## Disclosure

If AI assisted in producing any part of your contribution, disclose it in the
PR description. Add an `Assisted-by:` trailer to each affected commit:

```
Assisted-by: GitHub Copilot
Assisted-by: Claude Sonnet 4.6
Assisted-by: ChatGPT o3
```

Disclosure is not a penalty — it is trust infrastructure. It preserves
transparency, helps reviewers calibrate their attention, and keeps provenance
clear for the project's long-term health.

## Engaging With Maintainers

- **Respond personally.** Do not pipe review feedback back into an AI and
  apply the output blindly. Responses during review must reflect genuine
  understanding of the code and the project's design goals.
- **No AI ping-pong.** If maintainers observe a pattern of AI-driven responses
  without real engagement, the PR will be closed without further explanation.
- Maintainers reserve the right to close any low-effort AI contribution without
  a detailed technical critique.

## Maintainer Use of AI

Maintainers also use AI tools: for reviewing changes, exploring implementation options, and improving documentation. The same
disclosure and ownership expectations apply to maintainer-authored commits.

## Acknowledgements

This policy is inspired by the
[Kubewarden AI Policy](https://github.com/kubewarden/community/blob/main/AI_POLICY.md),
the [CloudNativePG AI Policy](https://github.com/cloudnative-pg/governance/blob/main/AI_POLICY.md),
and the [Kyverno AI Usage Policy](https://github.com/kyverno/kyverno/blob/main/AI_POLICY.md).
It aligns with the Linux Foundation's
[Generative AI guidance](https://www.linuxfoundation.org/legal/generative-ai)
and the CNCF community's evolving norms on sustainable AI-assisted open source
development.
