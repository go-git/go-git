package packfile_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

// similarBlobs stores blobs that share most of their content, so that
// the encoder picks deltas between them.
func similarBlobs(t *testing.T, s storer.EncodedObjectStorer) []plumbing.Hash {
	t.Helper()
	base := strings.Repeat("line of shared content\n", 200)
	ids := make([]plumbing.Hash, 0, 5)
	for i := range cap(ids) {
		o := s.NewEncodedObject()
		o.SetType(plumbing.BlobObject)
		w, err := o.Writer()
		require.NoError(t, err)
		_, err = fmt.Fprintf(w, "%sversion %d\n", base, i)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		id, err := s.SetEncodedObject(o)
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return ids
}

func encode(t *testing.T, s storer.EncodedObjectStorer, ids []plumbing.Hash, refDeltas bool, opts ...packfile.EncoderOption) ([]byte, plumbing.Hash) {
	t.Helper()
	var buf bytes.Buffer
	sum, err := packfile.NewEncoder(&buf, s, refDeltas, opts...).Encode(ids, 10)
	require.NoError(t, err)
	return buf.Bytes(), sum
}

func TestSHA256PackRoundTrip(t *testing.T) {
	t.Parallel()

	for _, refDeltas := range []bool{false, true} {
		t.Run("refDeltas="+strconv.FormatBool(refDeltas), func(t *testing.T) {
			t.Parallel()

			src := memory.NewStorage(memory.WithObjectFormat(hash.SHA256))
			ids := similarBlobs(t, src)
			opt := packfile.WithObjectFormat(hash.SHA256)
			pack, sum := encode(t, src, ids, refDeltas, opt)
			assert.Equal(t, hash.SHA256, sum.Format())

			s := packfile.NewScanner(bytes.NewReader(pack), opt)
			var objects, deltas int
			var footer plumbing.Hash
			for s.Scan() {
				switch d := s.Data(); d.Section {
				case packfile.ObjectSection:
					objects++
					if d.Value().(packfile.ObjectHeader).Type.IsDelta() {
						deltas++
					}
				case packfile.FooterSection:
					footer = d.Value().(plumbing.Hash)
				}
			}
			require.NoError(t, s.Error())
			assert.Equal(t, len(ids), objects)
			assert.NotZero(t, deltas)
			assert.Equal(t, sum, footer)

			w := new(idxfile.Writer)
			dst := memory.NewStorage(memory.WithObjectFormat(hash.SHA256))
			got, err := packfile.NewParser(bytes.NewReader(pack), opt,
				packfile.WithScannerObservers(w), packfile.WithStorage(dst)).Parse()
			require.NoError(t, err)
			assert.Equal(t, sum, got)
			for _, id := range ids {
				require.NoError(t, dst.HasEncodedObject(id))
			}

			idx, err := w.Index()
			require.NoError(t, err)
			assert.Equal(t, hash.SHA256, idx.ObjectFormat())

			fs := memfs.New()
			f, err := fs.Create("pack")
			require.NoError(t, err)
			_, err = f.Write(pack)
			require.NoError(t, err)

			for _, pfOpts := range [][]packfile.PackfileOption{nil, {packfile.WithFs(fs)}} {
				p := packfile.NewPackfile(f, append(pfOpts, packfile.WithIdx(idx), opt)...)
				for _, id := range ids {
					want, err := src.EncodedObject(plumbing.AnyObject, id)
					require.NoError(t, err)
					obj, err := p.Get(id)
					require.NoError(t, err)
					assert.Equal(t, id, obj.Hash())
					assert.Equal(t, readAll(t, want), readAll(t, obj))
				}
			}
		})
	}
}

func readAll(t *testing.T, o plumbing.EncodedObject) []byte {
	t.Helper()
	r, err := o.Reader()
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return b
}

func TestUnsupportedObjectFormat(t *testing.T) {
	t.Parallel()

	src := memory.NewStorage()
	ids := similarBlobs(t, src)
	pack, _ := encode(t, src, ids, false)
	opt := packfile.WithObjectFormat("md5")

	t.Run("scanner", func(t *testing.T) {
		t.Parallel()
		s := packfile.NewScanner(bytes.NewReader(pack), opt)
		assert.False(t, s.Scan())
		require.ErrorIs(t, s.Error(), hash.ErrUnsupportedHashFunction)
	})

	t.Run("parser", func(t *testing.T) {
		t.Parallel()
		_, err := packfile.NewParser(bytes.NewReader(pack), opt).Parse()
		require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
	})

	t.Run("packfile", func(t *testing.T) {
		t.Parallel()
		w := new(idxfile.Writer)
		_, err := packfile.NewParser(bytes.NewReader(pack), packfile.WithScannerObservers(w)).Parse()
		require.NoError(t, err)
		idx, err := w.Index()
		require.NoError(t, err)

		fs := memfs.New()
		f, err := fs.Create("pack")
		require.NoError(t, err)
		_, err = f.Write(pack)
		require.NoError(t, err)

		_, err = packfile.NewPackfile(f, packfile.WithIdx(idx), opt).Get(ids[0])
		require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
	})

	t.Run("encoder", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		_, err := packfile.NewEncoder(&buf, src, false, opt).Encode(ids, 10)
		require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
		assert.Zero(t, buf.Len())
	})
}

func TestEncoderRejectsObjectsOfAnotherFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		storage hash.ObjectFormat
		opts    []packfile.EncoderOption
	}{
		{"sha1 objects in a sha256 pack", hash.SHA1, []packfile.EncoderOption{packfile.WithObjectFormat(hash.SHA256)}},
		{"sha256 objects in a default pack", hash.SHA256, nil},
		{"sha256 objects in a sha1 pack", hash.SHA256, []packfile.EncoderOption{packfile.WithObjectFormat(hash.SHA1)}},
	}
	for _, tc := range tests {
		for _, refDeltas := range []bool{false, true} {
			t.Run(tc.name+"/refDeltas="+strconv.FormatBool(refDeltas), func(t *testing.T) {
				t.Parallel()
				src := memory.NewStorage(memory.WithObjectFormat(tc.storage))
				ids := similarBlobs(t, src)

				var buf bytes.Buffer
				_, err := packfile.NewEncoder(&buf, src, refDeltas, tc.opts...).Encode(ids, 10)
				require.ErrorIs(t, err, hash.ErrObjectFormatMismatch)
				assert.Zero(t, buf.Len())
			})
		}
	}
}

func TestPackfileRejectsIndexOfAnotherFormat(t *testing.T) {
	t.Parallel()

	src := memory.NewStorage(memory.WithObjectFormat(hash.SHA256))
	ids := similarBlobs(t, src)
	opt := packfile.WithObjectFormat(hash.SHA256)
	pack, _ := encode(t, src, ids, false, opt)

	w := new(idxfile.Writer)
	_, err := packfile.NewParser(bytes.NewReader(pack), opt, packfile.WithScannerObservers(w)).Parse()
	require.NoError(t, err)
	idx, err := w.Index()
	require.NoError(t, err)

	fs := memfs.New()
	f, err := fs.Create("pack")
	require.NoError(t, err)
	_, err = f.Write(pack)
	require.NoError(t, err)

	_, err = packfile.NewPackfile(f, packfile.WithIdx(idx)).Get(ids[0])
	require.ErrorIs(t, err, hash.ErrObjectFormatMismatch)

	_, err = packfile.NewPackfile(f, packfile.WithIdx(otherIndex{idx})).Get(ids[0])
	require.ErrorIs(t, err, hash.ErrObjectFormatMismatch)
}

// otherIndex is an idxfile.Index that is not a *idxfile.MemoryIndex.
type otherIndex struct {
	idxfile.Index
}

