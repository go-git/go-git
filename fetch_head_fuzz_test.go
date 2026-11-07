package git

import (
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// FuzzFetchHeadRoundTrip writes FETCH_HEAD for a fetched ref with any name,
// object name and remote URL, and checks that every entry is one well-formed
// line, that the URL lost at most its user information, and that FETCH_HEAD
// reads back as the object on its first line.
func FuzzFetchHeadRoundTrip(f *testing.F) {
	for _, seed := range []struct {
		oid       []byte
		name, url string
	}{
		{make([]byte, 20), "refs/heads/main", "https://github.com/go-git/go-git.git"},
		{make([]byte, 32), "refs/tags/v1", "https://user:token@example.com/repo.git/"},
		{[]byte{1, 2, 3}, "HEAD", "git@github.com:go-git/go-git.git"},
		{make([]byte, 40), "refs/pull/1/head", "ssh://git@[::1]:22/repo"},
		{nil, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "/srv/git/user@host/new\nline/.git"},
		{nil, "refs/heads/tab\tname", "a@b"},
	} {
		f.Add(seed.oid, seed.name, seed.url)
	}

	f.Fuzz(func(t *testing.T, oid []byte, name, url string) {
		// git refuses ref names holding a newline, and FETCH_HEAD does not
		// escape them.
		if strings.Contains(name, "\n") {
			t.Skip()
		}

		// Any input names an object, in SHA-256 when it is long enough.
		size := 20
		if len(oid) >= 32 {
			size = 32
		}
		oid = append(oid, make([]byte, size)...)[:size]
		hash, ok := plumbing.FromBytes(oid)
		if !ok {
			t.Fatalf("FromBytes(%x) failed", oid)
		}

		fs := memfs.New()
		st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
		defer func() { _ = st.Close() }()
		r := NewRemote(st, &config.RemoteConfig{Name: "origin", URLs: []string{url}})

		entries := []fetchHeadEntry{
			{hash: hash, name: name, forMerge: true},
			{hash: plumbing.ZeroHash, name: "refs/tags/v1"},
		}
		if err := r.writeFetchHead(entries, nil, url); err != nil {
			t.Fatalf("writeFetchHead: %v", err)
		}

		b, err := util.ReadFile(fs, fetchHeadPath)
		if err != nil {
			t.Fatalf("reading FETCH_HEAD: %v", err)
		}
		lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if len(lines) != len(entries) {
			t.Fatalf("FETCH_HEAD has %d lines, want %d: %q", len(lines), len(entries), b)
		}

		// The URL is the original, or the original without what comes
		// before its first @, back to the scheme.
		anon := anonymizeURL(url)
		if at := strings.IndexByte(url, '@'); anon != url {
			prefix := 0
			if scheme := strings.Index(url, "://"); scheme >= 0 {
				prefix = scheme + len("://")
			}
			if at < 0 || anon != url[:prefix]+url[at+1:] {
				t.Fatalf("anonymizeURL(%q) = %q, which is not %q without user information", url, anon, url)
			}
		}

		for i, line := range lines {
			fields := strings.SplitN(line, "\t", 3)
			if len(fields) != 3 {
				t.Fatalf("line %d is not three tab-separated fields: %q", i, line)
			}
			if fields[0] != entries[i].hash.String() {
				t.Fatalf("line %d names %q, want %q", i, fields[0], entries[i].hash)
			}
			// No object exists, so nothing peels to a commit.
			if fields[1] != "not-for-merge" {
				t.Fatalf("line %d is marked %q, want not-for-merge", i, fields[1])
			}
			if !strings.HasSuffix(fields[2], fetchHeadURL(url)) {
				t.Fatalf("line %d does not end with the URL %q: %q", i, fetchHeadURL(url), line)
			}
		}

		ref, err := st.Reference("FETCH_HEAD")
		if err != nil {
			t.Fatalf("reading FETCH_HEAD as a reference: %v", err)
		}
		if ref.Hash() != hash {
			t.Fatalf("FETCH_HEAD resolves to %s, want %s", ref.Hash(), hash)
		}
	})
}
