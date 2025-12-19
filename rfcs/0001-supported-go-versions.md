# RFC 0001: Supported Go versions policy

## Summary

This RFC proposes formalising go-git's Go version support policy to officially support only the last 2 stable Go release lines, reducing from the current policy of supporting the last 3 stable release lines. This change aligns our support policy with our limited maintainer capacity, enables faster delivery of security fixes for Go runtime CVEs, and reduces testing complexity while maintaining a reasonable upgrade path for users through semantic versioning. This stance on supported Go versions has been loosely applied to the v6 version of go-git and go-billy; this RFC aims to make that change permanent.

## Motivation

The go-git project currently maintains a policy of supporting the last 3 stable Go release lines. While this provides broader compatibility, it creates several challenges that this RFC aims to address.

**Limited Maintainership Capacity**

Open source maintainer time is a precious and finite resource. The go-git project operates with limited maintainer capacity, and each additional supported Go version contributes to that burden. By focusing on fewer versions, maintainers can better focus their time on feature development, bug fixes, and community engagement.

**Security Posture and Go Runtime CVEs**

In recent years, the Go team has issued several CVEs affecting the Go runtime itself (e.g., CVE-2023-39325, CVE-2023-29409, CVE-2024-24783). When we support Go versions that have reached end-of-life upstream, we face a challenging dilemma: we cannot deliver runtime security fixes to users on those versions, yet maintaining support signals that these versions are acceptable to use. This creates a misalignment between our support policy and the security interests of our users.

The Go team's official support policy covers only the two most recent major releases. By aligning with this policy, we ensure that all officially supported go-git versions can benefit from upstream security patches.

**Use Cases and Expected Outcomes**

This policy primarily affects two user categories:

1. **Users prioritising security and modern features**: These users will benefit from reduced time-to-release for security fixes and more maintainer focus on feature development.

2. **Users with constraints preventing Go upgrades**: These users can continue using older go-git versions. Given go-git's position as a dependency library rather than an end-user application, and given that Go's compatibility promise means newer go-git versions generally work with older Go toolchains (subject to the `go.mod` directive), most users should find the transition manageable.

The expected outcome is a more sustainable maintenance model that better serves the security interests of the majority of users while maintaining a viable upgrade path for constrained environments.

## Detailed design

### Go Directive Policy

The `go.mod` file's `go` directive will always be set to `N-1`, where `N` is the latest stable Go version. This means:

- When Go 1.26 is released, go-git's next version will update `go.mod` to `go 1.25`
- When Go 1.27 is released, go-git's next version will update `go.mod` to `go 1.26`

This ensures that the minimum required Go version remains current while providing one version of buffer for users to upgrade.

### Testing Matrix

All GitHub Actions workflows will test against exactly 2 Go versions: the minimum version specified in `go.mod` and the latest stable release.

Current state (`.github/workflows/test.yml`):
```yaml
matrix:
  go-version: [1.24.x, 1.25.x]
  platform: [ubuntu-latest, macos-latest, windows-latest]
```

After Go 1.26 release and go.mod update:
```yaml
matrix:
  go-version: [oldstable, stable]
  platform: [ubuntu-latest, macos-latest, windows-latest]
```

This policy applies to all workflows that test against multiple Go versions, across all projects in the go-git org. For workflows that only test against a single Go version, the latest version will be used instead.

### Backward Compatibility for Constrained Users

Users who cannot upgrade their Go toolchain have several options:

1. **Continue using the last compatible go-git version**: Older versions remain available and functional
2. **Maintain a fork**: Organizations with specific constraints can maintain their own fork with extended Go version support

### Impact on the v5.x release branch

This proposal focuses solely on go-git v6. Releases from the [releases/v5.x] branch, for the maintenance-only `v5.x` releases, will continue to strive to support the last 3 stable Go release lines. However, if a severe upstream CVE is disclosed that affects go-git, we may break that promise in a new minor release.

## Drawbacks

**Reduced Compatibility Window**

Some users in constrained environments (legacy systems, strict compliance requirements, slow-moving enterprises) may find their upgrade cycles misaligned with go-git's release cycle. These users will need to either:
- Remain on older go-git versions longer
- Accelerate their Go toolchain upgrades
- Invest in maintaining compatibility forks

**Increased Pressure on Version Bumps**

With a shorter support window, the timing of when to bump the minimum Go version becomes more critical. Poor timing (e.g., immediately before a critical bug fix release) could force users into difficult upgrade decisions, such as having to bump their Go toolchain in order to benefit from a go-git feature or bug fix.

## Rationale and alternatives

### Why This Design is Optimal

**Alignment with Upstream**: The Go team supports only 2 versions, and aligning with this policy means all supported go-git versions can receive runtime security updates. This is the most principled approach to security maintenance and aligns with the duty of care towards go-git users.

**Sustainable Maintenance Model**: By reducing the support matrix from 3 to 2 versions, we reduce testing time and remove the additional burden of supporting a Go runtime that is no longer supported upstream. An additional benefit is the ability to use new Go features with a shorter waiting time. An example of this is some [improvements to go-billy] that depend on Go 1.25; this proposal means that such changes could be merged/released in February 2026 with the Go 1.26 release, otherwise they would need to wait until August 2026.

**Semantic Versioning Safety Net**: go-git follows semantic versioning, meaning users who cannot upgrade Go can remain on older go-git major versions without losing stability guarantees.

### Alternatives Considered

**Alternative 1: Maintain Status Quo (Support 3 Versions)**

*Rationale for rejection*: This alternative prioritises broad compatibility over maintainer sustainability and security responsiveness. Given limited maintainer capacity and the increasing frequency of Go runtime CVEs, maintaining 3 versions creates a false sense of security for users on the oldest version while stretching maintainer resources.

**Alternative 2: Support Only Latest Stable (N)**

*Rationale for rejection*: This would be too aggressive and would require users to upgrade Go with every minor version change. It would create significant friction for enterprise users and would not provide enough buffer time for ecosystem stabilisation after new Go releases.

The policy proposed in this RFC represents the best balance between user compatibility needs, maintainer sustainability, and security responsibility. It aligns go-git with upstream Go support policies, industry best practices, and the realities of open source maintenance.

[improvements to go-billy]: https://github.com/go-git/go-billy/pull/158/files#diff-33ef32bf6c23acb95f5902d7097b7a1d5128ca061167ec0716715b0b9eeaa5f6
[releases/v5.x]: https://github.com/go-git/go-git/tree/releases/v5.x