func TestZeroObjectFormatOptionIsSHA1(t *testing.T) {
	t.Parallel()

	src := memory.NewStorage()
	ids := similarBlobs(t, src)
	pack, sum := encode(t, src, ids, false)

	w := new(idxfile.Writer)
	_, err := packfile.NewParser(bytes.NewReader(pack), packfile.WithScannerObservers(w)).Parse()
	require.NoError(t, err)
	idx, err := w.Index()
	require.NoError(t, err)

	fs := memfs.New()
	f, err := fs.Create("pack")
	require.NoError(t, err)
	_, err = f.Write(pack)
	require.NoError(t, err)

	for name, opt := range map[string]packfile.ObjectFormatOption{
		"zero value": {},
		"unset":      packfile.WithObjectFormat(hash.UnsetObjectFormat),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := packfile.NewScanner(bytes.NewReader(pack), opt)
			for s.Scan() {
			}
			require.NoError(t, s.Error())

			got, err := packfile.NewParser(bytes.NewReader(pack), opt).Parse()
			require.NoError(t, err)
			assert.Equal(t, sum, got)

			obj, err := packfile.NewPackfile(f, packfile.WithIdx(idx), opt).Get(ids[0])
			require.NoError(t, err)
			assert.Equal(t, ids[0], obj.Hash())

			encoded, encodedSum := encode(t, src, ids, false, opt)
			assert.Equal(t, pack, encoded)
			assert.Equal(t, sum, encodedSum)
		})
	}
}

func TestEncoderRejectsDeltaBaseOfAnotherFormat(t *testing.T) {
	t.Parallel()

	sha256Src := memory.NewStorage(memory.WithObjectFormat(hash.SHA256))
	target, err := sha256Src.EncodedObject(plumbing.AnyObject, similarBlobs(t, sha256Src)[0])
	require.NoError(t, err)
	sha1Src := memory.NewStorage()
	base, err := sha1Src.EncodedObject(plumbing.AnyObject, similarBlobs(t, sha1Src)[0])
	require.NoError(t, err)

	objects := []*packfile.ObjectToPack{{
		Object:   target,
		Original: target,
		Base:     &packfile.ObjectToPack{Object: base, Original: base},
	}}

	var buf bytes.Buffer
	_, err = packfile.NewEncoder(&buf, sha256Src, false,
		packfile.WithObjectFormat(hash.SHA256),
		packfile.WithObjectSelector(fixedSelector(objects))).Encode(nil, 10)
	require.ErrorIs(t, err, hash.ErrObjectFormatMismatch)
	assert.ErrorContains(t, err, "delta base")
	assert.Zero(t, buf.Len())
}

type fixedSelector []*packfile.ObjectToPack

func (s fixedSelector) ObjectsToPack([]plumbing.Hash, uint) ([]*packfile.ObjectToPack, error) {
	return s, nil
}

func TestScannerRejectsPackOfAnotherFormat(t *testing.T) {
	t.Parallel()

	sha1Src := memory.NewStorage()
	sha1Pack, _ := encode(t, sha1Src, similarBlobs(t, sha1Src), false)
	sha256Src := memory.NewStorage(memory.WithObjectFormat(hash.SHA256))
	sha256Pack, _ := encode(t, sha256Src, similarBlobs(t, sha256Src), false, packfile.WithObjectFormat(hash.SHA256))

	tests := []struct {
		name   string
		pack   []byte
		format hash.ObjectFormat
	}{
		{"sha1 pack read as sha256", sha1Pack, hash.SHA256},
		{"sha256 pack read as sha1", sha256Pack, hash.SHA1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := packfile.NewScanner(bytes.NewReader(tc.pack), packfile.WithObjectFormat(tc.format))
			for s.Scan() {
			}
			require.ErrorIs(t, s.Error(), packfile.ErrMalformedPackfile)

			_, err := packfile.NewParser(bytes.NewReader(tc.pack), packfile.WithObjectFormat(tc.format)).Parse()
			require.ErrorIs(t, err, packfile.ErrMalformedPackfile)
		})
	}
}

