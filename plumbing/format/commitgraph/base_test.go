package commitgraph

import (
	"bytes"
	"crypto"
	"encoding/binary"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	gogithash "github.com/go-git/go-git/v6/plumbing/hash"
)

func fixtureLayers(t *testing.T, of formatcfg.ObjectFormat) ([]string, [][]byte) {
	t.Helper()
	_, fs := fixtureGraph(t, "commit-graph-chain-"+of.String())
	dir := "objects/info/commit-graphs/"
	manifest, err := util.ReadFile(fs, dir+"commit-graph-chain")
	require.NoError(t, err)
	names := strings.Fields(string(manifest))
	layers := make([][]byte, 0, len(names))
	for _, name := range names {
		data, err := util.ReadFile(fs, dir+"graph-"+name+".graph")
		require.NoError(t, err)
		layers = append(layers, data)
	}
	return names, layers
}

func openFixtureParents(t *testing.T, of formatcfg.ObjectFormat, layers [][]byte) Index {
	t.Helper()
	var idx Index
	for _, raw := range layers {
		next, err := OpenFileIndexWithParent(graphTestReader{bytes.NewReader(raw)}, idx, WithObjectFormat(of))
		require.NoError(t, err)
		idx = next
	}
	return idx
}

func fixtureChecksum(raw []byte, of formatcfg.ObjectFormat) []byte {
	algorithm := crypto.SHA1
	if of == formatcfg.SHA256 {
		algorithm = crypto.SHA256
	}
	h := gogithash.New(algorithm)
	_, _ = h.Write(raw[:len(raw)-of.Size()])
	copy(raw[len(raw)-of.Size():], h.Sum(nil))
	return raw
}

func replaceFixtureBase(t *testing.T, raw, replacement []byte, of formatcfg.ObjectFormat) []byte {
	t.Helper()
	for i := 0; i < int(raw[6]); i++ {
		entry := 8 + i*12
		if string(raw[entry:entry+4]) != "BASE" {
			continue
		}
		start := int(binary.BigEndian.Uint64(raw[entry+4 : entry+12]))
		end := int(binary.BigEndian.Uint64(raw[entry+16 : entry+24]))
		out := append(bytes.Clone(raw[:start]), replacement...)
		out = append(out, raw[end:]...)
		delta := len(replacement) - (end - start)
		for j := i + 1; j <= int(raw[6]); j++ {
			position := 8 + j*12 + 4
			old := binary.BigEndian.Uint64(raw[position : position+8])
			binary.BigEndian.PutUint64(out[position:position+8], uint64(int(old)+delta))
		}
		return fixtureChecksum(out, of)
	}
	t.Fatal("fixture lacks BASE chunk")
	return nil
}

func TestHeaderBaseCountDoesNotControlLoading(t *testing.T) {
	t.Parallel()
	for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(of.String(), func(t *testing.T) {
			t.Parallel()
			_, layers := fixtureLayers(t, of)
			standalone := bytes.Clone(layers[0])
			standalone[7] = 255
			idx, err := OpenFileIndex(graphTestReader{bytes.NewReader(fixtureChecksum(standalone, of))}, WithObjectFormat(of))
			require.NoError(t, err)
			require.NoError(t, idx.Close())
			parent := openFixtureParents(t, of, layers[:2])
			defer parent.Close()
			top := bytes.Clone(layers[2])
			top[7] = 0
			idx, err = OpenFileIndexWithParent(graphTestReader{bytes.NewReader(fixtureChecksum(top, of))}, parent, WithObjectFormat(of))
			require.NoError(t, err)
			require.Equal(t, uint32(3), idx.MaximumNumberOfHashes())
		})
	}
}

func TestRejectsIncorrectBaseIdentities(t *testing.T) {
	t.Parallel()
	for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(of.String(), func(t *testing.T) {
			t.Parallel()
			names, layers := fixtureLayers(t, of)
			var base []byte
			for _, name := range names[:2] {
				base = append(base, plumbing.NewHash(name).Bytes()...)
			}
			for _, mutation := range []string{"wrong", "reordered", "truncated", "missing", "surplus"} {
				t.Run(mutation, func(t *testing.T) {
					t.Parallel()
					parent := openFixtureParents(t, of, layers[:2])
					defer parent.Close()
					data := bytes.Clone(base)
					switch mutation {
					case "wrong":
						data[0] ^= 1
					case "reordered":
						data = append(bytes.Clone(base[of.Size():]), base[:of.Size()]...)
					case "truncated":
						data = data[:len(data)-1]
					case "missing":
						data = nil
					case "surplus":
						data = append(data, base[:of.Size()]...)
					}
					raw := replaceFixtureBase(t, layers[2], data, of)
					idx, err := OpenFileIndexWithParent(graphTestReader{bytes.NewReader(raw)}, parent, WithObjectFormat(of))
					if mutation == "surplus" {
						require.NoError(t, err)
						require.Equal(t, uint32(3), idx.MaximumNumberOfHashes())
					} else {
						require.ErrorIs(t, err, ErrMalformedCommitGraphFile)
					}
				})
			}
		})
	}
}

