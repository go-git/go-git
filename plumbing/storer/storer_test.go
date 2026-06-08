package storer

import (
	"errors"
	"testing"
)

// idleReleaserImpl is a stand-in to verify that IdleReleaser has
// exactly one method, `CloseIdleDescriptors() error`. The test fails
// to compile if the interface drifts.
type idleReleaserImpl struct{ err error }

func (i idleReleaserImpl) CloseIdleDescriptors() error { return i.err }

var _ IdleReleaser = idleReleaserImpl{}

func TestIdleReleaserContract(t *testing.T) {
	t.Parallel()

	t.Run("NilError", func(t *testing.T) {
		t.Parallel()
		var r IdleReleaser = idleReleaserImpl{}
		if err := r.CloseIdleDescriptors(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ErrorPropagates", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("release-failed")
		var r IdleReleaser = idleReleaserImpl{err: sentinel}
		if err := r.CloseIdleDescriptors(); !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
	})
}
