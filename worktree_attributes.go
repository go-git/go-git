package git

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/format/gitattributes"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/x/plugin"
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
// $GIT_DIR/info/attributes, which has the highest priority. The global and
// the system attributes files are read below them, see globalAttributes.
// A tree named by attr.tree is not read.
//
// Git warns about an attributes file it cannot read and about a line it
// cannot parse, and goes on without them, so neither fails here either.
//
// [1]: https://github.com/git/git/blob/v2.54.0/attr.c#L841-L872
type checkinAttributes struct {
	w   *Worktree
	idx *index.Index
	// cfgs holds the configurations to read raw options from, the
	// highest priority first: the repository, the global and the system one.
	cfgs []*config.Config
	// files provides the system and the global attributes files, and is
	// nil when the config loader has none.
	files plugin.AttributesSource
	// fold tells whether patterns match regardless of case, as under
	// core.ignorecase.
	fold bool

	// dirs holds the attributes read for each directory, by its path.
	dirs map[string][]gitattributes.MatchAttribute
	// inIndex holds the index entries of attributes files, by path.
	inIndex map[string]*index.Entry
	info    []gitattributes.MatchAttribute
	// infoRead tells whether info has been read.
	infoRead bool
	// global holds the attributes of the system and the global attributes
	// files, read once globalRead is set.
	global     []gitattributes.MatchAttribute
	globalRead bool
	globalErr  error

	// repo is the repository configuration, whose scopes below it are
	// loaded once loaded is set, when a path first needs them.
	repo   *config.Config
	loaded bool
}

// newCheckinAttributes returns the check-in attributes of w under the
// configurations cfgs, the highest priority first, with idx, the index the
// operation starts from, for the attributes files the worktree lacks.
func newCheckinAttributes(w *Worktree, idx *index.Index, cfgs ...*config.Config) *checkinAttributes {
	return &checkinAttributes{
		w:      w,
		idx:    idx,
		cfgs:   cfgs,
		fold:   rawBool(cfgs, "core", "", "ignorecase"),
		dirs:   make(map[string][]gitattributes.MatchAttribute),
		loaded: true,
	}
}

// lazyCheckinAttributes returns the check-in attributes of w under the
// repository configuration cfg, which load completes with the global and
// the system ones, when a path first needs them: Git reads no attributes
// configuration for a path it does not check.
func lazyCheckinAttributes(w *Worktree, idx *index.Index, cfg *config.Config) *checkinAttributes {
	a := newCheckinAttributes(w, idx, cfg)
	a.repo, a.loaded = cfg, false
	return a
}

// load reads the configuration scopes below the repository one, once.
func (a *checkinAttributes) load() error {
	if a.loaded {
		return nil
	}
	cfgs, files, err := scopedConfigs(a.repo)
	if err != nil {
		return err
	}
	a.cfgs, a.files, a.loaded = cfgs, files, true
	a.fold = rawBool(cfgs, "core", "", "ignorecase")
	return nil
}

// rawOption returns the value of the option key in the section, and
// subsection, of the raw configuration of the first of cfgs that sets it,
// and whether any does. A configuration that sets an option to nothing
// hides it from those below.
func rawOption(cfgs []*config.Config, section, subsection, key string) (string, bool) {
	for _, cfg := range cfgs {
		if cfg == nil || cfg.Raw == nil || !cfg.Raw.HasSection(section) {
			continue
		}
		s := cfg.Raw.Section(section)
		switch {
		case subsection == "" && s.HasOption(key):
			return s.Option(key), true
		case subsection != "" && s.HasSubsection(subsection) && s.Subsection(subsection).HasOption(key):
			return s.Subsection(subsection).Option(key), true
		}
	}
	return "", false
}

// rawBool reports whether the option key is true, as Git's
// git_config_bool does. The parser reads an option without a value the
// same as one set to nothing, so both read as true, which Git does for
// the former only.
func rawBool(cfgs []*config.Config, section, subsection, key string) bool {
	v, ok := rawOption(cfgs, section, subsection, key)
	return ok && (v == "" || parseBool(v))
}