func TestRejectsMixedParentHashVersions(t *testing.T) {
	t.Parallel()
	for _, childFormat := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(childFormat.String(), func(t *testing.T) {
			t.Parallel()
			parentFormat := formatcfg.SHA1
			if childFormat == parentFormat {
				parentFormat = formatcfg.SHA256
			}
			_, parentLayers := fixtureLayers(t, parentFormat)
			parent := openFixtureParents(t, parentFormat, parentLayers[:1])
			defer parent.Close()
			_, childLayers := fixtureLayers(t, childFormat)
			_, err := OpenFileIndexWithParent(graphTestReader{bytes.NewReader(childLayers[1])}, parent, WithObjectFormat(childFormat))
			require.ErrorIs(t, err, ErrObjectFormatMismatch)
		})
	}
}

type opaqueGraphIndex struct{ Index }

type unsizedGraphReader struct{ io.ReaderAt }

func (unsizedGraphReader) Close() error { return nil }

// claimedSizeReader reports size while holding fewer bytes, so the size
// checks pass and only the trailer read runs short.
type claimedSizeReader struct {
	io.ReaderAt
	size int64
}

func (r claimedSizeReader) Size() int64 { return r.size }

func (claimedSizeReader) Close() error { return nil }

func TestGraphTrailerMustBeComplete(t *testing.T) {
	t.Parallel()
	_, layers := fixtureLayers(t, formatcfg.SHA1)
	raw := layers[0][:len(layers[0])-1]
	for name, r := range map[string]ReaderAtCloser{
		"unsized": unsizedGraphReader{bytes.NewReader(raw)},
		"sized":   claimedSizeReader{bytes.NewReader(raw), int64(len(layers[0]))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := OpenFileIndex(r)
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		})
	}
}

func TestOpaqueParents(t *testing.T) {
	t.Parallel()
	_, layers := fixtureLayers(t, formatcfg.SHA1)
	base := openFixtureParents(t, formatcfg.SHA1, layers[:1])
	defer base.Close()
	idx, err := OpenFileIndexWithParent(graphTestReader{bytes.NewReader(layers[1])}, opaqueGraphIndex{base})
	require.NoError(t, err)
	require.Equal(t, uint32(2), idx.MaximumNumberOfHashes())
	wrong := NewMemoryIndex()
	wrong.Add(plumbing.NewHash(strings.Repeat("a", 64)), &CommitData{})
	_, err = OpenFileIndexWithParent(graphTestReader{bytes.NewReader(layers[1])}, wrong)
	require.ErrorIs(t, err, ErrObjectFormatMismatch)
	_, err = OpenFileIndexWithParent(graphTestReader{bytes.NewReader(layers[1])}, NewMemoryIndex())
	require.NoError(t, err)
}

func fixtureFilesystem(t *testing.T, of formatcfg.ObjectFormat) billy.Filesystem {
	t.Helper()
	_, fs := fixtureGraph(t, "commit-graph-chain-"+of.String())
	return fs
}

func TestChainOptionsAndIdentities(t *testing.T) {
	t.Parallel()
	for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(of.String(), func(t *testing.T) {
			t.Parallel()
			fs := fixtureFilesystem(t, of)
			idx, err := OpenChainOrFileIndex(fs, WithObjectFormat(of))
			require.NoError(t, err)
			require.Equal(t, uint32(3), idx.MaximumNumberOfHashes())
			require.NoError(t, idx.Close())
			names, _ := fixtureLayers(t, of)
			wrongName := strings.Repeat("f", of.HexSize())
			dir := "objects/info/commit-graphs"
			require.NoError(t, fs.Rename(path.Join(dir, "graph-"+names[0]+".graph"), path.Join(dir, "graph-"+wrongName+".graph")))
			names[0] = wrongName
			require.NoError(t, util.WriteFile(fs, path.Join(dir, "commit-graph-chain"), []byte(strings.Join(names, "\n")+"\n"), 0o644))
			_, err = OpenChainIndex(fs, WithObjectFormat(of))
			require.ErrorIs(t, err, ErrMalformedCommitGraphFile)
		})
	}
}

