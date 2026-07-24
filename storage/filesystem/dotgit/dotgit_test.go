package dotgit

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/storage"
)

type SuiteDotGit struct {
	suite.Suite
}

func TestSuiteDotGit(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SuiteDotGit))
}

func (s *SuiteDotGit) EmptyFS() (fs billy.Filesystem) { return memfs.New() }

func (s *SuiteDotGit) TestModuleRejectsEscapingNames() {
	d := New(s.EmptyFS())
	// Only true path-traversal cases — names like "/etc" or
	// "modules/../escape" land inside modules/ once Join cleans
	// them, so the containment check correctly accepts those.
	bad := []string{
		"..",
		"../x",
		"foo/../..",
		"a/b/c/../../../..",
	}
	for _, n := range bad {
		_, err := d.Module(n)
		s.ErrorIs(err, ErrModuleNameEscape, "name %q", n)
	}
}

func (s *SuiteDotGit) TestModuleAcceptsBenignNames() {
	d := New(s.EmptyFS())
	for _, n := range []string{"foo", "lib/foo", "x.y"} {
		_, err := d.Module(n)
		s.Require().NoError(err, "name %q", n)
	}
}

func (s *SuiteDotGit) TestReferenceNameRejectsEscapingNames() {
	d := New(s.EmptyFS())
	s.Require().NoError(d.Initialize())

	// A ".." component (or a control character) lets a reference name climb
	// out of its sub-tree into unrelated .git metadata such as config; a
	// malicious remote can advertise such a name and, after refspec mapping,
	// have it reach the storage layer.
	bad := []plumbing.ReferenceName{
		"refs/heads/../../config",
		"refs/remotes/origin/../../../config",
		"refs/heads/..",
		"refs/heads/foo\x00bar",
	}
	for _, n := range bad {
		ref := plumbing.NewHashReference(n, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
		s.ErrorIs(d.SetRef(ref, nil), ErrReferenceNameEscape, "SetRef %q", n)

		_, err := d.Ref(n)
		s.ErrorIs(err, ErrReferenceNameEscape, "Ref %q", n)

		s.ErrorIs(d.RemoveRef(n), ErrReferenceNameEscape, "RemoveRef %q", n)

		_, err = d.ReflogWriter(n)
		s.ErrorIs(err, ErrReferenceNameEscape, "ReflogWriter %q", n)
	}

	// The rejected traversals must not have written .git/config.
	_, err := d.fs.Stat(configPath)
	s.Error(err, "traversal must not create .git/config")
}

func (s *SuiteDotGit) TestReferenceNameRejectsWindowsDisguisedTraversal() {
	d := New(s.EmptyFS())
	s.Require().NoError(d.Initialize())

	// On NTFS, ".." followed by trailing periods/spaces or an Alternate Data
	// Stream / $INDEX_ALLOCATION suffix is canonicalised back to "..", so a
	// literal `== ".."` check would miss these while the filesystem still
	// performs the parent-directory hop. These are pure-string checks, so the
	// containment must hold on every host, not only Windows.
	bad := []plumbing.ReferenceName{
		"refs/heads/..::$INDEX_ALLOCATION",
		"refs/heads/..:$DATA",
		"refs/heads/.. /config",
		"refs/heads/.../config",
	}
	for _, n := range bad {
		ref := plumbing.NewHashReference(n, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
		s.ErrorIs(d.SetRef(ref, nil), ErrReferenceNameEscape, "SetRef %q", n)

		_, err := d.Ref(n)
		s.ErrorIs(err, ErrReferenceNameEscape, "Ref %q", n)
	}

	_, err := d.fs.Stat(configPath)
	s.Error(err, "traversal must not create .git/config")
}

func (s *SuiteDotGit) TestReferenceNameRejectsHFSDisguisedTraversal() {
	d := New(s.EmptyFS())
	s.Require().NoError(d.Initialize())

	// HFS+ strips a set of ignorable Unicode code points during
	// normalisation, so ".<U+200C>." (zero-width non-joiner) collapses to
	// ".." on disk. As above, this is a string-level check that must hold
	// regardless of host OS.
	n := plumbing.ReferenceName("refs/heads/." + "\u200c" + "./config")
	ref := plumbing.NewHashReference(n, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
	s.ErrorIs(d.SetRef(ref, nil), ErrReferenceNameEscape, "SetRef %q", n)

	_, err := d.Ref(n)
	s.ErrorIs(err, ErrReferenceNameEscape, "Ref %q", n)

	_, err = d.fs.Stat(configPath)
	s.Error(err, "traversal must not create .git/config")
}

func (s *SuiteDotGit) TestReferenceNameRejectsAbsoluteAndDriveNames() {
	d := New(s.EmptyFS())
	s.Require().NoError(d.Initialize())

	// A leading or trailing separator, or a drive-letter prefix, lets the
	// name collapse onto a top-level .git file (e.g. "/config" -> .git/config)
	// once joined. filepath.VolumeName alone would not catch "C:config" off
	// Windows, so these are rejected host-independently as validSubmoduleName
	// does.
	bad := []plumbing.ReferenceName{
		"/config",
		"\\config",
		"C:config",
		"refs/heads/foo/",
		"",
	}
	for _, n := range bad {
		ref := plumbing.NewHashReference(n, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
		s.ErrorIs(d.SetRef(ref, nil), ErrReferenceNameEscape, "SetRef %q", n)
	}

	_, err := d.fs.Stat(configPath)
	s.Error(err, "must not create .git/config")
}

func (s *SuiteDotGit) TestReferenceNameRejectsTopLevelMetadata() {
	d := New(s.EmptyFS())
	s.Require().NoError(d.Initialize())

	// A single-level name that is neither under refs/ nor a [A-Z_] pseudo-ref
	// would land on top-level .git metadata once joined; the IsSafe gate
	// rejects it, matching upstream refname_is_safe.
	bad := []plumbing.ReferenceName{
		"config", "config.worktree", "index", "packed-refs",
		"shallow", "hooks", "objects", "bar", "HEAD2", "head",
	}
	for _, n := range bad {
		ref := plumbing.NewHashReference(n, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
		s.ErrorIs(d.SetRef(ref, nil), ErrReferenceNameEscape, "SetRef %q", n)
	}

	_, err := d.fs.Stat(configPath)
	s.Error(err, "must not create .git/config")
}

func (s *SuiteDotGit) TestReferenceNameAcceptsBenignNames() {
	d := New(s.EmptyFS())
	s.Require().NoError(d.Initialize())
	for _, n := range []plumbing.ReferenceName{
		"HEAD", "ORIG_HEAD", "FETCH_HEAD", "MERGE_HEAD", "CHERRY_PICK_HEAD",
		"refs/heads/main", "refs/heads/release-1.2",
		"refs/tags/v1.0.0", "refs/remotes/origin/HEAD", "refs/stash",
	} {
		ref := plumbing.NewHashReference(n, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
		s.Require().NoError(d.SetRef(ref, nil), "SetRef %q", n)
	}
}

func (s *SuiteDotGit) TestInitialize() {
	fs := s.EmptyFS()

	dir := New(fs)

	err := dir.Initialize()
	s.Require().NoError(err)

	_, err = fs.Stat(fs.Join("objects", "info"))
	s.Require().NoError(err)

	_, err = fs.Stat(fs.Join("objects", "pack"))
	s.Require().NoError(err)

	_, err = fs.Stat(fs.Join("refs", "heads"))
	s.Require().NoError(err)

	_, err = fs.Stat(fs.Join("refs", "tags"))
	s.Require().NoError(err)
}

func (s *SuiteDotGit) TestSetRefs() {
	fs := s.EmptyFS()

	dir := New(fs)

	testSetRefs(s, dir)
}

func (s *SuiteDotGit) TestSetRefsNorwfs() {
	fs := s.EmptyFS()

	dir := New(&norwfs{fs})

	testSetRefs(s, dir)
}

func (s *SuiteDotGit) TestRefsHeadFirst() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)
	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.NotEqual(0, len(refs))
	s.Equal("HEAD", refs[0].Name().String())
}

func testSetRefs(s *SuiteDotGit, dir *DotGit) {
	firstFoo := plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	)
	err := dir.SetRef(firstFoo, nil)

	s.Require().NoError(err)

	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/symbolic",
		"ref: refs/heads/foo",
	), nil)

	s.Require().NoError(err)

	// A single-level, non-pseudo-ref name is refused: it is not under refs/
	// and its spelling is not the [A-Z_] pseudo-ref form (upstream
	// refname_is_safe rejects it likewise).
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"bar",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.ErrorIs(err, ErrReferenceNameEscape)

	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/feature/baz",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 3)

	ref := findReference(refs, "refs/heads/foo")
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())

	ref = findReference(refs, "refs/heads/symbolic")
	s.NotNil(ref)
	s.Equal("refs/heads/foo", ref.Target().String())

	ref = findReference(refs, "bar")
	s.Nil(ref)

	_, err = dir.readReferenceFile(".", "refs/heads/feature/baz")
	s.Require().NoError(err)

	_, err = dir.readReferenceFile(".", "refs/heads/feature")
	s.ErrorIs(err, ErrIsDir)

	ref, err = dir.Ref("refs/heads/foo")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())

	ref, err = dir.Ref("refs/heads/symbolic")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("refs/heads/foo", ref.Target().String())

	// "bar" was refused at write time and is refused at read time too.
	_, err = dir.Ref("bar")
	s.ErrorIs(err, ErrReferenceNameEscape)

	// Check that SetRef with a non-nil `old` works.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	), firstFoo)
	s.Require().NoError(err)

	// `firstFoo` is no longer the right `old` reference, so this
	// should fail.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	), firstFoo)
	s.NotNil(err)
}