// scopedConfigs returns the repository configuration cfg and, below it,
// the global and the system ones of the registered config loader, the
// highest priority first, and the attributes files of the loader, when it
// provides them. It fails where Git dies: when a scope cannot be loaded.
func scopedConfigs(cfg *config.Config) ([]*config.Config, plugin.AttributesSource, error) {
	cfgs := []*config.Config{cfg}
	if !plugin.Has(plugin.ConfigLoader()) {
		return cfgs, nil, nil
	}
	src, err := plugin.Get(plugin.ConfigLoader())
	if err != nil {
		return nil, nil, err
	}
	for _, scope := range []struct {
		scope config.Scope
		name  string
	}{{config.GlobalScope, "global"}, {config.SystemScope, "system"}} {
		s, err := src.Load(scope.scope)
		if err != nil {
			return nil, nil, fmt.Errorf("load %s configuration: %w", scope.name, err)
		}
		c, err := s.Config()
		if err != nil {
			return nil, nil, fmt.Errorf("read %s configuration: %w", scope.name, err)
		}
		cfgs = append(cfgs, c)
	}
	files, _ := src.(plugin.AttributesSource)
	return cfgs, files, nil
}

// globalAttributes returns the attributes of the system attributes file
// and of the global one, which Git reads at the lowest priority, the
// global file over the system one. The global file is core.attributesFile
// of the repository, global or system configuration, or else the default
// location. A relative name is read from the worktree, which is where Git
// runs. An option set to nothing leaves no global file, not the default
// one. With the prefix ":(optional)", a missing file is as if the option
// were unset[2].
//
// The files come from the attributes source of the config loader, if it
// has one, so a loader that provides none leaves no host file to read.
// Git reads the system file unless GIT_ATTR_NOSYSTEM is true[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/attr.c#L891-L894
// [2]: https://github.com/git/git/blob/v2.54.0/config.c#L1279-L1297
func (a *checkinAttributes) globalAttributes() ([]gitattributes.MatchAttribute, error) {
	if a.globalRead {
		return a.global, a.globalErr
	}
	a.globalRead = true
	a.global, a.globalErr = a.readGlobalAttributes()
	return a.global, a.globalErr
}

func (a *checkinAttributes) readGlobalAttributes() ([]gitattributes.MatchAttribute, error) {
	if a.files == nil {
		return nil, nil
	}

	var global []gitattributes.MatchAttribute
	if !envBool("GIT_ATTR_NOSYSTEM") {
		fs, name, err := a.files.AttributesFile(config.SystemScope, "")
		if err != nil {
			return nil, err
		}
		if name != "" {
			global = readAttributesFile(fs, name)
		}
	}

	fs, file, err := a.globalFile()
	if err != nil {
		return nil, err
	}
	if file != "" {
		global = append(global, readAttributesFile(fs, file)...)
	}
	a.foldPatterns(global, nil)
	return global, nil
}

// globalFile returns the filesystem and the name of the global attributes
// file, or an empty name when there is none. It fails where Git dies, on
// a name that cannot be expanded, even an optional one.
func (a *checkinAttributes) globalFile() (billy.Basic, string, error) {
	configured, set := rawOption(a.cfgs, "core", "", "attributesfile")
	name, optional := strings.CutPrefix(configured, ":(optional)")
	if name == "" && set && !optional {
		return nil, "", nil
	}

	if name != "" {
		var fs billy.Basic = a.w.filesystem
		file := name
		var err error
		switch {
		case strings.HasPrefix(name, "~") || strings.HasPrefix(name, "/") || filepath.IsAbs(name):
			fs, file, err = a.files.AttributesFile(config.GlobalScope, name)
		default:
			if root, ok := worktreeRoot(a.w.filesystem); ok {
				fs, file, err = a.files.AttributesFile(config.GlobalScope, resolveFromRoot(root, name))
			}
		}
		if err != nil {
			return nil, "", err
		}
		if !optional {
			return fs, file, nil
		}
		if fs != nil && file != "" {
			if _, err := fs.Stat(file); err == nil {
				return fs, file, nil
			}
		}
	}
	return a.files.AttributesFile(config.GlobalScope, "")
}

