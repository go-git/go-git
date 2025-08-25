package index

import (
	"bytes"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
)

func (s *IndexSuite) TestIndexAdd() {
	idx := &Index{}
	e := idx.Add("foo")
	e.Size = 42

	e, err := idx.Entry("foo")
	s.NoError(err)
	s.Equal("foo", e.Name)
	s.Equal(uint32(42), e.Size)
}

func (s *IndexSuite) TestIndexEntry() {
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo", Size: 42},
			{Name: "bar", Size: 82},
		},
	}

	e, err := idx.Entry("foo")
	s.NoError(err)
	s.Equal("foo", e.Name)

	e, err = idx.Entry("missing")
	s.Nil(e)
	s.ErrorIs(err, ErrEntryNotFound)
}

func (s *IndexSuite) TestIndexRemove() {
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo", Size: 42},
			{Name: "bar", Size: 82},
		},
	}

	e, err := idx.Remove("foo")
	s.NoError(err)
	s.Equal("foo", e.Name)

	e, err = idx.Remove("foo")
	s.Nil(e)
	s.ErrorIs(err, ErrEntryNotFound)
}

func (s *IndexSuite) TestIndexGlob() {
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo/bar/bar", Size: 42},
			{Name: "foo/baz/qux", Size: 42},
			{Name: "fux", Size: 82},
		},
	}

	m, err := idx.Glob(filepath.Join("foo", "b*"))
	s.NoError(err)
	s.Len(m, 2)
	s.Equal("foo/bar/bar", m[0].Name)
	s.Equal("foo/baz/qux", m[1].Name)

	m, err = idx.Glob("f*")
	s.NoError(err)
	s.Len(m, 3)

	m, err = idx.Glob("f*/baz/q*")
	s.NoError(err)
	s.Len(m, 1)
}

func (s *IndexSuite) TestExtensions_EOIE() {
	idx := &Index{
		Version: 4,
		EndOfIndexEntry: &EndOfIndexEntry{
			Offset: 1234,
			Hash:   plumbing.NewHash("abcd1234abcd1234abcd1234abcd1234abcd1234"),
		},
	}

	var buffer bytes.Buffer

	encoder := NewEncoder(&buffer)
	s.NoError(encoder.Encode(idx))

	decoder := NewDecoder(&buffer)
	out := &Index{}

	s.NoError(decoder.Decode(out))
	s.NotNil(out.EndOfIndexEntry)

	s.Equal(uint32(1234), out.EndOfIndexEntry.Offset)
	s.Equal(idx.EndOfIndexEntry.Hash, out.EndOfIndexEntry.Hash)
}

func (s *IndexSuite) TestExtensions_TREE() {
	idx := &Index{
		Version: 4,
		Cache: &Tree{
			Entries: []TreeEntry{
				{
					Path:    "",
					Entries: 5,
					Trees:   2,
					Hash:    plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				},
				{
					Path:    "src",
					Entries: 3,
					Trees:   1,
					Hash:    plumbing.NewHash("aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"),
				}, {
					Path:    "x",
					Entries: 1,
					Trees:   0,
					Hash:    plumbing.NewHash("abcd1234abcd1234abcd1234abcd1234abcd1234"),
				},
				{
					Path:    "docs",
					Entries: -1,
					Trees:   0,
				},
			},
		},
	}

	var buffer bytes.Buffer

	encoder := NewEncoder(&buffer)
	s.NoError(encoder.Encode(idx))

	decoder := NewDecoder(&buffer)
	out := &Index{}

	s.NoError(decoder.Decode(out))

	s.NotNil(out.Cache)
	s.NotEmpty(out.Cache.Entries)
	s.Equal(len(idx.Cache.Entries), len(out.Cache.Entries))

	for i := range idx.Cache.Entries {
		s.Equal(idx.Cache.Entries[i].Path, out.Cache.Entries[i].Path)
		s.Equal(idx.Cache.Entries[i].Hash, out.Cache.Entries[i].Hash)
		s.Equal(idx.Cache.Entries[i].Entries, out.Cache.Entries[i].Entries)
		s.Equal(idx.Cache.Entries[i].Trees, out.Cache.Entries[i].Trees)
	}
}

