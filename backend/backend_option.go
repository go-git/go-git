package backend

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
)

type BackendOption func(*Backend)

func WithPreReceiveHook(hook func(cmd *packp.Command, options []string) error) BackendOption {
	return func(b *Backend) {
		b.PreReceiveHook = hook
	}
}

func WithPostReceiveHook(hook func(cmd *packp.Command, options []string) error) BackendOption {
	return func(b *Backend) {
		b.PostReceiveHook = hook
	}
}

func WithPostUpdateHook(hook func(refs []plumbing.ReferenceName, options []string)) BackendOption {
	return func(b *Backend) {
		b.PostUpdateHook = hook
	}
}
