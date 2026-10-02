package dotgit

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

// A loose ref file either fails with ErrEmptyRefFile or ErrBrokenRefFile, or
// decodes to a value that encodes back to itself, and an object ID it accepts
// has the length of a supported object format, as git's parser requires.
func FuzzLooseRefDecode(f *testing.F) {
	for _, seed := range []string{
		"", "\n", "garbage\n",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881\n",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881 extra\n",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881\r\n",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881\v\n",
		"  e8d3ffab552895c19b9fcf7aa264d277cde33881\n",
		"e8d3ffab552895c19b9fcf7aa264d277cde3388\n",
		strings.Repeat("ab", 32) + "\n",
		"ref: refs/heads/main\n", "ref:refs/heads/main", "ref:\t refs/heads/main \n", "ref:\n",
	} {
		f.Add([]byte(seed))
	}

	d := New(memfs.New())
	f.Fuzz(func(t *testing.T, data []byte) {
		ref, err := d.readReferenceFrom(bytes.NewReader(data), "refs/heads/x")
		if err != nil {
			if !errors.Is(err, ErrEmptyRefFile) && !errors.Is(err, ErrBrokenRefFile) {
				t.Fatalf("unexpected error for %q: %v", data, err)
			}
			return
		}

		var encoded string
		switch ref.Type() {
		case plumbing.HashReference:
			id := string(data)
			if i := strings.IndexAny(id, " \t\n\r"); i >= 0 {
				id = id[:i]
			}
			if l := len(id); l != formatcfg.SHA1HexSize && l != formatcfg.SHA256HexSize {
				t.Fatalf("accepted an object ID of length %d in %q", l, data)
			}
			encoded = ref.Hash().String() + "\n"
		case plumbing.SymbolicReference:
			if ref.Target() == "" {
				t.Fatalf("accepted an empty symbolic target in %q", data)
			}
			encoded = "ref: " + ref.Target().String() + "\n"
		default:
			t.Fatalf("unexpected reference type for %q: %v", data, ref.Type())
		}

		again, err := d.readReferenceFrom(strings.NewReader(encoded), "refs/heads/x")
		if err != nil {
			t.Fatalf("re-encoded %q does not decode: %v", encoded, err)
		}
		if again.String() != ref.String() {
			t.Fatalf("round trip changed %q to %q", ref, again)
		}
	})
}

// Iterating a packed-refs file under any prefix either fails, as Refs does,
// or yields names that match the prefix, in ascending order, each once, that
// are exactly the ones Refs yields under it, whatever the file's header
// claims about its order.
func FuzzPackedRefsWithPrefix(f *testing.F) {
	const a, b = "e8d3ffab552895c19b9fcf7aa264d277cde33881", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	for _, seed := range []struct{ packed, prefix string }{
		{"", ""},
		{"# pack-refs with: peeled fully-peeled sorted \n" + a + " refs/heads/main\n" + a + " refs/tags/v1\n^" + b + "\n", "refs/heads/"},
		{"# pack-refs with: sorted \n" + a + " refs/tags/v1\n" + a + " refs/heads/main\n", "refs/"},
		{a + " refs/heads/main\r\n" + b + " refs/heads/main\r\n", "refs/heads/m"},
		{a + " refs/heads/a/b\n" + a + " refs/heads/a-c\n" + a + " refs/heads/a0\n", "refs/heads/a"},
		{"malformed line\n" + a + " refs/heads/main\n", "r"},
	} {
		f.Add(seed.packed, seed.prefix)
	}

	f.Fuzz(func(t *testing.T, packed, prefix string) {
		fs := memfs.New()
		if err := util.WriteFile(fs, packedRefsPath, []byte(packed), 0o644); err != nil {
			t.Fatal(err)
		}
		d := New(fs)

		iter, err := d.RefsWithPrefix(prefix)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		if err := iter.ForEach(func(r *plumbing.Reference) error {
			got = append(got, r.Name().String())
			return nil
		}); err != nil {
			if _, refsErr := d.Refs(); refsErr == nil {
				t.Fatalf("prefix iteration failed where Refs did not: %v", err)
			}
			return
		}

		all, err := d.Refs()
		if err != nil {
			t.Fatalf("Refs failed where prefix iteration did not: %v", err)
		}
		var want []string
		for _, r := range all {
			if strings.HasPrefix(r.Name().String(), prefix) {
				want = append(want, r.Name().String())
			}
		}
		for i, name := range got {
			if !strings.HasPrefix(name, prefix) {
				t.Fatalf("%q does not match prefix %q", name, prefix)
			}
			if i > 0 && got[i-1] >= name {
				t.Fatalf("%q does not sort after %q", name, got[i-1])
			}
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("prefix %q yields %q, Refs %q", prefix, got, want)
		}
	})
}