func TestGitWrittenChainAt256BaseGraphs(t *testing.T) {
	t.Parallel()
	fixture, fs := fixtureGraph(t, "commit-graph-chain-sha1-257")
	idx, err := OpenChainIndex(fs)
	require.NoError(t, err)
	defer idx.Close()
	require.Equal(t, uint32(257), idx.MaximumNumberOfHashes())
	for _, entry := range fixture.CommitGraphEntries() {
		_, err := idx.GetIndexByHash(plumbing.NewHash(entry.Hash))
		require.NoError(t, err)
	}
}

type trackedGraphFile struct {
	billy.File
	closes map[string]int
}

func (f trackedGraphFile) Close() error {
	f.closes[f.Name()]++
	return f.File.Close()
}

type trackedGraphFS struct {
	billy.Filesystem
	closes map[string]int
	fail   string
}

func (fs trackedGraphFS) Open(name string) (billy.File, error) {
	if name == fs.fail {
		return nil, io.ErrClosedPipe
	}
	f, err := fs.Filesystem.Open(name)
	if err != nil {
		return nil, err
	}
	return trackedGraphFile{File: f, closes: fs.closes}, nil
}

func TestChainFailureReleasesOwnedFiles(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"missing first", "bad first", "bad second", "main open"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			fs := trackedGraphFS{Filesystem: fixtureFilesystem(t, formatcfg.SHA1), closes: make(map[string]int)}
			names, _ := fixtureLayers(t, formatcfg.SHA1)
			first := "objects/info/commit-graphs/graph-" + names[0] + ".graph"
			second := "objects/info/commit-graphs/graph-" + names[1] + ".graph"
			switch failure {
			case "missing first":
				require.NoError(t, fs.Remove(first))
			case "bad first":
				require.NoError(t, util.WriteFile(fs.Filesystem, first, []byte("bad"), 0o644))
			case "bad second":
				require.NoError(t, util.WriteFile(fs.Filesystem, second, []byte("bad"), 0o644))
			case "main open":
				fs.fail = "objects/info/commit-graph"
			}
			var err error
			require.NotPanics(t, func() { _, err = OpenChainOrFileIndex(fs) })
			require.Error(t, err)
			if failure == "main open" {
				require.ErrorIs(t, err, io.ErrClosedPipe)
				return
			}
			if failure != "missing first" {
				require.Equal(t, 1, fs.closes[first])
			}
			if failure == "bad second" {
				require.Equal(t, 1, fs.closes[second])
			}
		})
	}
}

func insertUnknownFixtureChunk(t *testing.T, raw []byte, after string, partial bool, of formatcfg.ObjectFormat) []byte {
	t.Helper()
	count := int(raw[6])
	next, cut := 0, 0
	for i := range count {
		entry := 8 + i*12
		if string(raw[entry:entry+4]) == after {
			next = i + 1
			cut = int(binary.BigEndian.Uint64(raw[entry+16 : entry+24]))
			if partial {
				cut = int(binary.BigEndian.Uint64(raw[entry+4:entry+12])) + of.Size()
			}
			break
		}
	}
	require.NotZero(t, next)
	added := []byte{1, 2, 3, 4}
	if partial {
		added = nil
	}
	out := bytes.Clone(raw[:8])
	out[6]++
	for i := 0; i <= count; i++ {
		if i == next {
			out = append(out, []byte("TEST")...)
			out = binary.BigEndian.AppendUint64(out, uint64(cut+12))
		}
		entry := 8 + i*12
		out = append(out, raw[entry:entry+4]...)
		offset := int(binary.BigEndian.Uint64(raw[entry+4:entry+12])) + 12
		if i >= next {
			offset += len(added)
		}
		out = binary.BigEndian.AppendUint64(out, uint64(offset))
	}
	out = append(out, raw[8+(count+1)*12:cut]...)
	out = append(out, added...)
	out = append(out, raw[cut:]...)
	return fixtureChecksum(out, of)
}

func TestUnknownChunksBoundKnownChunks(t *testing.T) {
	t.Parallel()
	for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(of.String(), func(t *testing.T) {
			t.Parallel()
			_, fs := fixtureGraph(t, "commit-graph-"+of.String())
			standalone, err := util.ReadFile(fs, "objects/info/commit-graph")
			require.NoError(t, err)
			for _, after := range []string{"CDAT", "GDA2"} {
				raw := insertUnknownFixtureChunk(t, standalone, after, false, of)
				idx, err := OpenFileIndex(graphTestReader{bytes.NewReader(raw)}, WithObjectFormat(of))
				require.NoError(t, err, after)
				require.NoError(t, idx.Close())
			}
			_, layers := fixtureLayers(t, of)
			parent := openFixtureParents(t, of, layers[:2])
			defer parent.Close()
			raw := insertUnknownFixtureChunk(t, layers[2], "BASE", true, of)
			_, err = OpenFileIndexWithParent(graphTestReader{bytes.NewReader(raw)}, parent, WithObjectFormat(of))
			require.ErrorIs(t, err, ErrMalformedCommitGraphFile)
		})
	}
}

