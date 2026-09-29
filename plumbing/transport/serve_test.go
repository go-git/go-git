package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func testServe[T UploadPackRequest | ReceivePackRequest](
	t testing.TB,
	st storage.Storer,
	fun func(
		ctx context.Context,
		st storage.Storer,
		r io.ReadCloser,
		w io.WriteCloser,
		opts *T,
	) error,
	r io.ReadCloser,
	opts *T,
) *bytes.Buffer {
	var out bytes.Buffer
	err := fun(
		context.TODO(),
		st,
		r,
		ioutil.WriteNopCloser(&out),
		opts,
	)
	require.NoError(t, err)
	require.Greater(t, out.Len(), 0)
	return &out
}

func testAdvertise[T UploadPackRequest | ReceivePackRequest](
	t testing.TB,
	fun func(
		ctx context.Context,
		st storage.Storer,
		r io.ReadCloser,
		w io.WriteCloser,
		opts *T,
	) error,
	proto string,
	stateless bool,
) *bytes.Buffer {
	dot, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(t.TempDir))
	if err != nil {
		t.Fatal(err)
	}
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	defer func() { _ = st.Close() }()
	opts := new(T)
	switch o := any(opts).(type) {
	case *UploadPackRequest:
		o.GitProtocol = proto
		o.AdvertiseRefs = true
		o.StatelessRPC = stateless
	case *ReceivePackRequest:
		o.GitProtocol = proto
		o.AdvertiseRefs = true
		o.StatelessRPC = stateless
	}
	return testServe(t, st, fun, io.NopCloser(bytes.NewBuffer(nil)), opts)
}

// A reference store reports what is on disk. The advertisement excludes
// malformed Git names, including stale lock files.
func TestAdvertiseRefsSkipsUnadvertisableNames(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	st := memory.NewStorage()
	for _, n := range []plumbing.ReferenceName{
		"refs/heads/main",
		"refs/heads/main.lock",
		"refs/heads/.hidden",
		"refs/heads/bad~name",
		"refs/heads/a..b",
		"CONFIG",
	} {
		require.NoError(t, st.SetReference(plumbing.NewHashReference(n, hash)))
	}

	var buf bytes.Buffer
	require.NoError(t, AdvertiseRefs(
		context.Background(), st, ioutil.WriteNopCloser(&buf),
		UploadPackService, false, protocol.V0,
	))

	// The first advertised line carries the capability list after a NUL, so
	// match the name against what precedes it.
	out := buf.String()
	assert.Contains(t, out, hash.String()+" refs/heads/main\x00")
	for _, n := range []string{
		"refs/heads/main.lock", "refs/heads/.hidden",
		"refs/heads/bad~name", "refs/heads/a..b", "CONFIG",
	} {
		assert.NotContains(t, out, n, "%q must not be advertised", n)
	}
}

func TestAdvertisable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name plumbing.ReferenceName
		want bool
	}{
		{"HEAD", true},
		{"refs/heads/main", true},
		{"refs/heads/@", true},
		{"refs/heads/-foo", true},
		{"refs/heads/\u200c./main", true},
		{"refs/stash", true},
		{"refs/heads/main.lock", false},
		{"refs/heads/.hidden", false},
		{"refs/heads/bad~name", false},
		{"CONFIG", false},
		{"ORIG_HEAD", false},
		// Well-formed by Validate, but not under refs/ and not HEAD, so the
		// prefix arm is the only thing that decides it.
		{"foo/bar", false},
		{"refs/../config", false},
	} {
		assert.Equal(t, tc.want, advertisable(tc.name), "advertisable(%q)", tc.name)
	}
}

// The advertisement filter also covers protocol v2 ls-refs. The v2 grammar
// raises the stakes: a ref line is "obj-id SP refname *(SP ref-attribute)",
// so a name holding a space arrives at the peer as a different ref plus an
// attribute it never sent.
func TestServeLsRefsV2SkipsUnadvertisableNames(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	st := memory.NewStorage()
	for _, n := range []plumbing.ReferenceName{
		"refs/heads/main",
		"refs/heads/main.lock",
		"refs/heads/bad~name",
		"refs/heads/sp ace",
		"CONFIG",
	} {
		require.NoError(t, st.SetReference(plumbing.NewHashReference(n, hash)))
	}

	var buf bytes.Buffer
	require.NoError(t, serveLsRefsV2(context.Background(), st, &buf, &packp.LsRefsArgs{}))

	out := buf.String()
	assert.Contains(t, out, "refs/heads/main\n")
	for _, n := range []string{"refs/heads/main.lock", "bad~name", "sp ace", "CONFIG"} {
		assert.NotContains(t, out, n, "%q must not be advertised over v2", n)
	}
}

