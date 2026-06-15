package reftable

import (
	"fmt"
	"sync"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openFixtureStack(t *testing.T) *Stack {
	t.Helper()
	f := fixtures.ByTag("reftable").One()
	dotgit, err := f.DotGit()
	require.NoError(t, err)

	reftableDir, err := dotgit.Chroot("reftable")
	require.NoError(t, err)

	stack, err := OpenStack(reftableDir, 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stack.Close() })
	return stack
}

func TestStackRef(t *testing.T) {
	t.Parallel()
	stack := openFixtureStack(t)

	// HEAD should exist.
	head, err := stack.Ref("HEAD")
	require.NoError(t, err)
	require.NotNil(t, head)
	assert.Equal(t, uint8(refValueSymref), head.ValueType)
	assert.NotEmpty(t, head.Target)

	// The branch HEAD points to should also exist.
	branch, err := stack.Ref(head.Target)
	require.NoError(t, err)
	require.NotNil(t, branch, "branch %s not found", head.Target)
	assert.Equal(t, uint8(refValueVal1), branch.ValueType)
}

func TestStackRefNotFound(t *testing.T) {
	t.Parallel()
	stack := openFixtureStack(t)

	rec, err := stack.Ref("refs/heads/nonexistent")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestStackIterRefs(t *testing.T) {
	t.Parallel()
	stack := openFixtureStack(t)

	var names []string
	err := stack.IterRefs(func(rec RefRecord) bool {
		names = append(names, rec.RefName)
		return true
	})
	require.NoError(t, err)

	assert.Contains(t, names, "HEAD")
	assert.Greater(t, len(names), 1, "expected more than just HEAD")
}

func TestStackLogsFor(t *testing.T) {
	t.Parallel()
	stack := openFixtureStack(t)

	// Find the branch HEAD points to.
	head, err := stack.Ref("HEAD")
	require.NoError(t, err)
	require.NotNil(t, head)

	logs, err := stack.LogsFor(head.Target)
	require.NoError(t, err)

	// Logs may be empty depending on fixture content.
	if len(logs) > 1 {
		// Should be newest first.
		assert.GreaterOrEqual(t, logs[0].UpdateIndex, logs[1].UpdateIndex)
	}
}

func TestStackCompaction(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	stack, err := OpenStack(fs, 20)
	require.NoError(t, err)
	defer func() { _ = stack.Close() }()

	err = stack.SetRef(RefRecord{RefName: "refs/heads/branch-1", ValueType: refValueVal1, Value: []byte("11111111111111111111")})
	require.NoError(t, err)

	err = stack.SetRef(RefRecord{RefName: "refs/heads/branch-2", ValueType: refValueVal1, Value: []byte("22222222222222222222")})
	require.NoError(t, err)

	err = stack.SetRef(RefRecord{RefName: "refs/heads/branch-3", ValueType: refValueVal1, Value: []byte("33333333333333333333")})
	require.NoError(t, err)

	assert.Len(t, stack.tables, 3)

	r1, err := stack.Ref("refs/heads/branch-1")
	require.NoError(t, err)
	assert.NotNil(t, r1)
	assert.Equal(t, []byte("11111111111111111111"), r1.Value)

	r2, err := stack.Ref("refs/heads/branch-2")
	require.NoError(t, err)
	assert.NotNil(t, r2)
	assert.Equal(t, []byte("22222222222222222222"), r2.Value)

	err = stack.Compact()
	require.NoError(t, err)

	assert.Len(t, stack.tables, 1)

	r1Merged, err := stack.Ref("refs/heads/branch-1")
	require.NoError(t, err)
	assert.NotNil(t, r1Merged)
	assert.Equal(t, []byte("11111111111111111111"), r1Merged.Value)

	r3Merged, err := stack.Ref("refs/heads/branch-3")
	require.NoError(t, err)
	assert.NotNil(t, r3Merged)
	assert.Equal(t, []byte("33333333333333333333"), r3Merged.Value)
}

func TestStackAutoCompaction(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	stack, err := OpenStack(fs, 20)
	require.NoError(t, err)
	defer func() { _ = stack.Close() }()

	for i := 1; i <= 6; i++ {
		err = stack.SetRef(RefRecord{
			RefName:   fmt.Sprintf("refs/heads/branch-%d", i),
			ValueType: refValueVal1,
			Value:     fmt.Appendf(nil, "%020d", i),
		})
		require.NoError(t, err)
	}

	assert.Len(t, stack.tables, 1)

	r1, err := stack.Ref("refs/heads/branch-1")
	require.NoError(t, err)
	assert.NotNil(t, r1)
	assert.Equal(t, []byte("00000000000000000001"), r1.Value)

	r6, err := stack.Ref("refs/heads/branch-6")
	require.NoError(t, err)
	assert.NotNil(t, r6)
	assert.Equal(t, []byte("00000000000000000006"), r6.Value)
}

func TestStackConcurrentWrites(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	stack, err := OpenStack(fs, 20)
	require.NoError(t, err)
	defer func() { _ = stack.Close() }()

	const numGoroutines = 10
	const writesPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ { //nolint:modernize
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ { //nolint:modernize
				err := stack.SetRef(RefRecord{
					RefName:   fmt.Sprintf("refs/heads/g-%d-%d", gid, i),
					ValueType: refValueVal1,
					Value:     []byte("11111111111111111111"),
				})
				if err != nil {
					t.Errorf("SetRef failed: %v", err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify all refs are present and correct.
	for g := 0; g < numGoroutines; g++ { //nolint:modernize
		for i := 0; i < writesPerGoroutine; i++ { //nolint:modernize
			name := fmt.Sprintf("refs/heads/g-%d-%d", g, i)
			ref, err := stack.Ref(name)
			require.NoError(t, err)
			require.NotNil(t, ref, "ref %s was lost", name)
			assert.Equal(t, []byte("11111111111111111111"), ref.Value)
		}
	}
}

func TestStackReloadCaching(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	stack, err := OpenStack(fs, 20)
	require.NoError(t, err)
	defer func() { _ = stack.Close() }()

	err = stack.SetRef(RefRecord{RefName: "refs/heads/branch-1", ValueType: refValueVal1, Value: []byte("11111111111111111111")})
	require.NoError(t, err)
	assert.Len(t, stack.tables, 1)
	t1 := stack.tables[0]

	err = stack.SetRef(RefRecord{RefName: "refs/heads/branch-2", ValueType: refValueVal1, Value: []byte("22222222222222222222")})
	require.NoError(t, err)
	assert.Len(t, stack.tables, 2)

	assert.Same(t, t1, stack.tables[0])
}

func TestSuggestCompactionSegment(t *testing.T) {
	t.Parallel()

	tc := []struct {
		sizes []uint64
		wantS int
		wantE int
	}{
		{[]uint64{100, 100}, 0, 1},
		{[]uint64{1000, 100, 50}, -1, -1},
		{[]uint64{1000, 100, 200}, 1, 2},
		{[]uint64{10000, 1000, 100, 50, 20}, -1, -1},
		{[]uint64{10, 5, 2}, -1, -1},
		{[]uint64{10, 5, 2, 3}, 0, 3},
	}

	for _, tt := range tc {
		s, e := suggestCompactionSegment(tt.sizes)
		assert.Equal(t, tt.wantS, s, "sizes: %v", tt.sizes)
		assert.Equal(t, tt.wantE, e, "sizes: %v", tt.sizes)
	}
}