func fixtureGraph(t *testing.T, tag string) (*fixtures.Fixture, billy.Filesystem) {
	t.Helper()
	matches := fixtures.ByTag(tag)
	require.Len(t, matches, 1)
	fixture := matches.One()
	fs, err := fixture.DotGit(fixtures.WithMemFS())
	require.NoError(t, err)
	return fixture, fs
}

func TestChainTopLayerMustMatchItsName(t *testing.T) {
	t.Parallel()
	for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(of.String(), func(t *testing.T) {
			t.Parallel()
			fs := fixtureFilesystem(t, of)
			names, _ := fixtureLayers(t, of)
			top := len(names) - 1
			wrongName := strings.Repeat("f", of.HexSize())
			dir := "objects/info/commit-graphs"
			require.NoError(t, fs.Rename(path.Join(dir, "graph-"+names[top]+".graph"), path.Join(dir, "graph-"+wrongName+".graph")))
			names[top] = wrongName
			require.NoError(t, util.WriteFile(fs, path.Join(dir, "commit-graph-chain"), []byte(strings.Join(names, "\n")+"\n"), 0o644))
			_, err := OpenChainIndex(fs, WithObjectFormat(of))
			require.ErrorIs(t, err, ErrMalformedCommitGraphFile)
		})
	}
}

func TestLayerWithBaseGraphsNeedsParent(t *testing.T) {
	t.Parallel()
	_, layers := fixtureLayers(t, formatcfg.SHA1)
	_, err := OpenFileIndex(graphTestReader{bytes.NewReader(layers[1])})
	require.ErrorIs(t, err, ErrMalformedCommitGraphFile)

	idx, err := OpenFileIndex(graphTestReader{bytes.NewReader(layers[0])})
	require.NoError(t, err)
	require.NoError(t, idx.Close())
}

func TestSHA256ChainNeedsObjectFormat(t *testing.T) {
	t.Parallel()
	_, err := OpenChainIndex(fixtureFilesystem(t, formatcfg.SHA256))
	require.ErrorIs(t, err, ErrObjectFormatMismatch)
}

func TestUnknownHashVersion(t *testing.T) {
	t.Parallel()
	_, layers := fixtureLayers(t, formatcfg.SHA1)
	raw := bytes.Clone(layers[0])
	raw[5] = 3
	_, err := OpenFileIndex(graphTestReader{bytes.NewReader(raw)})
	require.ErrorIs(t, err, ErrUnsupportedHashFunction)
}

func TestChainFileRejectsOtherFormatWidth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		of  formatcfg.ObjectFormat
		hex string
	}{
		{formatcfg.SHA1, strings.Repeat("a", formatcfg.SHA256.HexSize())},
		{formatcfg.SHA256, strings.Repeat("a", formatcfg.SHA1.HexSize())},
	} {
		_, err := OpenChainFile(strings.NewReader(tc.hex+"\n"), WithObjectFormat(tc.of))
		require.ErrorIs(t, err, ErrObjectFormatMismatch, tc.of)

		chain, err := OpenChainFile(strings.NewReader(strings.Repeat("b", tc.of.HexSize())+"\n"), WithObjectFormat(tc.of))
		require.NoError(t, err)
		require.Len(t, chain, 1)
	}
}

func TestReadObjectFormat(t *testing.T) {
	t.Parallel()
	for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		_, layers := fixtureLayers(t, of)
		got, err := ReadObjectFormat(bytes.NewReader(layers[0]))
		require.NoError(t, err)
		require.Equal(t, of, got)
	}

	_, layers := fixtureLayers(t, formatcfg.SHA1)
	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
		err    error
	}{
		{"signature", func(b []byte) []byte { b[0] = 'X'; return b }, ErrMalformedCommitGraphFile},
		{"version", func(b []byte) []byte { b[4] = 2; return b }, ErrUnsupportedVersion},
		{"hash version", func(b []byte) []byte { b[5] = 3; return b }, ErrUnsupportedHashFunction},
		{"truncated", func(b []byte) []byte { return b[:6] }, io.EOF},
	} {
		_, err := ReadObjectFormat(bytes.NewReader(tc.mutate(bytes.Clone(layers[0]))))
		require.ErrorIs(t, err, tc.err, tc.name)
	}
}
