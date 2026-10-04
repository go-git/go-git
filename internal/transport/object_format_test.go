package transport

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestObjectFormatNilStorer(t *testing.T) {
	t.Parallel()
	assert.Equal(t, config.DefaultObjectFormat, ObjectFormat(nil))
	assert.Equal(t, config.DefaultObjectFormat, ObjectFormat((*memory.Storage)(nil)))
}
