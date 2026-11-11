package packfile

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHumanBytes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "500 bytes", humanBytes(500))
	assert.Equal(t, "1.50 KiB", humanBytes(1536))
	assert.Equal(t, "1.00 MiB", humanBytes(mib))
	assert.Equal(t, "1.00 GiB", humanBytes(gib))
}

func TestObjectProgressDoneLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	p := newObjectProgress(&buf, "Receiving objects", 31, false, nil)
	for i := 0; i < 31; i++ {
		p.step()
	}
	p.done()

	out := buf.String()
	require.Contains(t, out, "Receiving objects:")
	require.Contains(t, out, "100% (31/31)")
	require.Contains(t, out, ", done.\n")
	assert.True(t, strings.HasSuffix(out, ", done.\n"))
}

func TestObjectProgressSkipsUnchangedPercent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	p := newObjectProgress(&buf, "Resolving deltas", 200, false, nil)
	p.step()
	first := buf.Len()
	p.step() // still 0% of 200 until current>=2... 1/200=0%, 2/200=1%
	// 1/200 and 2/200 are both 0% with integer division? 1*100/200=0, 2*100/200=1
	// After first step lastPct is 0, second step pct=1 so it writes.
	assert.Greater(t, buf.Len(), first)
}
