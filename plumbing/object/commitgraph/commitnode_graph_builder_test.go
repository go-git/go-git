package commitgraph

import (
	"reflect"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	commitgraph "github.com/go-git/go-git/v5/plumbing/format/commitgraph/v2"
	. "gopkg.in/check.v1"
)

type noGenerationDataIndex struct {
	commitgraph.Index
}

func (f *noGenerationDataIndex) GetCommitDataByIndex(i uint32) (*commitgraph.CommitData, error) {
	data, err := f.Index.GetCommitDataByIndex(i)
	if err != nil {
		return data, err
	}
	return &commitgraph.CommitData{
		TreeHash:      data.TreeHash,
		ParentIndexes: data.ParentIndexes,
		ParentHashes:  data.ParentHashes,
		Generation:    data.Generation,
		GenerationV2:  0,
		When:          data.When,
	}, err
}

func (f *noGenerationDataIndex) HasGenerationV2() bool {
	return false
}

func (s *CommitNodeSuite) TestCreateCommitNodeGraph(c *C) {
	f := fixtures.ByTag("commit-graph-chain-2").One()

	storer := unpackRepository(f)

	index, err := commitgraph.OpenChainOrFileIndex(storer.Filesystem())
	c.Assert(err, IsNil)

	newIndex, err := CreateCommitGraph(storer, index, CreateCommitNodeGraphOptions{})
	c.Assert(err, IsNil)

	appendIndex, err := CreateCommitGraph(storer, index, CreateCommitNodeGraphOptions{Append: true})
	c.Assert(err, IsNil)

	appendIndexNoGeneration, err := CreateCommitGraph(storer, &noGenerationDataIndex{index}, CreateCommitNodeGraphOptions{Append: true})
	c.Assert(err, IsNil)

	chainIndex, err := CreateCommitGraph(storer, index, CreateCommitNodeGraphOptions{Chain: true})
	c.Assert(err, IsNil)

	chainIndexNoGeneration, err := CreateCommitGraph(storer, &noGenerationDataIndex{index}, CreateCommitNodeGraphOptions{Chain: true})
	c.Assert(err, IsNil)

	fullIndex, err := CreateCommitGraph(storer, nil, CreateCommitNodeGraphOptions{})
	c.Assert(err, IsNil)

	c.Assert(len(newIndex.Hashes()), Equals, len(index.Hashes()))

	hashesInNewIndex := make([]string, newIndex.MaximumNumberOfHashes())
	for _, hash := range newIndex.Hashes() {
		nidx, err := newIndex.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		hashesInNewIndex[nidx] = hash.String()
		_, err = appendIndex.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		_, err = appendIndexNoGeneration.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		_, err = chainIndex.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		_, err = chainIndexNoGeneration.GetIndexByHash(hash)
		c.Assert(err, IsNil)
	}

	hashesInIndex := make([]string, index.MaximumNumberOfHashes())
	for _, hash := range index.Hashes() {
		oidx, err := index.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		hashesInIndex[oidx] = hash.String()
		_, err = appendIndex.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		chainIdx, err := chainIndex.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		c.Assert(chainIdx, Equals, oidx)
	}

	hashesInFullIndex := make([]string, fullIndex.MaximumNumberOfHashes())
	for _, hash := range fullIndex.Hashes() {
		fidx, err := fullIndex.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		hashesInFullIndex[fidx] = hash.String()
	}

	c.Assert(hashesInNewIndex, DeepEquals, hashesInFullIndex)
	c.Assert(hashesInNewIndex, DeepEquals, hashesInFullIndex)

	for _, hash := range newIndex.Hashes() {
		fidx, err := fullIndex.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		nidx, err := newIndex.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		newData, err := newIndex.GetCommitDataByIndex(nidx)
		c.Assert(err, IsNil)
		fullData, err := fullIndex.GetCommitDataByIndex(fidx)
		c.Assert(err, IsNil)
		c.Assert(newData, CommitDataChecker, fullData)
		chainIdx, err := chainIndexNoGeneration.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		dataChained, err := chainIndexNoGeneration.GetCommitDataByIndex(chainIdx)
		c.Assert(err, IsNil)
		c.Assert(newData, CommitDataChecker, dataChained)
	}
	for _, hash := range appendIndex.Hashes() {
		idx, err := appendIndex.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		data, err := appendIndex.GetCommitDataByIndex(idx)
		c.Assert(err, IsNil)
		noGenIdx, err := appendIndexNoGeneration.GetIndexByHash(hash)
		c.Assert(err, IsNil)
		dataNoGen, err := appendIndexNoGeneration.GetCommitDataByIndex(noGenIdx)
		c.Assert(err, IsNil)
		c.Assert(data, CommitDataChecker, dataNoGen)
	}
}

type commitDataChecker struct {
	*CheckerInfo
}

var CommitDataChecker = &commitDataChecker{
	&CheckerInfo{Name: "CommitDataChecker", Params: []string{"obtained", "expected"}},
}

func (checker *commitDataChecker) Check(params []interface{}, names []string) (result bool, error string) {
	if params[0] == params[1] {
		return true, ""
	}

	left, right := params[0].(*commitgraph.CommitData), params[1].(*commitgraph.CommitData)
	if left == nil ||
		right == nil ||
		left.TreeHash != right.TreeHash ||
		!reflect.DeepEqual(left.ParentIndexes, right.ParentIndexes) ||
		!reflect.DeepEqual(left.ParentHashes, right.ParentHashes) ||
		left.Generation != right.Generation ||
		left.GenerationV2 != right.GenerationV2 ||
		left.When.Unix() != right.When.Unix() {
		return DeepEquals.Check(params, names)
	}

	return true, ""
}