// The symref capability names a reference too. Advertising a target that was
// just withheld contradicts the rest of the advertisement, and go-git's own
// client refuses such a clone outright.
func TestAdvertiseRefsSkipsUnadvertisableSymrefTarget(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	st := memory.NewStorage()
	require.NoError(t, st.SetReference(plumbing.NewHashReference("refs/heads/main.lock", hash)))
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main.lock")))

	var buf bytes.Buffer
	require.NoError(t, AdvertiseRefs(
		context.Background(), st, ioutil.WriteNopCloser(&buf),
		UploadPackService, false, protocol.V0,
	))

	out := buf.String()
	assert.NotContains(t, out, "symref=HEAD:refs/heads/main.lock")
	assert.NotContains(t, out, "refs/heads/main.lock")
}

// A symref whose target the store will not hand back costs that entry, not
// the advertisement. Before this, one such reference made the server serve
// nothing at all.
func TestAdvertiseRefsSurvivesUnresolvableSymref(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	st := memory.NewStorage()
	require.NoError(t, st.SetReference(plumbing.NewHashReference("refs/heads/main", hash)))
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference("refs/heads/sym", "CONFIG")))

	var buf bytes.Buffer
	require.NoError(t, AdvertiseRefs(
		context.Background(), st, ioutil.WriteNopCloser(&buf),
		UploadPackService, false, protocol.V0,
	))

	assert.Contains(t, buf.String(), "refs/heads/main")
}

func TestAdvertiseRefsSkipsCyclicSymrefs(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	require.NoError(t, st.SetReference(plumbing.NewHashReference("refs/heads/main", hash)))
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference("refs/heads/loop", "refs/heads/loop")))
	var buf bytes.Buffer
	require.NoError(t, AdvertiseRefs(context.Background(), st, ioutil.WriteNopCloser(&buf), UploadPackService, false, protocol.V0))
	assert.Contains(t, buf.String(), "refs/heads/main")
	assert.NotContains(t, buf.String(), "refs/heads/loop")
}

type referenceReadErrorStorage struct {
	storage.Storer
	err error
}

func (s referenceReadErrorStorage) Reference(plumbing.ReferenceName) (*plumbing.Reference, error) {
	return nil, s.err
}

func TestAdvertiseRefsPropagatesSymrefReadError(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))
	want := errors.New("reference storage unavailable")
	var buf bytes.Buffer
	err := AdvertiseRefs(context.Background(), referenceReadErrorStorage{st, want}, ioutil.WriteNopCloser(&buf), UploadPackService, false, protocol.V0)
	require.ErrorIs(t, err, want)
}

func TestServeLsRefsV2PropagatesSymrefReadError(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))
	want := errors.New("reference storage unavailable")
	var buf bytes.Buffer
	err := serveLsRefsV2(context.Background(), referenceReadErrorStorage{st, want}, &buf, &packp.LsRefsArgs{Symrefs: true})
	require.ErrorIs(t, err, want)
}

func TestServeLsRefsV2SkipsUnresolvableSymrefs(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	require.NoError(t, st.SetReference(plumbing.NewHashReference("refs/heads/main", hash)))
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference("refs/heads/loop", "refs/heads/loop")))
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference("refs/heads/missing", "refs/heads/absent")))
	var buf bytes.Buffer
	require.NoError(t, serveLsRefsV2(context.Background(), st, &buf, &packp.LsRefsArgs{Symrefs: true}))
	assert.Contains(t, buf.String(), hash.String()+" refs/heads/main\n")
	assert.NotContains(t, buf.String(), "refs/heads/loop")
	assert.NotContains(t, buf.String(), "refs/heads/missing")
}

type referenceIterationErrorStorage struct {
	storage.Storer
	err error
}

func (s referenceIterationErrorStorage) IterReferences() (storer.ReferenceIter, error) {
	return referenceIterationError{storer.NewReferenceSliceIter(nil), s.err}, nil
}

