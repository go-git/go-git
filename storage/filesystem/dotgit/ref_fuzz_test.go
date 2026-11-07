package dotgit

import (
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
)

// FuzzRefFileContents reads arbitrary contents as a loose reference. Whenever
// they start with a full object name that whitespace or the end of the file
// closes, as in a FETCH_HEAD line, Ref must return that object, as git's
// parse_loose_ref_contents does.
func FuzzRefFileContents(f *testing.F) {
	for _, seed := range []string{
		"",
		"\n",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5\t\tbranch 'main' of https://example.com/repo\n" +
			"e8d3ffab552895c19b9fcf7aa264d277cde33881\tnot-for-merge\ttag 'v1' of https://example.com/repo\n",
		"  6ecf0ef2c2dffb796033e5a02219af86ec6584e5 trailing",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5x\t\t",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5\r\n",
		"2c3f3c8f8e7b0b1ac2b2f8a48c5dd4ebce2fd5ee7a8b3b6c1a1c0d1e2f3a4b5c\t\tbranch 'main' of /srv/repo\n",
		"ref: refs/heads/main\n",
		"ref:refs/heads/main",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, contents []byte) {
		fs := memfs.New()
		if err := util.WriteFile(fs, "FETCH_HEAD", contents, 0o644); err != nil {
			t.Fatalf("writing FETCH_HEAD: %v", err)
		}

		ref, err := New(fs).Ref("FETCH_HEAD")

		line := strings.TrimSpace(string(contents))
		for _, size := range []int{40, 64} {
			if len(line) < size || (len(line) > size && !strings.ContainsRune(" \t\n\r", rune(line[size]))) {
				continue
			}
			name := line[:size]
			if strings.Trim(name, "0123456789abcdefABCDEF") != "" {
				continue
			}

			if err != nil {
				t.Fatalf("Ref(FETCH_HEAD) for %q: %v", contents, err)
			}
			if !strings.EqualFold(ref.Hash().String(), name) {
				t.Fatalf("Ref(FETCH_HEAD) for %q = %s, want %s", contents, ref.Hash(), name)
			}
		}
	})
}