// resolveFromRoot returns the path of name, relative to the directory root,
// with the symlinks along it resolved as the host does, which a source may
// refuse to follow itself. Where that fails, it is the cleaned path.
func resolveFromRoot(root, name string) string {
	if p, err := filepath.EvalSymlinks(root + string(filepath.Separator) + filepath.FromSlash(name)); err == nil {
		return p
	}
	return filepath.Join(root, name)
}

// worktreeRoot returns the directory of the host that holds the worktree
// fs, if it is one on disk, which only an [osfs.BoundOS] tells from an
// in-memory one that also has a root. Git runs there, and reads a relative name, such as
// ../attrs, or one through a symlink, from outside the worktree, which fs
// itself refuses.
func worktreeRoot(fs billy.Filesystem) (string, bool) {
	if wfs, ok := fs.(*worktreeFilesystem); ok {
		fs = wfs.Filesystem
	}
	b, ok := fs.(*osfs.BoundOS)
	if !ok || !filepath.IsAbs(b.Root()) {
		return "", false
	}
	return b.Root(), true
}

// envBool reports whether the environment variable name is set to a true
// value, as Git's git_env_bool does.
func envBool(name string) bool {
	return parseBool(os.Getenv(name))
}

// parseBool reports whether v is a true value for Git: true, yes, on, or a
// non-zero number in any base, with an optional k, m or g suffix, as
// git_parse_maybe_bool and git_parse_int read it[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/parse.c#L157-L182
func parseBool(v string) bool {
	v = strings.ToLower(v)
	switch v {
	case "true", "yes", "on":
		return true
	}
	if strings.HasSuffix(v, "k") || strings.HasSuffix(v, "m") || strings.HasSuffix(v, "g") {
		v = v[:len(v)-1]
	}
	digits := strings.TrimLeft(v, "+-")
	if strings.Contains(digits, "_") || strings.HasPrefix(digits, "0b") || strings.HasPrefix(digits, "0o") {
		return false
	}
	n, err := strconv.ParseInt(v, 0, 64)
	return err == nil && n != 0
}

// readAttributesFile reads the attributes file name of fs, following a
// symlink, as Git does for the files outside the worktree.
func readAttributesFile(fs billy.Basic, name string) []gitattributes.MatchAttribute {
	fi, err := fs.Stat(name)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() >= maxAttributesFile {
		return nil
	}
	f, err := fs.Open(name)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	return parseAttributes(f, nil, true, true)
}

// keepsCRLF reports whether the attributes of name tell Git to hash the
// file as written, without converting its CRLF line endings, even under
// core.autocrlf: text is unset, which the binary macro does too, or, with
// text unspecified or of another value than auto or input, crlf is
// unset[1].
//
// It also reports true where it cannot tell what Git would hash: attr.tree
// or GIT_ATTR_SOURCE is set, or a filter applies that Git would run, see
// runsFilter, which go-git does not. A reset then keeps the file as a change
// rather than overwrite it.
// It fails where Git dies, on a configuration or a global attributes file
// name it cannot load or expand.
//
// [1]: https://github.com/git/git/blob/v2.54.0/convert.c#L1269-L1339
func (a *checkinAttributes) keepsCRLF(name string) (bool, error) {
	if err := a.load(); err != nil {
		return false, err
	}
	parts := strings.Split(name, "/")

	_, sourceSet := os.LookupEnv("GIT_ATTR_SOURCE")
	if tree, _ := rawOption(a.cfgs, "attr", "", "tree"); tree != "" || sourceSet {
		return true, nil
	}

	info := a.infoAttributes()
	global, err := a.globalAttributes()
	if err != nil {
		return false, err
	}
	stack := make([]gitattributes.MatchAttribute, 0, len(global)+len(info))
	stack = append(stack, global...)
	for i := range parts {
		stack = append(stack, a.dir(parts[:i])...)
	}
	stack = append(stack, info...)
	if len(stack) == 0 {
		return false, nil
	}

	if a.fold {
		for i, part := range parts {
			parts[i] = asciiLower(part)
		}
	}
	results, _ := gitattributes.NewMatcher(stack).Match(parts, []string{"text", "crlf", "filter"})
	if f := results["filter"]; f != nil && f.IsValueSet() && a.runsFilter(f.Value()) {
		return true, nil
	}
	if action, ok := crlfAction(results["text"]); ok {
		return action, nil
	}
	action, _ := crlfAction(results["crlf"])
	return action, nil
}