func (s *SuiteDotGit) TestRefsFromPackedRefs() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	refs, err := dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, "refs/remotes/origin/branch")
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())
}

func (s *SuiteDotGit) TestRefsFromReferenceFile() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	refs, err := dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, "refs/remotes/origin/HEAD")
	s.NotNil(ref)
	s.Equal(plumbing.SymbolicReference, ref.Type())
	s.Equal("refs/remotes/origin/master", string(ref.Target()))
}

func BenchmarkRefMultipleTimes(b *testing.B) {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	if err != nil {
		b.Fatal(err)
	}
	refname := plumbing.ReferenceName("refs/remotes/origin/branch")

	dir := New(fs)
	_, err = dir.Ref(refname)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	for b.Loop() {
		_, err := dir.Ref(refname)
		if err != nil {
			b.Fatalf("unexpected error: %s", err)
		}
	}
}

func (s *SuiteDotGit) TestRemoveRefFromReferenceFile() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	name := plumbing.ReferenceName("refs/remotes/origin/HEAD")
	err = dir.RemoveRef(name)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, string(name))
	s.Nil(ref)
}

func (s *SuiteDotGit) TestRemoveRefFromPackedRefs() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	name := plumbing.ReferenceName("refs/remotes/origin/master")
	err = dir.RemoveRef(name)
	s.Require().NoError(err)

	b, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(""+
		"# pack-refs with: peeled fully-peeled \n"+
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"+
		"e8d3ffab552895c19b9fcf7aa264d277cde33881 refs/remotes/origin/branch\n",
		string(b))
}

func (s *SuiteDotGit) TestRemoveRefFromReferenceFileAndPackedRefs() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	// Make a ref file for a ref that's already in `packed-refs`.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/remotes/origin/branch",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	// Make sure it only appears once in the refs list.
	refs, err := dir.Refs()
	s.Require().NoError(err)
	found := false
	for _, ref := range refs {
		if ref.Name() == "refs/remotes/origin/branch" {
			s.False(found)
			found = true
		}
	}

	name := plumbing.ReferenceName("refs/remotes/origin/branch")
	err = dir.RemoveRef(name)
	s.Require().NoError(err)

	b, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(""+
		"# pack-refs with: peeled fully-peeled \n"+
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"+
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/remotes/origin/master\n",
		string(b))

	refs, err = dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, string(name))
	s.Nil(ref)
}

