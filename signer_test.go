package git

import (
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

type b64signer struct{}

// This is not secure, and is only used as an example for testing purposes.
// Please don't do this.
func (b64signer) Sign(message io.Reader) ([]byte, error) {
	b, err := io.ReadAll(message)
	if err != nil {
		return nil, err
	}
	out := make([]byte, base64.StdEncoding.EncodedLen(len(b)))
	base64.StdEncoding.Encode(out, b)
	return out, nil
}

func ExampleSigner() {
	repo, err := Init(memory.NewStorage(), memfs.New())
	if err != nil {
		panic(err)
	}
	w, err := repo.Worktree()
	if err != nil {
		panic(err)
	}
	commit, err := w.Commit("example commit", &CommitOptions{
		Author: &object.Signature{
			Name:  "John Doe",
			Email: "john@example.com",
			When:  time.UnixMicro(1234567890).UTC(),
		},
		Signer:            b64signer{},
		AllowEmptyCommits: true,
	})
	if err != nil {
		panic(err)
	}

	obj, err := repo.CommitObject(commit)
	if err != nil {
		panic(err)
	}
	fmt.Println(obj.PGPSignature)
	// Output: dHJlZSA0YjgyNWRjNjQyY2I2ZWI5YTA2MGU1NGJmOGQ2OTI4OGZiZWU0OTA0CmF1dGhvciBKb2huIERvZSA8am9obkBleGFtcGxlLmNvbT4gMTIzNCArMDAwMApjb21taXR0ZXIgSm9obiBEb2UgPGpvaG5AZXhhbXBsZS5jb20+IDEyMzQgKzAwMDAKCmV4YW1wbGUgY29tbWl0
}