func requireGitAtLeast(t *testing.T, wantMajor, wantMinor int) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not found in PATH")
	}
	out, err := gitenv.Command("git", "version").Output()
	if err != nil {
		t.Skipf("cannot run git version: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		t.Skipf("cannot parse git version %q", out)
	}
	parts := strings.SplitN(fields[2], ".", 3)
	major, errMajor := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if errMajor != nil || major < wantMajor || (major == wantMajor && minor < wantMinor) {
		t.Skipf("git %s is older than %d.%d", fields[2], wantMajor, wantMinor)
	}
}

func runGit(t *testing.T, dir string, stdin []byte, args ...string) string {
	t.Helper()
	cmd := gitenv.Command("git", args...)
	require.NoError(t, cmd.Err)
	cmd.Dir = dir
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com",
		"GIT_COMMITTER_NAME=c", "GIT_COMMITTER_EMAIL=c@example.com",
		"GIT_AUTHOR_DATE=1700000000 +0000", "GIT_COMMITTER_DATE=1700000000 +0000",
	)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// gitSHA256Repo creates a SHA-256 repository whose history rewrites one
// file a few times, so that a repack stores some of its versions as
// deltas.
func gitSHA256Repo(t *testing.T) string {
	t.Helper()
	// SHA-256 repositories need git 2.29.
	requireGitAtLeast(t, 2, 29)

	dir := t.TempDir()
	runGit(t, dir, nil, "init", "--quiet", "--object-format=sha256", "--initial-branch=main")
	base := strings.Repeat("line of shared content\n", 200)
	for i := range 5 {
		content := fmt.Sprintf("%sversion %d\n", base, i)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file"), []byte(content), 0o644))
		runGit(t, dir, nil, "add", "file")
		runGit(t, dir, nil, "commit", "--quiet", "-m", "version "+strconv.Itoa(i))
	}
	return dir
}

// allObjects lists the ids and types of every object in the repository
// at dir, as `git cat-file --batch-check` prints them.
func allObjects(t *testing.T, dir string) []string {
	t.Helper()
	out := runGit(t, dir, nil, "cat-file", "--batch-all-objects", "--batch-check=%(objectname) %(objecttype)")
	lines := strings.Split(out, "\n")
	sort.Strings(lines)
	return lines
}

func TestGitAcceptsGoGitSHA256Pack(t *testing.T) {
	t.Parallel()

	for _, refDeltas := range []bool{false, true} {
		t.Run("refDeltas="+strconv.FormatBool(refDeltas), func(t *testing.T) {
			t.Parallel()
			srcDir := gitSHA256Repo(t)
			src := filesystem.NewStorage(osfs.New(filepath.Join(srcDir, ".git")), cache.NewObjectLRUDefault())
			t.Cleanup(func() { _ = src.Close() })

			iter, err := src.IterEncodedObjects(plumbing.AnyObject)
			require.NoError(t, err)
			var ids []plumbing.Hash
			require.NoError(t, iter.ForEach(func(o plumbing.EncodedObject) error {
				ids = append(ids, o.Hash())
				return nil
			}))

			pack, sum := encode(t, src, ids, refDeltas, packfile.WithObjectFormat(hash.SHA256))

			dstDir := t.TempDir()
			runGit(t, dstDir, nil, "init", "--quiet", "--bare", "--object-format=sha256")
			out := runGit(t, dstDir, pack, "index-pack", "--stdin", "--strict")
			assert.Equal(t, "pack\t"+sum.String(), out)

			idxPath := filepath.Join(dstDir, "objects", "pack", "pack-"+sum.String()+".idx")
			verify := runGit(t, dstDir, nil, "verify-pack", "-v", idxPath)
			assert.Contains(t, verify, "chain length = 1:", "the pack has no deltas")
			runGit(t, dstDir, nil, "fsck", "--full", "--strict")
			assert.Equal(t, allObjects(t, srcDir), allObjects(t, dstDir))
		})
	}
}