func (s *SuiteDotGit) TestRemoveRefNonExistent() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	before, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	name := plumbing.ReferenceName("refs/heads/nonexistent")
	err = dir.RemoveRef(name)
	s.Require().NoError(err)

	after, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(string(after), string(before))
}

func (s *SuiteDotGit) TestRemoveRefInvalidPackedRefs() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	brokenContent := "BROKEN STUFF REALLY BROKEN"

	err = util.WriteFile(fs, packedRefsPath, []byte(brokenContent), os.FileMode(0o755))
	s.Require().NoError(err)

	name := plumbing.ReferenceName("refs/heads/nonexistent")
	err = dir.RemoveRef(name)
	s.NotNil(err)

	after, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(string(after), brokenContent)
}

func (s *SuiteDotGit) TestRemoveRefInvalidPackedRefs2() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	brokenContent := strings.Repeat("a", bufio.MaxScanTokenSize*2)

	err = util.WriteFile(fs, packedRefsPath, []byte(brokenContent), os.FileMode(0o755))
	s.Require().NoError(err)

	name := plumbing.ReferenceName("refs/heads/nonexistent")
	err = dir.RemoveRef(name)
	s.NotNil(err)

	after, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(string(after), brokenContent)
}

func (s *SuiteDotGit) TestRefsFromHEADFile() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	refs, err := dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, "HEAD")
	s.NotNil(ref)
	s.Equal(plumbing.SymbolicReference, ref.Type())
	s.Equal("refs/heads/master", string(ref.Target()))
}

func (s *SuiteDotGit) TestConfig() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	file, err := dir.Config()
	s.Require().NoError(err)
	s.Equal("config", filepath.Base(file.Name()))
}

func (s *SuiteDotGit) TestConfigWriteAndConfig() {
	fs := s.EmptyFS()

	dir := New(fs)

	f, err := dir.ConfigWriter()
	s.Require().NoError(err)

	_, err = f.Write([]byte("foo"))
	s.Require().NoError(err)

	f, err = dir.Config()
	s.Require().NoError(err)

	cnt, err := io.ReadAll(f)
	s.Require().NoError(err)

	s.Equal("foo", string(cnt))
}

func (s *SuiteDotGit) TestIndex() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	idx, err := dir.Index()
	s.Require().NoError(err)
	s.NotNil(idx)
}

func (s *SuiteDotGit) TestIndexWriteAndIndex() {
	fs := s.EmptyFS()

	dir := New(fs)

	f, err := dir.IndexWriter()
	s.Require().NoError(err)

	_, err = f.Write([]byte("foo"))
	s.Require().NoError(err)

	f, err = dir.Index()
	s.Require().NoError(err)

	cnt, err := io.ReadAll(f)
	s.Require().NoError(err)

	s.Equal("foo", string(cnt))
}

func (s *SuiteDotGit) TestShallow() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	file, err := dir.Shallow()
	s.Require().NoError(err)
	s.Nil(file)
}

func (s *SuiteDotGit) TestShallowWriteAndShallow() {
	fs := s.EmptyFS()

	dir := New(fs)

	f, err := dir.ShallowWriter()
	s.Require().NoError(err)

	_, err = f.Write([]byte("foo"))
	s.Require().NoError(err)

	f, err = dir.Shallow()
	s.Require().NoError(err)

	cnt, err := io.ReadAll(f)
	s.Require().NoError(err)

	s.Equal("foo", string(cnt))
}

func findReference(refs []*plumbing.Reference, name string) *plumbing.Reference {
	n := plumbing.ReferenceName(name)
	for _, ref := range refs {
		if ref.Name() == n {
			return ref
		}
	}

	return nil
}

func (s *SuiteDotGit) TestObjectPacks() {
	f := fixtures.Basic().ByTag(".git").One()
	fs, err := f.DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	testObjectPacks(s, fs, dir, f)
}

func (s *SuiteDotGit) TestObjectPacksExclusive() {
	f := fixtures.Basic().ByTag(".git").One()
	fs, err := f.DotGit()
	s.Require().NoError(err)
	dir := NewWithOptions(fs, Options{ExclusiveAccess: true})

	testObjectPacks(s, fs, dir, f)
}

func testObjectPacks(s *SuiteDotGit, fs billy.Filesystem, dir *DotGit, f *fixtures.Fixture) {
	hashes, err := dir.ObjectPacks()
	s.Require().NoError(err)
	s.Len(hashes, 1)
	s.Equal(plumbing.NewHash(f.PackfileHash), hashes[0])

	// Make sure that a random file in the pack directory doesn't
	// break everything.
	badFile, err := fs.Create("objects/pack/OOPS_THIS_IS_NOT_RIGHT.pack")
	s.Require().NoError(err)
	err = badFile.Close()
	s.Require().NoError(err)
	// temporary file generated by git gc
	tmpFile, err := fs.Create("objects/pack/.tmp-11111-pack-58rf8y4wm1b1k52bpe0kdlx6lpreg6ahso8n3ylc.pack")
	s.Require().NoError(err)
	err = tmpFile.Close()
	s.Require().NoError(err)

	hashes2, err := dir.ObjectPacks()
	s.Require().NoError(err)
	s.Len(hashes2, 1)
	s.Equal(hashes2[0], hashes[0])
}

func (s *SuiteDotGit) TestObjectPack() {
	f := fixtures.Basic().ByTag(".git").One()
	fs, err := f.DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	pack, err := dir.ObjectPack(plumbing.NewHash(f.PackfileHash))
	s.Require().NoError(err)
	s.Equal(".pack", filepath.Ext(pack.Name()))
}

func (s *SuiteDotGit) TestObjectPackIdx() {
	f := fixtures.Basic().ByTag(".git").One()
	fs, err := f.DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	idx, err := dir.ObjectPackIdx(plumbing.NewHash(f.PackfileHash))
	s.Require().NoError(err)
	s.Equal(".idx", filepath.Ext(idx.Name()))
	s.Nil(idx.Close())
}