func (s *IndexSuite) TestExtensions_REUC() {
	idx := &Index{
		Version: 4,
		ResolveUndo: &ResolveUndo{
			Entries: []ResolveUndoEntry{
				{
					Path: "a.txt",
					Stages: map[Stage]plumbing.Hash{
						AncestorMode: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
						OurMode:      plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
						TheirMode:    plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
					},
				},
				{
					Path: "b.txt",
					Stages: map[Stage]plumbing.Hash{
						AncestorMode: plumbing.NewHash("1111111111111111111111111111111111111111"),
						OurMode:      plumbing.NewHash("3333333333333333333333333333333333333333"),
					},
				},
				{
					Path: "c.txt",
					Stages: map[Stage]plumbing.Hash{
						AncestorMode: plumbing.NewHash("1111111111111111111111111111111111111111"),
						TheirMode:    plumbing.NewHash("2222222222222222222222222222222222222222"),
					},
				},
				{
					Path:   "d.txt",
					Stages: map[Stage]plumbing.Hash{},
				},
			},
		},
	}

	var buffer bytes.Buffer

	encoder := NewEncoder(&buffer)
	s.NoError(encoder.Encode(idx))

	decoder := NewDecoder(&buffer)
	out := &Index{}

	s.NoError(decoder.Decode(out))
	s.NotNil(out.ResolveUndo)
	s.Equal(len(idx.ResolveUndo.Entries), len(out.ResolveUndo.Entries))

	for i := range idx.ResolveUndo.Entries {
		s.Equal(idx.ResolveUndo.Entries[i].Path, out.ResolveUndo.Entries[i].Path)
		s.Equal(idx.ResolveUndo.Entries[i].Stages[AncestorMode], out.ResolveUndo.Entries[i].Stages[AncestorMode])
		s.Equal(idx.ResolveUndo.Entries[i].Stages[TheirMode], out.ResolveUndo.Entries[i].Stages[TheirMode])
		s.Equal(idx.ResolveUndo.Entries[i].Stages[OurMode], out.ResolveUndo.Entries[i].Stages[OurMode])
	}
}

func (s *IndexSuite) TestExtensions_LINK() {
	idx := &Index{
		Version: 4,
		Link: &Link{
			ObjectID: plumbing.NewHash("abcd1234abcd1234abcd1234abcd1234abcd1234"),

			// Valid EWAH-compressed bitmap [0, 2, 4].
			DeleteBitmap: []byte{
				0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x02,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0xa8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
			},

			// Valid EWAH-compressed bitmap [1, 3, 5].
			ReplaceBitmap: []byte{
				0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, 0x02,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0x54, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
			},
		},
	}

	var buffer bytes.Buffer

	encoder := NewEncoder(&buffer)
	s.NoError(encoder.Encode(idx))

	decoder := NewDecoder(&buffer)
	out := &Index{}

	s.NoError(decoder.Decode(out))
	s.NotNil(out.Link)
	s.Equal(idx.Link.ObjectID, out.Link.ObjectID)
	s.Equal(idx.Link.DeleteBitmap, out.Link.DeleteBitmap)
	s.Equal(idx.Link.ReplaceBitmap, out.Link.ReplaceBitmap)
}

func (s *IndexSuite) TestExtensions_UNTR() {
	idx := &Index{
		Version: 4,
		UntrackedCache: &UntrackedCache{
			Environments: []string{"BAR=FOO", "FOO=BAR"},

			InfoExcludeStats: UntrackedCacheStats{
				CreatedAt:  time.Date(2018, 1, 1, 0, 0, 0, 0, time.Local),
				ModifiedAt: time.Date(2019, 1, 1, 0, 0, 0, 0, time.Local),

				Dev: 1, Inode: 100, UID: 1001, GID: 101, Size: 1000,
			},
			ExcludesFileStats: UntrackedCacheStats{
				CreatedAt:  time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local),
				ModifiedAt: time.Date(2021, 1, 1, 0, 0, 0, 0, time.Local),

				Dev: 2, Inode: 200, UID: 2002, GID: 202, Size: 2000,
			},

			DirFlags: 0b01101010,

			InfoExcludeHash:  plumbing.NewHash("1111111111111111111111111111111111111111"),
			ExcludesFileHash: plumbing.NewHash("2222222222222222222222222222222222222222"),

			PerDirIgnoreFile: ".gitignore",

			Entries: []UntrackedCacheEntry{
				{
					Blocks:  2,
					Name:    "",
					Entries: []string{"docs", "pkg", "main.go"},
				},
				{
					Blocks:  0,
					Name:    "docs",
					Entries: []string{"README.md"},
				},
				{
					Blocks:  1,
					Name:    "pkg",
					Entries: []string{"extensions"},
				},
				{
					Blocks:  0,
					Name:    "extensions",
					Entries: []string{"extensions.go"},
				},
			},

			// Valid EWAH-compressed bitmap [0, 1].
			ValidBitmap: []byte{
				0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x02,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
			},

			// Valid EWAH-compressed bitmap [2, 3].
			CheckOnlyBitmap: []byte{
				0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x02,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0x30, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
			},

			// Valid EWAH-compressed bitmap [0, 3].
			MetadataBitmap: []byte{
				0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x02,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0x90, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
			},

			Stats: []UntrackedCacheStats{
				{
					CreatedAt:  time.Date(2022, 1, 1, 0, 0, 0, 0, time.Local),
					ModifiedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.Local),

					Dev: 3, Inode: 300, UID: 3003, GID: 303, Size: 3000,
				},
				{

					CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local),
					ModifiedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local),

					Dev: 4, Inode: 400, UID: 4004, GID: 404, Size: 4000,
				},
			},
			Hashes: []plumbing.Hash{
				plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
				plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
				plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"),
			},
		},
	}

	var buffer bytes.Buffer

	encoder := NewEncoder(&buffer)
	s.NoError(encoder.Encode(idx))

	decoder := NewDecoder(&buffer)
	out := &Index{}

	s.NoError(decoder.Decode(out))
	s.NotNil(out.UntrackedCache)

	s.Equal(idx.UntrackedCache.Environments, out.UntrackedCache.Environments)
	s.Equal(idx.UntrackedCache.InfoExcludeStats, out.UntrackedCache.InfoExcludeStats)
	s.Equal(idx.UntrackedCache.ExcludesFileStats, out.UntrackedCache.ExcludesFileStats)
	s.Equal(idx.UntrackedCache.DirFlags, out.UntrackedCache.DirFlags)
	s.Equal(idx.UntrackedCache.InfoExcludeHash, out.UntrackedCache.InfoExcludeHash)
	s.Equal(idx.UntrackedCache.ExcludesFileHash, out.UntrackedCache.ExcludesFileHash)
	s.Equal(idx.UntrackedCache.PerDirIgnoreFile, out.UntrackedCache.PerDirIgnoreFile)

	s.Equal(len(idx.UntrackedCache.Entries), len(out.UntrackedCache.Entries))
	for i := range idx.UntrackedCache.Entries {
		s.Equal(idx.UntrackedCache.Entries[i].Blocks, out.UntrackedCache.Entries[i].Blocks)
		s.Equal(idx.UntrackedCache.Entries[i].Name, out.UntrackedCache.Entries[i].Name)
		s.Equal(idx.UntrackedCache.Entries[i].Entries, out.UntrackedCache.Entries[i].Entries)
	}

	s.Equal(idx.UntrackedCache.ValidBitmap, out.UntrackedCache.ValidBitmap)
	s.Equal(idx.UntrackedCache.CheckOnlyBitmap, out.UntrackedCache.CheckOnlyBitmap)
	s.Equal(idx.UntrackedCache.MetadataBitmap, out.UntrackedCache.MetadataBitmap)

	s.Equal(len(idx.UntrackedCache.Stats), len(out.UntrackedCache.Stats))
	for i := range idx.UntrackedCache.Stats {
		s.Equal(idx.UntrackedCache.Stats[i], out.UntrackedCache.Stats[i])
	}

	s.Equal(len(idx.UntrackedCache.Hashes), len(out.UntrackedCache.Hashes))
	for i := range idx.UntrackedCache.Hashes {
		s.Equal(idx.UntrackedCache.Hashes[i], out.UntrackedCache.Hashes[i])
	}
}

