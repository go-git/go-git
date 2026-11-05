package git

import (
	"bufio"
	"bytes"
	"io"
	"path"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing/format/gitattributes"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
)

const (
	// attributesFileName is the name of the attributes file of a directory
	// in the worktree.
	attributesFileName = ".gitattributes"

	// maxAttributesLine and maxAttributesFile are the limits beyond which
	// Git ignores a line, or a whole attributes file, with a warning.
	maxAttributesLine = 2048
	maxAttributesFile = 100 << 20
)

// checkinAttributes reads the attributes Git applies to a worktree file it
// hashes for the index, the check-in direction[1]: from the .gitattributes
// files along the file's path, each read from the worktree or, where the
// worktree has none, as under a sparse checkout, from the index; then from
// $GIT_DIR/info/attributes, which has the highest priority. The global
// attributes file (core.attributesFile) and the system one are not read.
//
// Git warns about an attributes file it cannot read and about a line it
// cannot parse, and goes on without them, so neither fails here either.
//
// [1]: https://github.com/git/git/blob/v2.54.0/attr.c#L841-L872
type checkinAttributes struct {
	w   *Worktree
	idx *index.Index

	// dirs holds the attributes read for each directory, by its path.
	dirs map[string][]gitattributes.MatchAttribute
	// inIndex holds the index entries of attributes files, by path.
	inIndex map[string]*index.Entry
	info    []gitattributes.MatchAttribute
	// infoRead tells whether info has been read.
	infoRead bool
}

// newCheckinAttributes returns the check-in attributes of w, with idx, the
// index the operation starts from, for the attributes files the worktree
// lacks.
func newCheckinAttributes(w *Worktree, idx *index.Index) *checkinAttributes {
	return &checkinAttributes{w: w, idx: idx, dirs: make(map[string][]gitattributes.MatchAttribute)}
}

// keepsCRLF reports whether the attributes of name tell Git to hash the
// file as written, without converting its CRLF line endings, even under
// core.autocrlf: text is unset, which the binary macro does too, or, with
// text unspecified or of another value than auto or input, crlf is
// unset[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/convert.c#L1269-L1339
func (a *checkinAttributes) keepsCRLF(name string) bool {
	parts := strings.Split(name, "/")

	info := a.infoAttributes()
	stack := make([]gitattributes.MatchAttribute, 0, len(info))
	for i := range parts {
		stack = append(stack, a.dir(parts[:i])...)
	}
	stack = append(stack, info...)
	if len(stack) == 0 {
		return false
	}

	results, _ := gitattributes.NewMatcher(stack).Match(parts, []string{"text", "crlf"})
	if action, ok := crlfAction(results["text"]); ok {
		return action
	}
	action, _ := crlfAction(results["crlf"])
	return action
}

// crlfAction reports whether attr, text or the older crlf, keeps line
// endings as written, and whether it decides that at all.
func crlfAction(attr gitattributes.Attribute) (keep, decided bool) {
	switch {
	case attr == nil || attr.IsUnspecified():
		return false, false
	case attr.IsUnset():
		return true, true
	case attr.IsSet():
		return false, true
	}
	switch attr.Value() {
	case "auto", "input":
		return false, true
	}
	return false, false
}

// dir returns the attributes of the .gitattributes file in dir.
func (a *checkinAttributes) dir(dir []string) []gitattributes.MatchAttribute {
	key := path.Join(dir...)
	if attrs, ok := a.dirs[key]; ok {
		return attrs
	}

	name := path.Join(key, attributesFileName)
	root := len(dir) == 0
	attrs, ok := readWorktreeAttributes(a.w.filesystem, name, dir, root)
	if !ok {
		attrs = a.indexAttributes(name, dir, root)
	}
	a.dirs[key] = attrs
	return attrs
}

// readWorktreeAttributes reads the attributes file name of fs. It reports
// false when there is no regular file of a size Git reads there: Git
// opens it without following a symlink, and reads the index instead when
// it cannot.
func readWorktreeAttributes(fs billy.Filesystem, name string, domain []string, macros bool) ([]gitattributes.MatchAttribute, bool) {
	fi, err := fs.Lstat(name)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() >= maxAttributesFile {
		return nil, false
	}

	f, err := fs.Open(name)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()

	return parseAttributes(f, domain, macros, true), true
}