func TestOpenPackRevReadFromDisk(t *testing.T) {
	t.Parallel()

	dot, h, _ := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})

	rev, err := dot.OpenPackRev(h)
	require.NoError(t, err)

	var hdr [4]byte
	_, err = rev.ReadAt(hdr[:], 0)
	require.NoError(t, err)
	assert.Equal(t, [4]byte{'R', 'I', 'D', 'X'}, hdr)

	require.NoError(t, rev.Close())
}

func TestOpenPackRevIgnoreReverseIndex(t *testing.T) {
	t.Parallel()

	dot, h, fs := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})

	// Overwrite the .rev with garbage.
	revPath := fmt.Sprintf("objects/pack/pack-%s.rev", h)
	corruptRevFile(t, fs, revPath, []byte("corrupt rev data"))

	rev, err := dot.OpenPackRev(h)
	require.NoError(t, err)

	var hdr [4]byte
	_, err = rev.ReadAt(hdr[:], 0)
	require.NoError(t, err)
	assert.NotEqual(t, [4]byte{'R', 'I', 'D', 'X'}, hdr)
	require.NoError(t, rev.Close())

	// Re-open DotGit with ReadReverseIndex=false so the corrupt disk
	// file is ignored and a fresh rev is generated instead.
	dot = NewWithOptions(fs, Options{
		ReadReverseIndex:  false,
		WriteReverseIndex: true,
	})

	rev, err = dot.OpenPackRev(h)
	require.NoError(t, err)

	_, err = rev.ReadAt(hdr[:], 0)
	require.NoError(t, err)
	assert.Equal(t, [4]byte{'R', 'I', 'D', 'X'}, hdr, "generated rev should have valid RIDX header")
	require.NoError(t, rev.Close())
}

func TestOpenPackRevNoRevFileOnDisk(t *testing.T) {
	t.Parallel()

	_, h, fs := createPackWithRev(t, Options{
		ReadReverseIndex:  false,
		WriteReverseIndex: false,
	})

	revPath := fmt.Sprintf("objects/pack/pack-%s.rev", h)
	_, err := fs.Stat(revPath)
	require.True(t, os.IsNotExist(err), ".rev file should not exist on disk")

	dot := NewWithOptions(fs, Options{ReadReverseIndex: false})
	rev, err := dot.OpenPackRev(h)
	require.NoError(t, err)

	var hdr [4]byte
	_, err = rev.ReadAt(hdr[:], 0)
	require.NoError(t, err)
	assert.Equal(t, [4]byte{'R', 'I', 'D', 'X'}, hdr)
	require.NoError(t, rev.Close())
}

func TestOpenRevFileDoesNotExist(t *testing.T) {
	t.Parallel()

	_, h, fs := createPackWithRev(t, Options{
		ReadReverseIndex:  false,
		WriteReverseIndex: false,
	})

	// ReadReverseIndex=true tries to open the .rev from disk — which
	// does not exist. Generate one on demand.
	dot := NewWithOptions(fs, Options{ReadReverseIndex: true})
	_, err := dot.OpenPackRev(h)
	require.NoError(t, err)
}

func TestOpenPackRevGeneratedMatchesDisk(t *testing.T) {
	t.Parallel()

	dot, h, fs := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})

	diskRev, err := dot.OpenPackRev(h)
	require.NoError(t, err)

	diskData, err := io.ReadAll(diskRev)
	require.NoError(t, err)
	require.NoError(t, diskRev.Close())

	genDot := NewWithOptions(fs, Options{ReadReverseIndex: false})
	genRev, err := genDot.OpenPackRev(h)
	require.NoError(t, err)

	genData, err := io.ReadAll(genRev)
	require.NoError(t, err)
	require.NoError(t, genRev.Close())

	assert.Equal(t, diskData, genData, "generated rev should be byte-identical to on-disk rev")
}

func TestOpenPackRevUsableByLazyIndex(t *testing.T) {
	t.Parallel()

	dot, h, _ := createPackWithRev(t, Options{
		ReadReverseIndex:  false,
		WriteReverseIndex: false,
	})

	openIdx := func() (idxfile.ReadAtCloser, error) {
		return dot.ObjectPackIdx(h)
	}
	openRev := func() (idxfile.ReadAtCloser, error) {
		return dot.OpenPackRev(h)
	}

	idx, err := idxfile.NewLazyIndex(openIdx, openRev, h)
	require.NoError(t, err)
	defer idx.Close()

	// Verify we can look up a known object by offset → hash (uses the rev).
	// First get an entry via the forward index to obtain a known offset.
	entries, err := idx.Entries()
	require.NoError(t, err)

	entry, err := entries.Next()
	require.NoError(t, err)
	require.NoError(t, entries.Close())

	// Use FindHash (reverse lookup) which relies on the rev data.
	got, err := idx.FindHash(int64(entry.Offset))
	require.NoError(t, err)
	assert.Equal(t, entry.Hash, got, "FindHash via generated rev should return the correct hash")
}

// createPackWithRev creates a packfile from the basic fixture and returns
// the DotGit, the pack hash, and the filesystem. The DotGit is created
// with the given options.
func createPackWithRev(t *testing.T, opts Options) (*DotGit, plumbing.Hash, billy.Filesystem) {
	t.Helper()

	f := fixtures.Basic().One()
	fs := osfs.New(t.TempDir())
	dot := NewWithOptions(fs, opts)
	require.NoError(t, dot.Initialize())

	w, err := dot.NewObjectPack()
	require.NoError(t, err)

	pf, pfErr := f.Packfile()
	require.NoError(t, pfErr)
	_, err = io.Copy(w, pf)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	h := plumbing.NewHash(f.PackfileHash)
	return dot, h, fs
}