func (s *IndexSuite) TestExtensions_FSMN() {
	indexes := []*Index{
		{
			Version: 4,
			FSMonitor: &FSMonitor{
				Version: 1,
				Since:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local),

				// Valid EWAH-compressed bitmap [0, 2, 4].
				DirtyBitmap: []byte{
					0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x02,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
					0xa8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00,
				},
			},
		},
		{
			Version: 4,
			FSMonitor: &FSMonitor{
				Version: 2,
				Token:   "fsmonitor example token",

				// Valid EWAH-compressed bitmap [1, 3, 5].
				DirtyBitmap: []byte{
					0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, 0x02,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
					0x54, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00,
				},
			},
		},
	}

	for _, i := range indexes {
		var buffer bytes.Buffer

		encoder := NewEncoder(&buffer)
		s.Require().NoError(encoder.Encode(i))

		decoder := NewDecoder(&buffer)
		out := &Index{}

		s.NoError(decoder.Decode(out))
		s.Require().NotNil(out.FSMonitor)
		s.Equal(i.FSMonitor.Version, out.FSMonitor.Version)
		s.Equal(i.FSMonitor.Token, out.FSMonitor.Token)
		s.Equal(i.FSMonitor.Since, out.FSMonitor.Since)
		s.Equal(i.FSMonitor.DirtyBitmap, out.FSMonitor.DirtyBitmap)
	}
}

func (s *IndexSuite) TestExtensions_IEOT() {
	idx := &Index{
		Version: 4,
		IndexEntryOffsetTable: &IndexEntryOffsetTable{
			Version: 1,
			Entries: []IndexEntryOffsetEntry{
				{Offset: 100, Count: 2},
				{Offset: 200, Count: 3},
			},
		},
	}

	var buffer bytes.Buffer

	encoder := NewEncoder(&buffer)
	s.Require().NoError(encoder.Encode(idx))

	decoder := NewDecoder(&buffer)
	out := &Index{}

	s.Require().NoError(decoder.Decode(out))
	s.Require().NotNil(out.IndexEntryOffsetTable)
	s.Equal(uint32(1), out.IndexEntryOffsetTable.Version)
	s.Require().Len(out.IndexEntryOffsetTable.Entries, 2)
	s.Equal(idx.IndexEntryOffsetTable.Entries[0].Offset, out.IndexEntryOffsetTable.Entries[0].Offset)
	s.Equal(idx.IndexEntryOffsetTable.Entries[0].Count, out.IndexEntryOffsetTable.Entries[0].Count)
	s.Equal(idx.IndexEntryOffsetTable.Entries[1].Offset, out.IndexEntryOffsetTable.Entries[1].Offset)
	s.Equal(idx.IndexEntryOffsetTable.Entries[1].Count, out.IndexEntryOffsetTable.Entries[1].Count)
}
