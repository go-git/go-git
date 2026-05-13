// Package compat provides support for Git's compatObjectFormat extension,
// which enables repositories to maintain bidirectional hash mappings between
// two object formats (e.g. SHA-256 and SHA-1).
//
// This package is experimental and its API may change in future go-git releases.
// For more information, see:
//   - https://git-scm.com/docs/hash-function-transition
//   - https://github.com/go-git/go-git/issues/1863
package compat