// corruptRevFile overwrites the file at path with the given data,
// handling the read-only permissions set by fixPermissions.
func corruptRevFile(t *testing.T, fs billy.Filesystem, path string, data []byte) {
	t.Helper()
	if chmodFS, ok := fs.(billy.Chmod); ok {
		require.NoError(t, chmodFS.Chmod(path, 0o644))
	}
	f, err := fs.Create(path)
	require.NoError(t, err)
	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func (s *SuiteDotGit) TestObjectPackNotFound() {
	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	pack, err := dir.ObjectPack(plumbing.ZeroHash)
	s.ErrorIs(err, ErrPackfileNotFound)
	s.Nil(pack)

	idx, err := dir.ObjectPackIdx(plumbing.ZeroHash)
	s.ErrorIs(err, ErrPackfileNotFound)
	s.Nil(idx)
}

func (s *SuiteDotGit) TestNewObject() {
	fs := s.EmptyFS()

	dir := New(fs)
	w, err := dir.NewObject()
	s.Require().NoError(err)

	err = w.WriteHeader(plumbing.BlobObject, 14)
	s.Require().NoError(err)
	n, err := w.Write([]byte("this is a test"))
	s.Require().NoError(err)
	s.Equal(14, n)

	s.Equal("a8a940627d132695a9769df883f85992f0ff4a43", w.Hash().String())

	err = w.Close()
	s.Require().NoError(err)

	i, err := fs.Stat("objects/a8/a940627d132695a9769df883f85992f0ff4a43")
	s.Require().NoError(err)
	s.Equal(int64(34), i.Size())
}

func (s *SuiteDotGit) TestObjects() {
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	testObjects(s, fs, dir)
	testObjectsWithPrefix(s, fs, dir)
}

func (s *SuiteDotGit) TestObjectsExclusive() {
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	dir := NewWithOptions(fs, Options{ExclusiveAccess: true})

	testObjects(s, fs, dir)
	testObjectsWithPrefix(s, fs, dir)
}

func testObjects(s *SuiteDotGit, _ billy.Filesystem, dir *DotGit) {
	hashes, err := dir.Objects()
	s.Require().NoError(err)
	s.Len(hashes, 187)
	s.Equal("0097821d427a3c3385898eb13b50dcbc8702b8a3", hashes[0].String())
	s.Equal("01d5fa556c33743006de7e76e67a2dfcd994ca04", hashes[1].String())
	s.Equal("03db8e1fbe133a480f2867aac478fd866686d69e", hashes[2].String())
}

func testObjectsWithPrefix(s *SuiteDotGit, _ billy.Filesystem, dir *DotGit) {
	prefix, _ := hex.DecodeString("01d5")
	hashes, err := dir.ObjectsWithPrefix(prefix)
	s.Require().NoError(err)
	s.Len(hashes, 1)
	s.Equal("01d5fa556c33743006de7e76e67a2dfcd994ca04", hashes[0].String())

	// Empty prefix should yield all objects.
	// (subset of testObjects)
	hashes, err = dir.ObjectsWithPrefix(nil)
	s.Require().NoError(err)
	s.Len(hashes, 187)
}

func (s *SuiteDotGit) TestObjectsNoFolder() {
	fs := s.EmptyFS()

	dir := New(fs)
	hash, err := dir.Objects()
	s.Require().NoError(err)
	s.Len(hash, 0)
}

func (s *SuiteDotGit) testObjectWithIncomingDir(incomingDirName string) {
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	file, err := dir.Object(hash)
	s.Require().NoError(err)
	s.True(strings.HasSuffix(
		file.Name(), fs.Join("objects", "03", "db8e1fbe133a480f2867aac478fd866686d69e")),
	)
	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", incomingDirName)
	incomingFilePath := fs.Join(incomingDirPath, incomingHash[0:2], incomingHash[2:40])
	fs.MkdirAll(incomingDirPath, os.FileMode(0o755))
	fs.Create(incomingFilePath)

	_, err = dir.Object(plumbing.NewHash(incomingHash))
	s.Require().NoError(err)
}

func (s *SuiteDotGit) TestObject() {
	s.testObjectWithIncomingDir("tmp_objdir-incoming-123456")
}

func (s *SuiteDotGit) TestPreGit235Object() {
	s.testObjectWithIncomingDir("incoming-123456")
}

func (s *SuiteDotGit) TestObjectStat() {
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	_, err = dir.ObjectStat(hash)
	s.Require().NoError(err)
	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "tmp_objdir-incoming-123456")
	incomingFilePath := fs.Join(incomingDirPath, incomingHash[0:2], incomingHash[2:40])
	fs.MkdirAll(incomingDirPath, os.FileMode(0o755))
	fs.Create(incomingFilePath)

	_, err = dir.ObjectStat(plumbing.NewHash(incomingHash))
	s.Require().NoError(err)
}

func (s *SuiteDotGit) TestObjectDelete() {
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	err = dir.ObjectDelete(hash)
	s.Require().NoError(err)

	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "tmp_objdir-incoming-123456")
	incomingSubDirPath := fs.Join(incomingDirPath, incomingHash[0:2])
	incomingFilePath := fs.Join(incomingSubDirPath, incomingHash[2:40])

	err = fs.MkdirAll(incomingSubDirPath, os.FileMode(0o755))
	s.Require().NoError(err)

	f, err := fs.Create(incomingFilePath)
	s.Require().NoError(err)

	err = f.Close()
	s.Require().NoError(err)

	err = dir.ObjectDelete(plumbing.NewHash(incomingHash))
	s.Require().NoError(err)
}

func (s *SuiteDotGit) TestObjectNotFound() {
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	hash := plumbing.NewHash("not-found-object")
	file, err := dir.Object(hash)
	s.NotNil(err)
	s.Nil(file)
}

func (s *SuiteDotGit) TestSubmodules() {
	fs, err := fixtures.ByTag("submodule").One().DotGit()
	s.Require().NoError(err)
	dir := New(fs)

	m, err := dir.Module("basic")
	s.Require().NoError(err)
	s.True(strings.HasSuffix(m.Root(), m.Join(".git", "modules", "basic")))
}

