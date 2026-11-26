# Optional go-xdiff adapter

This separate Go module adapts `github.com/go-git/go-xdiff` to
`github.com/go-git/go-git/v6/plumbing/diff`.
The root module does not depend on this adapter.

Import this module and call `xdiff.New` to select the backend explicitly.
Pass `xdiff.Options` to select comparison rules and function display behavior.
The constructor signature is `func New(options *Options) diff.Differ`.
Its `Diff` field holds native options.
For example:

```go
backend := xdiff.New(&xdiff.Options{
    Diff: native.Options{Algorithm: native.Histogram},
    FunctionContext: true,
})
```

Here, `native` names `github.com/go-git/go-xdiff`.
Set `FunctionNames: true` to add section headings.
Set `FindFunc` to supply a custom function finder.
Nil `FindFunc` uses native ASCII detection.
The example tests show backend selection and a custom section heading.

`xdiff.New(nil)` retains the native defaults: Myers with `IndentHeuristic: false`.
Set `Diff.IndentHeuristic: true` explicitly to enable that heuristic.
Nil options disable function names and full function context.
The constructor copies the options and both native slices, `Anchors` and `IgnoreRegex`.
It does not modify caller options.
Regexp pointers remain shared; do not modify those expressions.
Use neutral `diff.HunkOptions` to control context lengths.
Pass the backend through `object.PatchOptions{Differ: backend}` or
`git.BlameOptions{Differ: backend}` to select it for patches or blame.
Run the examples:

```sh
GOPRIVATE=github.com/go-git/go-xdiff go test -run ExampleNew -v .
```

The backend retains input bytes.
Do not modify them while you use the result.
Hunk lines contain borrowed input records with their original newlines.
The neutral encoder, not this adapter, adds missing-newline notices.

## Independent iterators

`Result.Edits()` returns reusable raw edits with zero-based, half-open ranges.
Comparison completes during `Diff`, so raw iteration cannot fail.
`Result.Hunks(ctx, diff.HunkOptions{})` returns reusable display hunks and errors.
Hunks contain their lines and an optional `Section` heading.
An error appears once with an empty hunk, then iteration stops.
Break the range loop to stop work immediately.
Both iterators run synchronously, without background work.

The adapter retains the native result and converts one complete hunk at a time.
Native code owns context grouping and function heuristics.
The adapter does not buffer the complete output.
Hunk lines remain borrowed until the next iteration.
Copy the lines and their content before retention.

## Raw changes and output filters

Native `IgnoreBlankLines` and `IgnoreRegex` options suppress matching output hunks.
They do not remove raw changes from `Edits`.
`Patch.Stats` and blame use those raw changes.
Thus, statistics can count lines that filtered output does not show.
If filters suppress all content changes, the encoder emits no content or content headers.
File metadata, such as a rename or mode change, can still appear.

## License considerations

The `go-xdiff` dependency is licensed under LGPL-2.1-or-later.
Review its LICENSE and NOTICE files before use or distribution.
Module isolation is not a license exemption.
Your use or distribution can create license obligations, including notice
and source requirements.
Get qualified legal advice for your application; this document does not
give legal certainty.

## Local development

The module replaces the root dependency with `../../..` for local tests.
No workspace file is required.
Run these commands from this directory:

```sh
GOPRIVATE=github.com/go-git/go-xdiff go mod tidy
GOPRIVATE=github.com/go-git/go-xdiff go test -race ./...
```

The integration test compares repeated-brace attribution with native Git.
It skips that comparison when Git is not available.
The Spinnaker fixture test checks all 421 lines with the original expected hashes.
See [Git's blame documentation](https://git-scm.com/docs/git-blame)
and [function context documentation](https://git-scm.com/docs/diff-context-options).

Benchmarks compare native calls with adapter calls on the same small input:

```sh
GOPRIVATE=github.com/go-git/go-xdiff go test -run '^$' -bench . -benchmem
```