// indexAttributes reads the attributes file name from its blob in the index:
// the entry of stage 0, or, in a conflict, of stage 2, as Git does.
func (a *checkinAttributes) indexAttributes(name string, domain []string, macros bool) []gitattributes.MatchAttribute {
	if a.idx == nil {
		return nil
	}
	if a.inIndex == nil {
		a.inIndex = make(map[string]*index.Entry)
		for _, e := range a.idx.Entries {
			if path.Base(e.Name) != attributesFileName {
				continue
			}
			if e.Stage != 0 && e.Stage != index.OurMode {
				continue
			}
			if seen, ok := a.inIndex[e.Name]; !ok || seen.Stage != 0 {
				a.inIndex[e.Name] = e
			}
		}
	}

	e, ok := a.inIndex[name]
	if !ok {
		return nil
	}
	blob, err := object.GetBlob(a.w.r.Storer, e.Hash)
	if err != nil || blob.Size >= maxAttributesFile {
		return nil
	}
	r, err := blob.Reader()
	if err != nil {
		return nil
	}
	defer func() { _ = r.Close() }()

	return parseAttributes(r, domain, macros, false)
}

// infoAttributes returns the attributes of $GIT_DIR/info/attributes, which
// Git reads following a symlink.
func (a *checkinAttributes) infoAttributes() []gitattributes.MatchAttribute {
	if a.infoRead {
		return a.info
	}
	a.infoRead = true

	s, ok := a.w.r.Storer.(interface{ Filesystem() billy.Filesystem })
	if !ok {
		return nil
	}
	fs := s.Filesystem()
	name := fs.Join("info", "attributes")
	fi, err := fs.Stat(name)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() >= maxAttributesFile {
		return nil
	}
	f, err := fs.Open(name)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	a.info = parseAttributes(f, nil, true, true)
	return a.info
}

var utf8BOM = []byte("\xef\xbb\xbf")

// parseAttributes reads the attributes in r, with patterns relative to the
// directory domain, skipping the lines Git ignores with a warning: one of
// maxAttributesLine bytes or more, one that does not parse, such as an
// invalid attribute name or a macro outside a file that may define them
// (macros), and a negative pattern. With bom, a UTF-8 byte order mark at
// the start is dropped, as Git drops it from a file. Reading stops at
// maxAttributesFile bytes, or at a read error, keeping what was read.
func parseAttributes(r io.Reader, domain []string, macros, bom bool) []gitattributes.MatchAttribute {
	br := bufio.NewReader(io.LimitReader(r, maxAttributesFile))

	var attrs []gitattributes.MatchAttribute
	for first := true; ; first = false {
		line, tooLong, err := readAttributesLine(br)
		if first && bom {
			line = bytes.TrimPrefix(line, utf8BOM)
		}
		if !tooLong && len(line) < maxAttributesLine {
			if m, ok := parseAttributesLine(string(line), domain, macros); ok {
				attrs = append(attrs, m)
			}
		}
		if err != nil {
			return attrs
		}
	}
}

// readAttributesLine reads a line from br without its line ending. It keeps
// at most maxAttributesLine bytes of it, and reports whether there were
// more.
func readAttributesLine(br *bufio.Reader) (line []byte, tooLong bool, err error) {
	for {
		chunk, err := br.ReadSlice('\n')
		if len(line)+len(chunk) <= maxAttributesLine+2 {
			line = append(line, chunk...)
		} else {
			tooLong = true
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		line = bytes.TrimSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\r"))
		return line, tooLong, err
	}
}

// parseAttributesLine parses one attributes line, reporting false for a
// line that names no pattern or that Git ignores.
func parseAttributesLine(line string, domain []string, macros bool) (gitattributes.MatchAttribute, bool) {
	m, err := gitattributes.ParseAttributesLine(line, domain, macros)
	if err != nil || m.Name == "" {
		return m, false
	}
	if m.Pattern != nil && strings.HasPrefix(m.Name, "!") {
		return m, false
	}
	for _, attr := range m.Attributes {
		if strings.HasPrefix(attr.Name(), "builtin_") {
			return m, false
		}
	}
	return m, true
}
