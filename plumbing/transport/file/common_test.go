package file

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
)

type CommonSuiteHelper struct {
	ReceivePackBin string
	UploadPackBin  string
}

func (h *CommonSuiteHelper) Setup(t *testing.T) {
	if err := exec.Command("git", "--version").Run(); err != nil {
		t.Skip("git command not found")
	}

	tmpDir := t.TempDir()
	h.ReceivePackBin = filepath.Join(tmpDir, "git-receive-pack")
	h.UploadPackBin = filepath.Join(tmpDir, "git-upload-pack")

	bin := filepath.Join(tmpDir, "go-git")

	cmd := exec.Command("go", "build", "-o", bin)
	cmd.Dir = "../../../cli/go-git"

	assert.NoError(t, cmd.Run())
	assert.NoError(t, os.Symlink(bin, h.ReceivePackBin))
	assert.NoError(t, os.Symlink(bin, h.UploadPackBin))
}

func (h *CommonSuiteHelper) TearDown() {
	fixtures.Clean()
}

func (h *CommonSuiteHelper) newEndpoint(t *testing.T, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(name)
	assert.NoError(t, err)

	return ep
}

func (h *CommonSuiteHelper) prepareRepository(t *testing.T, f *fixtures.Fixture) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	assert.NoError(t, err)

	return h.newEndpoint(t, fs.Root())
}
