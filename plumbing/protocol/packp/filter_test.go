package packp

import (
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFilterBlobNone(t *testing.T) {
	require.EqualValues(t, "blob:none", FilterBlobNone())
}

func TestFilterBlobLimit(t *testing.T) {
	require.EqualValues(t, "blob:limit=0", FilterBlobLimit(0, BlobLimitPrefixNone))
	require.EqualValues(t, "blob:limit=1000", FilterBlobLimit(1000, BlobLimitPrefixNone))
	require.EqualValues(t, "blob:limit=4k", FilterBlobLimit(4, BlobLimitPrefixKibi))
	require.EqualValues(t, "blob:limit=4m", FilterBlobLimit(4, BlobLimitPrefixMebi))
	require.EqualValues(t, "blob:limit=4g", FilterBlobLimit(4, BlobLimitPrefixGibi))
}

func TestFilterTreeDepth(t *testing.T) {
	require.EqualValues(t, "tree:0", FilterTreeDepth(0))
	require.EqualValues(t, "tree:1", FilterTreeDepth(1))
	require.EqualValues(t, "tree:2", FilterTreeDepth(2))
}

func TestFilterObjectType(t *testing.T) {
	filter, err := FilterObjectType(plumbing.TagObject)
	require.NoError(t, err)
	require.EqualValues(t, "object:type=tag", filter)

	filter, err = FilterObjectType(plumbing.CommitObject)
	require.NoError(t, err)
	require.EqualValues(t, "object:type=commit", filter)

	filter, err = FilterObjectType(plumbing.TreeObject)
	require.NoError(t, err)
	require.EqualValues(t, "object:type=tree", filter)

	filter, err = FilterObjectType(plumbing.BlobObject)
	require.NoError(t, err)
	require.EqualValues(t, "object:type=blob", filter)

	_, err = FilterObjectType(plumbing.InvalidObject)
	require.Error(t, err)

	_, err = FilterObjectType(plumbing.OFSDeltaObject)
	require.Error(t, err)
}

func TestFilterCombine(t *testing.T) {
	require.EqualValues(t, "combine:tree%3A2+blob%3Anone",
		FilterCombine(
			FilterTreeDepth(2),
			FilterBlobNone(),
		),
	)
}