func (s *SuiteDotGit) TestPackRefs() {
	fs := s.EmptyFS()

	dir := New(fs)

	err := dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/bar",
		"a8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 2)
	looseCount, err := dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(2, looseCount)

	err = dir.PackRefs()
	s.Require().NoError(err)

	// Make sure the refs are still there, but no longer loose.
	refs, err = dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 2)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(0, looseCount)

	ref, err := dir.Ref("refs/heads/foo")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())
	ref, err = dir.Ref("refs/heads/bar")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("a8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())

	// Now update one of them, re-pack, and check again.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"b8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(1, looseCount)
	err = dir.PackRefs()
	s.Require().NoError(err)

	// Make sure the refs are still there, but no longer loose.
	refs, err = dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 2)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(0, looseCount)

	ref, err = dir.Ref("refs/heads/foo")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("b8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())
}

func TestAlternatesDefault(t *testing.T) {
	t.Parallel()
	// Create a new dotgit object.
	dotFS := osfs.New(t.TempDir())

	testAlternates(t, dotFS, dotFS)
}

func TestAlternatesWithFS(t *testing.T) {
	t.Parallel()
	// Create a new dotgit object with a specific FS for alternates.
	altFS := osfs.New(t.TempDir())
	dotFS, _ := altFS.Chroot("repo2")

	testAlternates(t, dotFS, altFS)
}

func TestAlternatesWithBoundOS(t *testing.T) {
	t.Parallel()
	// Create a new dotgit object with a specific FS for alternates.
	altFS := osfs.New(t.TempDir(), osfs.WithBoundOS())
	dotFS, _ := altFS.Chroot("repo2")

	testAlternates(t, dotFS, altFS)
}

func testAlternates(t *testing.T, dotFS, altFS billy.Filesystem) {
	tests := []struct {
		name      string
		in        []string
		inWindows []string
		setup     func()
		wantErr   bool
		wantRoots []string
	}{
		{
			name: "no alternates",
		},
		{
			name:      "abs path",
			in:        []string{filepath.Join(altFS.Root(), "./repo1/.git/objects")},
			inWindows: []string{filepath.Join(altFS.Root(), ".\\repo1\\.git\\objects")},
			setup: func() {
				err := altFS.MkdirAll(filepath.Join("repo1", ".git", "objects"), 0o700)
				assert.NoError(t, err)
			},
			wantRoots: []string{filepath.Join("repo1", ".git")},
		},
		{
			name:      "rel path",
			in:        []string{"../../../repo3//.git/objects"},
			inWindows: []string{"..\\..\\..\\repo3\\.git\\objects"},
			setup: func() {
				err := altFS.MkdirAll(filepath.Join("repo3", ".git", "objects"), 0o700)
				assert.NoError(t, err)
			},
			wantRoots: []string{filepath.Join("repo3", ".git")},
		},
		{
			name:      "invalid abs path",
			in:        []string{"/alt/target2"},
			inWindows: []string{"\\alt\\target2"},
			wantErr:   true,
		},
		{
			name:      "invalid rel path",
			in:        []string{"../../../alt/target3"},
			inWindows: []string{"..\\..\\..\\alt\\target3"},
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := NewWithOptions(dotFS, Options{AlternatesFS: altFS})
			err := dir.Initialize()
			assert.NoError(t, err)

			content := strings.Join(tc.in, "\n")
			if runtime.GOOS == "windows" {
				content = strings.Join(tc.inWindows, "\r\n")
			}

			// Create alternates file.
			altpath := dotFS.Join("objects", "info", "alternates")
			f, err := dotFS.Create(altpath)
			assert.NoError(t, err)
			f.Write([]byte(content))
			f.Close()

			if tc.setup != nil {
				tc.setup()
			}

			dotgits, err := dir.Alternates()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			for i, d := range dotgits {
				assert.Regexp(t, "^"+regexp.QuoteMeta(altFS.Root()), d.fs.Root())
				assert.Regexp(t, regexp.QuoteMeta(tc.wantRoots[i])+"$", d.fs.Root())
			}
		})
	}
}

func TestAlternatesDupes(t *testing.T) {
	t.Parallel()
	dotFS := osfs.New(t.TempDir())
	dir := New(dotFS)
	err := dir.Initialize()
	assert.NoError(t, err)

	path := filepath.Join(dotFS.Root(), "target3")
	dupes := []string{path, path, path, path, path}

	content := strings.Join(dupes, "\n")
	if runtime.GOOS == "windows" {
		content = strings.Join(dupes, "\r\n")
	}

	err = dotFS.MkdirAll("target3", 0o700)
	assert.NoError(t, err)

	// Create alternates file.
	altpath := dotFS.Join("objects", "info", "alternates")
	f, err := dotFS.Create(altpath)
	assert.NoError(t, err)
	f.Write([]byte(content))
	f.Close()

	dotgits, err := dir.Alternates()
	assert.NoError(t, err)
	assert.Len(t, dotgits, 1)
}

