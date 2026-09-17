package dotgit

import (
	"bytes"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
)

// FuzzDotGitIsGitDir drives the HEAD and commondir parsing behind the nesting
// check. Their contents may be malformed or locally modified.
//
// Seeds are inline literals, as in the rest of this repository's fuzz targets,
// so the OSS-Fuzz harness that lifts the Fuzz function out compiles them
// standalone.
func FuzzDotGitIsGitDir(f *testing.F) {
	const (
		sha1   = "e8d3ffab552895c19b9fcf7aa264d277cde33881"
		sha256 = "e8d3ffab552895c19b9fcf7aa264d277cde33881e8d3ffab552895c19b9fcf7a"
	)
	// Valid symbolic and detached forms, so the fuzzer mutates from inputs
	// that reach the directory checks rather than only from rejected ones.
	f.Add([]byte("ref: refs/heads/main\n"), []byte("../../common\n"), true)
	f.Add([]byte("ref: refs/heads/main\n"), []byte(nil), false)
	f.Add([]byte(sha1+"\n"), []byte(nil), false)
	f.Add([]byte(sha256+"\n"), []byte(nil), false)

	// Every byte Git's isspace accepts after "ref:", and the two control
	// characters C's isspace accepts that Git's does not.
	f.Add([]byte("ref: \t\r\nrefs/heads/main\n"), []byte(nil), false)
	f.Add([]byte("ref:\vrefs/heads/main\n"), []byte(nil), false)
	f.Add([]byte("ref:\frefs/heads/main\n"), []byte(nil), false)

	// A HEAD truncated at the read limit, one padded past it, one whose hash
	// is cut a byte short of a full SHA-1, one carrying a NUL, and an empty
	// one.
	f.Add([]byte("ref:"+strings.Repeat(" ", 247)+"refs/"), []byte(nil), false)
	f.Add([]byte("ref: refs/heads/main\n"+strings.Repeat(" ", 1000)), []byte(nil), false)
	f.Add([]byte(sha1[:39]), []byte(nil), false)
	f.Add([]byte(sha1+"\x00trailing"), []byte(nil), false)
	f.Add([]byte{}, []byte(nil), false)

	// Commondir forms: relative, absolute, NUL terminated, CRLF terminated,
	// empty, one that leaves the filesystem, and the two sides of the cap.
	f.Add([]byte("ref: refs/heads/main\n"), []byte("/common\n"), true)
	f.Add([]byte("ref: refs/heads/main\n"), []byte("../../common\x00ignored\n"), true)
	f.Add([]byte("ref: refs/heads/main\n"), []byte("../../common\r\n"), true)
	f.Add([]byte("ref: refs/heads/main\n"), []byte{}, true)
	f.Add([]byte("ref: refs/heads/main\n"), []byte("../../../outside\n"), true)
	f.Add([]byte("ref: refs/heads/main\n"), bytes.Repeat([]byte("x"), maxCommonDirSize), true)
	f.Add([]byte("ref: refs/heads/main\n"), bytes.Repeat([]byte("x"), maxCommonDirSize+1), true)

	f.Fuzz(func(t *testing.T, head, commondir []byte, withCommonDir bool) {
		fs := memfs.New()
		// A complete Git directory for a commondir to redirect to, so a
		// resolvable redirect reaches the objects and refs checks instead of
		// stopping at a path that is missing either way.
		for _, dir := range []string{"common/objects", "common/refs", "modules/lib/objects", "modules/lib/refs"} {
			if err := fs.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("creating %s: %v", dir, err)
			}
		}
		if err := util.WriteFile(fs, "common/HEAD", []byte("ref: refs/heads/main\n"), 0o644); err != nil {
			t.Fatalf("writing the common HEAD: %v", err)
		}
		if err := util.WriteFile(fs, "modules/lib/HEAD", head, 0o644); err != nil {
			t.Fatalf("writing HEAD: %v", err)
		}
		if withCommonDir {
			if err := util.WriteFile(fs, "modules/lib/commondir", commondir, 0o644); err != nil {
				t.Fatalf("writing commondir: %v", err)
			}
		}

		nested, err := New(fs).isGitDir("modules/lib")

		if nested && err != nil {
			t.Fatalf("reported a Git directory together with the error %v", err)
		}
	})
}

// FuzzDotGitModule drives the name handling and the walk over its prefixes.
// Submodule names come from .gitmodules, which is content of the repository
// like any other, and a name may contain separators.
//
// Seeds are inline literals, as in the rest of this repository's fuzz targets,
// so the OSS-Fuzz harness that lifts the Fuzz function out compiles them
// standalone.
func FuzzDotGitModule(f *testing.F) {
	// Names whose prefix is the Git directory the fuzz body prepares, so the
	// walk finds something rather than running over absent paths.
	f.Add("lib")
	f.Add("lib/refs/heads")
	f.Add("lib/modules/x")
	f.Add("lib/../lib/refs")

	// Names that leave modules/, name nothing, or spell a prefix oddly.
	f.Add("")
	f.Add(".")
	f.Add("..")
	f.Add("../escape")
	f.Add("/absolute")
	f.Add("a//b")
	f.Add("./lib/./refs")
	f.Add("lib\\refs\\heads")
	f.Add("lib/refs\x00/heads")
	f.Add(strings.Repeat("a/", 512) + "b")

	f.Fuzz(func(t *testing.T, name string) {
		fs := memfs.New()
		for _, dir := range []string{"modules/lib/objects", "modules/lib/refs"} {
			if err := fs.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("creating %s: %v", dir, err)
			}
		}
		if err := util.WriteFile(fs, "modules/lib/HEAD", []byte("ref: refs/heads/main\n"), 0o644); err != nil {
			t.Fatalf("writing HEAD: %v", err)
		}

		sub, err := New(fs).Module(name)
		if (sub == nil) == (err == nil) {
			t.Fatalf("Module(%q) returned filesystem %v with error %v", name, sub, err)
		}
		if err != nil {
			return
		}

		// The chroot is what bounds every later read and write, so a root
		// outside modules/ hands the caller a filesystem over paths the name
		// was never checked against.
		modules := path.Clean(filepath.ToSlash(fs.Join(fs.Root(), modulePath)))
		root := path.Clean(filepath.ToSlash(sub.Root()))
		if root != modules && !strings.HasPrefix(root, modules+"/") {
			t.Fatalf("Module(%q) is rooted at %q, outside %q", name, root, modules)
		}
	})
}
