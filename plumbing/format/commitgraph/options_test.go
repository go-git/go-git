package commitgraph

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

type graphTestReader struct{ *bytes.Reader }

func (graphTestReader) Close() error { return nil }

type suppliedDataIndex struct {
	Index
	data *CommitData
}

func (idx suppliedDataIndex) GetCommitDataByIndex(uint32) (*CommitData, error) {
	return idx.data, nil
}

func TestGitWrittenObjectFormats(t *testing.T) {
	t.Parallel()
	for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(of.String(), func(t *testing.T) {
			t.Parallel()
			fixture, fs := fixtureGraph(t, "commit-graph-"+of.String())
			f, err := fs.Open("objects/info/commit-graph")
			require.NoError(t, err)
			idx, err := OpenFileIndex(f, WithObjectFormat(of))
			if err != nil {
				_ = f.Close()
			}
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, idx.Close()) })
			for i, entry := range fixture.CommitGraphEntries() {
				h := plumbing.NewHash(entry.Hash)
				pos, err := idx.GetIndexByHash(h)
				require.NoError(t, err)
				data, err := idx.GetCommitDataByIndex(pos)
				require.NoError(t, err)
				require.Equal(t, entry.Tree, data.TreeHash.String())
				require.Equal(t, int64(1700000000+i), data.When.Unix())
				require.Len(t, data.ParentHashes, len(entry.Parents))
				for j, p := range entry.Parents {
					require.Equal(t, p, data.ParentHashes[j].String())
				}
			}
			wrong := plumbing.NewHash(strings.Repeat("a", 104-of.HexSize()))
			_, err = idx.GetIndexByHash(wrong)
			require.ErrorIs(t, err, ErrObjectFormatMismatch)
		})
	}
}

func TestDefaultReaderRejectsSHA256(t *testing.T) {
	t.Parallel()
	_, fs := fixtureGraph(t, "commit-graph-sha256")
	f, err := fs.Open("objects/info/commit-graph")
	require.NoError(t, err)
	defer f.Close()
	_, err = OpenFileIndex(f)
	require.ErrorIs(t, err, ErrObjectFormatMismatch)
}

func TestEncodeEmptyObjectFormats(t *testing.T) {
	t.Parallel()
	for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(of.String(), func(t *testing.T) {
			t.Parallel()
			var b bytes.Buffer
			require.NoError(t, NewEncoder(&b, WithObjectFormat(of)).Encode(NewMemoryIndex()))
			version := byte(1)
			if of == formatcfg.SHA256 {
				version = 2
			}
			require.Equal(t, version, b.Bytes()[5])
			idx, err := OpenFileIndex(graphTestReader{bytes.NewReader(b.Bytes())}, WithObjectFormat(of))
			require.NoError(t, err)
			require.Empty(t, idx.Hashes())
			require.NoError(t, idx.Close())
		})
	}
}

func TestEncodeRejectsWrongObjectWidthsBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"commit", "tree", "parent", "mixed commits"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			correct := plumbing.NewHash(strings.Repeat("a", 40))
			wrong := plumbing.NewHash(strings.Repeat("b", 64))
			mi := NewMemoryIndex()
			data := &CommitData{TreeHash: correct, When: time.Unix(1, 0)}
			h := correct
			switch field {
			case "commit":
				h = wrong
			case "tree":
				data.TreeHash = wrong
			case "parent":
				data.ParentHashes = []plumbing.Hash{wrong}
			case "mixed commits":
				mi.Add(wrong, &CommitData{TreeHash: wrong})
			}
			mi.Add(h, data)
			var idx Index = mi
			if field == "parent" {
				idx = suppliedDataIndex{Index: mi, data: data}
			}
			var b bytes.Buffer
			err := NewEncoder(&b).Encode(idx)
			require.ErrorIs(t, err, ErrObjectFormatMismatch)
			require.Zero(t, b.Len())
		})
	}
}

func TestInvalidObjectFormat(t *testing.T) {
	t.Parallel()
	opts := WithObjectFormat(formatcfg.ObjectFormat("invalid"))
	_, err := OpenFileIndex(graphTestReader{bytes.NewReader(nil)}, opts)
	require.ErrorIs(t, err, formatcfg.ErrInvalidObjectFormat)
	_, err = OpenChainFile(strings.NewReader(""), opts)
	require.ErrorIs(t, err, formatcfg.ErrInvalidObjectFormat)
	var b bytes.Buffer
	err = NewEncoder(&b, opts).Encode(NewMemoryIndex())
	require.ErrorIs(t, err, formatcfg.ErrInvalidObjectFormat)
	require.Zero(t, b.Len())
}