func TestAlternatesAbsolutePathOutsideAlternatesFSRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	altRoot := filepath.Join(base, "alt-root")
	outsideObjects := filepath.Join(base, "outside", ".git", "objects")

	require.NoError(t, os.MkdirAll(altRoot, 0o700))
	require.NoError(t, os.MkdirAll(outsideObjects, 0o700))

	altFS := osfs.New(altRoot)
	dotFS, err := altFS.Chroot("repo")
	require.NoError(t, err)

	dir := NewWithOptions(dotFS, Options{AlternatesFS: altFS})
	require.NoError(t, dir.Initialize())

	require.NoError(t, altFS.MkdirAll(filepath.Join("outside", ".git", "objects"), 0o700))

	altpath := dotFS.Join("objects", "info", "alternates")
	f, err := dotFS.Create(altpath)
	require.NoError(t, err)
	_, err = f.Write([]byte(outsideObjects + "\n"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	dotgits, err := dir.Alternates()
	require.Error(t, err)
	assert.Nil(t, dotgits)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestAlternatesDefaultAbsolutePathOutsideDotGitRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	dotRoot := filepath.Join(base, "repo", ".git")
	alternateRoot := filepath.Join(base, "alternate", ".git")
	alternateObjects := filepath.Join(alternateRoot, "objects")

	require.NoError(t, os.MkdirAll(dotRoot, 0o700))
	require.NoError(t, os.MkdirAll(alternateObjects, 0o700))

	dotFS := osfs.New(dotRoot)
	dir := NewWithOptions(dotFS, Options{AlternatesFS: osfs.New(alternateRoot)})
	require.NoError(t, dir.Initialize())

	altpath := dotFS.Join("objects", "info", "alternates")
	f, err := dotFS.Create(altpath)
	require.NoError(t, err)
	_, err = f.Write([]byte(alternateObjects + "\n"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	dotgits, err := dir.Alternates()
	require.NoError(t, err)
	require.Len(t, dotgits, 1)
	assert.Equal(t, alternateRoot, dotgits[0].fs.Root())
}

type norwfs struct {
	billy.Filesystem
}

func (f *norwfs) Capabilities() billy.Capability {
	return billy.Capabilities(f.Filesystem) &^ billy.ReadAndWriteCapability
}

func (s *SuiteDotGit) TestIncBytes() {
	tests := []struct {
		in       []byte
		out      []byte
		overflow bool
	}{
		{[]byte{0}, []byte{1}, false},
		{[]byte{0xff}, []byte{0}, true},
		{[]byte{7, 0xff}, []byte{8, 0}, false},
		{[]byte{0xff, 0xff}, []byte{0, 0}, true},
	}
	for _, test := range tests {
		out, overflow := incBytes(test.in)
		s.Equal(test.out, out)
		s.Equal(test.overflow, overflow)
	}
}

// this filesystem wrapper returns os.ErrNotExist if the file matches
// the provided paths list
type notExistsFS struct {
	billy.Filesystem

	paths []string
}

func (f *notExistsFS) matches(path string) bool {
	p := filepath.ToSlash(path)
	return slices.Contains(f.paths, p)
}

func (f *notExistsFS) Open(filename string) (billy.File, error) {
	if f.matches(filename) {
		return nil, os.ErrNotExist
	}

	return f.Filesystem.Open(filename)
}

func (f *notExistsFS) ReadDir(path string) ([]fs.DirEntry, error) {
	if f.matches(path) {
		return nil, os.ErrNotExist
	}

	return f.Filesystem.ReadDir(path)
}

func (s *SuiteDotGit) TestDeletedRefs() {
	fs := s.EmptyFS()

	dir := New(&notExistsFS{
		Filesystem: fs,
		paths: []string{
			"refs/heads/bar",
			"refs/heads/baz",
		},
	})

	err := dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/bar",
		"a8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/baz/baz",
		"a8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 1)
	s.Equal(plumbing.ReferenceName("refs/heads/foo"), refs[0].Name())
}

// Checks that setting a reference that has been packed and checking its old value is successful
func (s *SuiteDotGit) TestSetPackedRef() {
	fs := s.EmptyFS()

	dir := New(fs)

	err := dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 1)
	looseCount, err := dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(1, looseCount)

	err = dir.PackRefs()
	s.Require().NoError(err)

	// Make sure the refs are still there, but no longer loose.
	refs, err = dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 1)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(0, looseCount)

	ref, err := dir.Ref("refs/heads/foo")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())

	// Attempt to update the reference using an invalid old reference value
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"b8d3ffab552895c19b9fcf7aa264d277cde33881",
	), plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33882",
	))
	s.ErrorIs(err, storage.ErrReferenceHasChanged)

	// Now update the reference and it should pass
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"b8d3ffab552895c19b9fcf7aa264d277cde33881",
	), plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	))
	s.Require().NoError(err)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(1, looseCount)
}

func TestIssue55(t *testing.T) {
	t.Parallel()

	writeObject := func(fs billy.Filesystem) {
		t.Helper()

		dir := New(fs)
		err := dir.Initialize()
		require.NoError(t, err)

		w, err := dir.NewObject()
		require.NoError(t, err)

		err = w.WriteHeader(plumbing.BlobObject, 14)
		require.NoError(t, err)
		n, err := w.Write([]byte("this is a test"))
		require.NoError(t, err)
		assert.Equal(t, 14, n)

		assert.Equal(t, "a8a940627d132695a9769df883f85992f0ff4a43", w.Hash().String())

		err = w.Close()
		require.NoError(t, err)
	}

	for _, tc := range []struct {
		name string
		fs   billy.Filesystem
	}{
		{"BoundOS", osfs.New(t.TempDir(), osfs.WithBoundOS())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("objects", "a8", "a940627d132695a9769df883f85992f0ff4a43")

			writeObject(tc.fs)
			i, err := tc.fs.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, int64(34), i.Size())

			ro, err := isReadOnly(tc.fs, path)
			require.NoError(t, err)
			assert.True(t, ro, "file %q is not read-only", path)

			// Recreate the same object.
			writeObject(tc.fs)
			i, err = tc.fs.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, int64(34), i.Size())

			ro, err = isReadOnly(tc.fs, path)
			require.NoError(t, err)
			assert.True(t, ro, "file %q is not read-only", path)
		})
	}
}

// TestDotGit_HasIncomingObjects_NoRace exercises the lazy
// initialisation in hasIncomingObjects under concurrent callers.
// Before the sync.Once fix, two goroutines racing through Object()
// would write the cache fields without synchronisation; the race
// detector flagged this reliably under -race -count=10.
func TestDotGit_HasIncomingObjects_NoRace(t *testing.T) {
	t.Parallel()
	fs := osfs.New(t.TempDir())
	require.NoError(t, fs.MkdirAll("objects", 0o755))
	d := New(fs)

	hash := plumbing.NewHash("0000000000000000000000000000000000000000")
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			_, _ = d.Object(hash)
		})
	}
	wg.Wait()
}

// fakePackHandleSource returns a Source whose Open and Size always
// fail. walkPackHandles never calls these (it only invokes fn on
// each cached *PackHandle), so the fakes satisfy New without
// requiring on-disk pack files.
func fakePackHandleSource() packhandle.Source {
	return packhandle.Source{
		Open: func() (packhandle.ReadAtCloser, error) {
			return nil, errors.New("fake source: not used")
		},
		Size: func() (int64, error) {
			return 0, errors.New("fake source: not used")
		},
	}
}