type referenceIterationError struct {
	storer.ReferenceIter
	err error
}

func (i referenceIterationError) ForEach(func(*plumbing.Reference) error) error {
	return i.err
}

func TestServeLsRefsV2PropagatesIterationError(t *testing.T) {
	t.Parallel()

	want := errors.New("reference iteration failed")
	st := referenceIterationErrorStorage{memory.NewStorage(), want}
	var buf bytes.Buffer
	err := serveLsRefsV2(context.Background(), st, &buf, &packp.LsRefsArgs{})
	require.ErrorIs(t, err, want)
}

func TestServeLsRefsV2SymrefTargets(t *testing.T) {
	t.Parallel()

	for _, target := range []plumbing.ReferenceName{"refs/heads/main", "refs/heads/\u200c./main", "refs/heads/main.lock", "refs/heads/sp ace", "CONFIG"} {
		t.Run(target.String(), func(t *testing.T) {
			t.Parallel()

			st := memory.NewStorage()
			hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
			require.NoError(t, st.SetReference(plumbing.NewHashReference(target, hash)))
			require.NoError(t, st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, target)))
			var buf bytes.Buffer
			require.NoError(t, serveLsRefsV2(context.Background(), st, &buf, &packp.LsRefsArgs{Symrefs: true}))
			assert.Contains(t, buf.String(), hash.String()+" HEAD")
			if target == "refs/heads/main" || target == "refs/heads/\u200c./main" {
				assert.Contains(t, buf.String(), "symref-target:"+target.String()+"\n")
			} else {
				assert.NotContains(t, buf.String(), "symref-target:")
				assert.NotContains(t, buf.String(), target.String())
			}
		})
	}
}

func TestAdvertisementsResolveSymbolicTargets(t *testing.T) {
	t.Parallel()

	for _, intermediate := range []plumbing.ReferenceName{"refs/heads/alias", "ORIG_HEAD"} {
		for _, target := range []plumbing.ReferenceName{"refs/heads/main", "refs/heads/main.lock"} {
			t.Run(intermediate.String()+"/"+target.String(), func(t *testing.T) {
				t.Parallel()

				st := memory.NewStorage()
				hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
				require.NoError(t, st.SetReference(plumbing.NewHashReference(target, hash)))
				require.NoError(t, st.SetReference(plumbing.NewSymbolicReference(intermediate, target)))
				require.NoError(t, st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, intermediate)))
				var v0, v2 bytes.Buffer
				require.NoError(t, AdvertiseRefs(context.Background(), st, ioutil.WriteNopCloser(&v0), UploadPackService, false, protocol.V0))
				require.NoError(t, serveLsRefsV2(context.Background(), st, &v2, &packp.LsRefsArgs{Symrefs: true}))
				assert.Contains(t, v0.String(), hash.String()+" HEAD")
				assert.Contains(t, v2.String(), hash.String()+" HEAD")
				assert.NotContains(t, v0.String(), "symref=HEAD:"+intermediate.String())
				assert.NotContains(t, v2.String(), "symref-target:"+intermediate.String())
				if target == "refs/heads/main" {
					assert.Contains(t, v0.String(), "symref=HEAD:"+target.String())
					assert.Contains(t, v2.String(), "HEAD symref-target:"+target.String()+"\n")
				} else {
					assert.NotContains(t, v0.String(), "symref=HEAD:")
					assert.NotContains(t, v2.String(), "symref-target:")
				}
			})
		}
	}
}

func TestAdvertisementsSkipFilesystemSymlinkLoops(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	require.NoError(t, st.SetReference(plumbing.NewHashReference("refs/heads/main", hash)))
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference("refs/heads/alias", "ORIG_HEAD")))
	broken := referenceReadErrorStorage{st, &os.PathError{Op: "stat", Path: "ORIG_HEAD", Err: syscall.ELOOP}}
	var v0, v2 bytes.Buffer
	require.NoError(t, AdvertiseRefs(context.Background(), broken, ioutil.WriteNopCloser(&v0), UploadPackService, false, protocol.V0))
	require.NoError(t, serveLsRefsV2(context.Background(), broken, &v2, &packp.LsRefsArgs{Symrefs: true}))
	for _, out := range []string{v0.String(), v2.String()} {
		assert.Contains(t, out, "refs/heads/main")
		assert.NotContains(t, out, "refs/heads/alias")
	}
}
