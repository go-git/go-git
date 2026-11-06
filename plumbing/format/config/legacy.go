package config

import (
	"bytes"

	"github.com/go-git/gcfg/v2/scanner"
	"github.com/go-git/gcfg/v2/token"
)

// rewriteLegacyHeaders returns src with every section header in the legacy
// `[section.subsection]` form rewritten to `[section "subsection"]`, which
// is the form gcfg parses.
//
// Git still reads the legacy form: a header made only of alphanumerics, '-'
// and '.', with no space, names the section up to the first '.' and the
// subsection after it, both lowercased [1]. src is tokenized with gcfg's own
// scanner to find the '[' tokens gcfg parses headers from, so a '[' inside a
// value, a quoted subsection or a comment is never rewritten. Headers Git
// rejects, and a legacy header with an empty section or subsection name, are
// left for gcfg to refuse. The latter fails closed: gcfg would read
// `[protocol ""]` as `[protocol]`, so `[protocol.]`, whose `allow` Git reads
// as `protocol..allow`, would set `protocol.allow` instead.
//
// [1]: https://github.com/git/git/blob/v2.54.0/config.c#L963-L978
func rewriteLegacyHeaders(src []byte) ([]byte, error) {
	if !hasLegacyHeader(src) {
		return src, nil
	}

	fset := token.NewFileSet()
	file, err := fset.AddFile("", fset.Base(), len(src))
	if err != nil {
		return nil, err
	}
	var s scanner.Scanner
	// Syntax errors are gcfg's to report when it reads the result.
	if err := s.Init(file, src, func(token.Position, string) {}, 0); err != nil {
		return nil, err
	}

	var out []byte
	last := 0
	for {
		pos, tok, _, err := s.Scan()
		if err != nil {
			return nil, err
		}
		if tok == token.EOF {
			break
		}
		if tok == token.LBRACK {
			off, err := file.Offset(pos)
			if err != nil {
				return nil, err
			}
			if dot, end, ok := legacyHeader(src, off); ok {
				out = append(out, src[last:off]...)
				out = append(out, '[')
				out = append(out, bytes.ToLower(src[off+1:dot])...)
				out = append(out, ' ', '"')
				out = append(out, bytes.ToLower(src[dot+1:end])...)
				out = append(out, '"', ']')
				last = end + 1
			}
		}
	}
	if out == nil {
		return src, nil
	}
	return append(out, src[last:]...), nil
}

// hasLegacyHeader reports whether any '[' in src starts something that
// reads as a legacy header, so that the scan in rewriteLegacyHeaders is
// only paid for by a config that may hold one. Each check stops at the
// first byte that is not part of a legacy name, so this is linear in src.
func hasLegacyHeader(src []byte) bool {
	for i := 0; i < len(src); i++ {
		j := bytes.IndexByte(src[i:], '[')
		if j < 0 {
			return false
		}
		i += j
		if _, _, ok := legacyHeader(src, i); ok {
			return true
		}
	}
	return false
}

// legacyHeader reports whether src holds a legacy `[section.subsection]`
// header at off, which is a '[', with non-empty section and subsection
// names, and returns the offsets of its first '.' and of its closing ']'.
func legacyHeader(src []byte, off int) (dot, end int, ok bool) {
	dot = -1
	for i := off + 1; i < len(src); i++ {
		switch c := src[i]; {
		case c == ']':
			return dot, i, dot > off+1 && i > dot+1
		case c == '.':
			if dot < 0 {
				dot = i
			}
		case !isKeyChar(c):
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// isKeyChar reports whether c may appear in a config key, as Git's
// iskeychar does: an ASCII letter or digit, or '-'.
func isKeyChar(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-'
}