// newFakePackHandle returns a new *packhandle.PackHandle whose
// sources all fail when opened. Suitable for tests that only
// exercise catalog-walk behaviour.
func newFakePackHandle(t *testing.T, i int) *packhandle.PackHandle {
	t.Helper()
	var h plumbing.Hash
	h.ResetBySize(20) // SHA-1 size
	_, _ = h.Write(bytes.Repeat([]byte{byte(i + 1)}, 20))
	src := fakePackHandleSource()
	ph, err := packhandle.New(packhandle.Sources{Pack: src, Idx: src, Rev: src}, h)
	require.NoError(t, err)
	return ph
}

// seedPackHandles installs n fake PackHandles into d.packHandles
// under the catalog lock, returning the hashes used as keys.
func seedPackHandles(t *testing.T, d *DotGit, n int) []plumbing.Hash {
	t.Helper()
	d.packHandlesMu.Lock()
	defer d.packHandlesMu.Unlock()
	if d.packHandles == nil {
		d.packHandles = make(map[plumbing.Hash]*packhandle.PackHandle)
	}
	hashes := make([]plumbing.Hash, n)
	for i := range n {
		var h plumbing.Hash
		h.ResetBySize(20) // SHA-1 size
		_, _ = h.Write(bytes.Repeat([]byte{byte(i + 1)}, 20))
		hashes[i] = h
		d.packHandles[h] = newFakePackHandle(t, i)
	}
	return hashes
}

// TestWalkPackHandles_SnapshotsCatalog verifies that walkPackHandles
// iterates a snapshot taken under the lock, so a concurrent mutation
// of the catalog (e.g. eviction) does not panic or skip entries the
// snapshot already captured.
func TestWalkPackHandles_SnapshotsCatalog(t *testing.T) {
	t.Parallel()
	fs, err := fixtures.Basic().One().DotGit()
	require.NoError(t, err)
	d := New(fs)
	t.Cleanup(func() { _ = d.Close() })

	const seedN = 3
	hashes := seedPackHandles(t, d, seedN)

	d.packHandlesMu.Lock()
	expectedCount := len(d.packHandles)
	d.packHandlesMu.Unlock()
	require.GreaterOrEqual(t, expectedCount, seedN,
		"catalog must hold at least the seeded entries")

	var seen atomic.Int32
	var evictDone atomic.Bool
	victim := hashes[0]
	err = d.walkPackHandles(func(_ *packhandle.PackHandle) error {
		seen.Add(1)
		if evictDone.CompareAndSwap(false, true) {
			d.packHandlesMu.Lock()
			delete(d.packHandles, victim)
			d.packHandlesMu.Unlock()
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(expectedCount), seen.Load(),
		"walk should visit every snapshot entry, even after mid-walk catalog mutation")
}

// TestDotGitCloseIdleDescriptors groups the DotGit-layer cases for
// the catalog walk that releases FDs without evicting the
// PackHandle entries.
func TestDotGitCloseIdleDescriptors(t *testing.T) {
	t.Parallel()

	t.Run("WalksCatalog", func(t *testing.T) {
		t.Parallel()
		fs, err := fixtures.Basic().One().DotGit()
		require.NoError(t, err)
		d := New(fs)
		t.Cleanup(func() { _ = d.Close() })

		packs, err := d.ObjectPacks()
		require.NoError(t, err)
		require.NotEmpty(t, packs)

		// Populate catalog and capture each PackHandle pointer.
		before := make(map[plumbing.Hash]*packhandle.PackHandle, len(packs))
		for _, h := range packs {
			ph, err := d.packHandle(h)
			require.NoError(t, err)
			before[h] = ph
			// Touch the pack so an FD is actually opened.
			pr, err := ph.OpenPackReader()
			require.NoError(t, err)
			require.NoError(t, pr.Close())
		}

		require.NoError(t, d.CloseIdleDescriptors())

		// Catalog map is intact and PackHandle pointers are the same.
		for _, h := range packs {
			ph, err := d.packHandle(h)
			require.NoError(t, err)
			assert.Same(t, before[h], ph,
				"PackHandle pointer should survive CloseIdleDescriptors")
		}
	})

	t.Run("EmptyCatalog", func(t *testing.T) {
		t.Parallel()
		fs, err := fixtures.Basic().One().DotGit()
		require.NoError(t, err)
		d := New(fs)
		t.Cleanup(func() { _ = d.Close() })
		// No packHandle() calls; catalog stays nil.
		require.NoError(t, d.CloseIdleDescriptors())
	})

	t.Run("AfterClose_NoOp", func(t *testing.T) {
		t.Parallel()
		fs, err := fixtures.Basic().One().DotGit()
		require.NoError(t, err)
		d := New(fs)
		require.NoError(t, d.Close())
		require.NoError(t, d.CloseIdleDescriptors())
	})
}

// TestWalkPackHandles_JoinsErrors verifies that fn errors are
// joined and returned, but do not stop the walk.
func TestWalkPackHandles_JoinsErrors(t *testing.T) {
	t.Parallel()
	fs, err := fixtures.Basic().One().DotGit()
	require.NoError(t, err)
	d := New(fs)
	t.Cleanup(func() { _ = d.Close() })

	const seedN = 3
	_ = seedPackHandles(t, d, seedN)

	d.packHandlesMu.Lock()
	totalEntries := len(d.packHandles)
	d.packHandlesMu.Unlock()
	require.GreaterOrEqual(t, totalEntries, seedN)

	sentinel := errors.New("walk-fn-error")
	var visits atomic.Int32
	err = d.walkPackHandles(func(_ *packhandle.PackHandle) error {
		visits.Add(1)
		return sentinel
	})
	require.Error(t, err)
	// Every visited entry returns sentinel; errors.Is unwraps the
	// joined chain and finds it.
	assert.ErrorIs(t, err, sentinel)
	// Crucially: the walk did not stop on the first error.
	assert.Equal(t, int32(totalEntries), visits.Load(),
		"walk should not stop on first error")
}