// runsFilter reports whether Git would run the filter driver to clean a
// file, or fail for want of a command: one that is required, or whose
// process command, or without one its clean command, is not empty[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/convert.c#L1008-L1019
func (a *checkinAttributes) runsFilter(driver string) bool {
	if rawBool(a.cfgs, "filter", driver, "required") {
		return true
	}
	if process, ok := rawOption(a.cfgs, "filter", driver, "process"); ok {
		return process != ""
	}
	clean, _ := rawOption(a.cfgs, "filter", driver, "clean")
	return clean != ""
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
	a.foldPatterns(attrs, dir)
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
	a.info = readAttributesFile(fs, fs.Join("info", "attributes"))
	a.foldPatterns(a.info, nil)
	return a.info
}

var utf8BOM = []byte("\xef\xbb\xbf")

// parseAttributes reads the attributes in r, with patterns relative to the
// directory domain, skipping the lines Git ignores with a warning: one of
// maxAttributesLine bytes or more, one that does not parse, such as an
// invalid attribute name or a macro outside a file that may define them
// (macros), and a negative pattern. A line ends at a NUL, which Git reads
// as the end of a C string, and the limit applies to what comes before it.
// Reading a blob, as opposed to a file, stops at the first NUL and counts a
// CR ending a line toward its length. With file, a UTF-8 byte order mark at
// the start is dropped, as Git drops it from a file. Reading stops at
// maxAttributesFile bytes, or at a read error, keeping what was read.
func parseAttributes(r io.Reader, domain []string, macros, file bool) []gitattributes.MatchAttribute {
	br := bufio.NewReader(io.LimitReader(r, maxAttributesFile))

	var attrs []gitattributes.MatchAttribute
	for first := true; ; first = false {
		line, nul, err := readAttributesLine(br, file)
		if first && file {
			line = bytes.TrimPrefix(line, utf8BOM)
		}
		if len(line) < maxAttributesLine {
			if m, ok := parseAttributesLine(string(line), domain, macros); ok {
				attrs = append(attrs, m)
			}
		}
		if err != nil || (nul && !file) {
			return attrs
		}
	}
}

// readAttributesLine reads a line from br without its line ending, which
// for a blob is the LF alone, and cuts it at a NUL, which it reports. It
// keeps no more than maxAttributesLine+1 bytes of it, enough to tell that
// it reaches maxAttributesLine.
func readAttributesLine(br *bufio.Reader, file bool) (line []byte, nul bool, err error) {
	for {
		chunk, err := br.ReadSlice('\n')
		eol := err == nil
		if eol {
			chunk = chunk[:len(chunk)-1]
		}
		if !nul {
			if i := bytes.IndexByte(chunk, 0); i >= 0 {
				chunk, nul = chunk[:i], true
			}
			line = append(line, chunk[:min(len(chunk), maxAttributesLine+1-len(line))]...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if file && !nul {
			line = bytes.TrimSuffix(line, []byte("\r"))
		}
		return line, nul, err
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

// foldPatterns makes the patterns of attrs, relative to domain, match
// regardless of case when a.fold is set, by lowercasing them to match
// against a lowercased path.
func (a *checkinAttributes) foldPatterns(attrs []gitattributes.MatchAttribute, domain []string) {
	if !a.fold {
		return
	}
	folded := make([]string, len(domain))
	for i, part := range domain {
		folded[i] = asciiLower(part)
	}
	for i := range attrs {
		if attrs[i].Pattern != nil {
			attrs[i].Pattern = gitattributes.ParsePattern(asciiLower(attrs[i].Name), folded)
		}
	}
}

// asciiLower lowercases the ASCII letters of s only, as Git's wildmatch
// does under WM_CASEFOLD[1]: the Kelvin sign U+212A does not match k.
//
// [1]: https://github.com/git/git/blob/v2.54.0/dir.c#L186-L210
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