func TestParseGitSHA256Pack(t *testing.T) {
	t.Parallel()

	for _, ofsDeltas := range []bool{true, false} {
		t.Run("ofsDeltas="+strconv.FormatBool(ofsDeltas), func(t *testing.T) {
			t.Parallel()
			dir := gitSHA256Repo(t)
			runGit(t, dir, nil, "-c", "repack.useDeltaBaseOffset="+strconv.FormatBool(ofsDeltas),
				"repack", "-adf", "-q", "--no-write-bitmap-index")

			packs, err := filepath.Glob(filepath.Join(dir, ".git", "objects", "pack", "pack-*.pack"))
			require.NoError(t, err)
			require.Len(t, packs, 1)
			pack, err := os.ReadFile(packs[0])
			require.NoError(t, err)
			gitIdx, err := os.ReadFile(strings.TrimSuffix(packs[0], ".pack") + ".idx")
			require.NoError(t, err)
			name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(packs[0]), "pack-"), ".pack")

			wantDelta := plumbing.REFDeltaObject
			if ofsDeltas {
				wantDelta = plumbing.OFSDeltaObject
			}
			opt := packfile.WithObjectFormat(hash.SHA256)
			s := packfile.NewScanner(bytes.NewReader(pack), opt)
			deltaTypes := map[plumbing.ObjectType]bool{}
			for s.Scan() {
				if d := s.Data(); d.Section == packfile.ObjectSection {
					if typ := d.Value().(packfile.ObjectHeader).Type; typ.IsDelta() {
						deltaTypes[typ] = true
					}
				}
			}
			require.NoError(t, s.Error())
			assert.Equal(t, map[plumbing.ObjectType]bool{wantDelta: true}, deltaTypes)

			w := new(idxfile.Writer)
			dst := memory.NewStorage(memory.WithObjectFormat(hash.SHA256))
			sum, err := packfile.NewParser(bytes.NewReader(pack), opt,
				packfile.WithScannerObservers(w), packfile.WithStorage(dst)).Parse()
			require.NoError(t, err)
			assert.Equal(t, name, sum.String())

			idx, err := w.Index()
			require.NoError(t, err)
			var idxBuf bytes.Buffer
			require.NoError(t, idxfile.Encode(&idxBuf, idx))
			assert.Equal(t, gitIdx, idxBuf.Bytes())

			f, err := osfs.New(filepath.Dir(packs[0])).Open(filepath.Base(packs[0]))
			require.NoError(t, err)
			p := packfile.NewPackfile(f, packfile.WithIdx(idx), opt)
			defer func() { _ = p.Close() }()
			for _, line := range allObjects(t, dir) {
				id, ok := plumbing.FromHex(strings.Fields(line)[0])
				require.True(t, ok)
				obj, err := p.Get(id)
				require.NoError(t, err)
				assert.Equal(t, id, obj.Hash())
				require.NoError(t, dst.HasEncodedObject(id))
			}
		})
	}
}

func TestEncoderEmptyPackUsesEncoderFormat(t *testing.T) {
	t.Parallel()

	src := memory.NewStorage(memory.WithObjectFormat(hash.SHA256))

	pack, sum := encode(t, src, nil, false)
	assert.Equal(t, hash.SHA1, sum.Format())
	assert.Len(t, pack, 12+hash.SHA1Size)

	pack, sum = encode(t, src, nil, false, packfile.WithObjectFormat(hash.SHA256))
	assert.Equal(t, hash.SHA256, sum.Format())
	assert.Len(t, pack, 12+hash.SHA256Size)
}
