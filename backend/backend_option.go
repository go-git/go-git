package backend

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
)

// Option configure a Backend
type Option func(*Backend)

// WithPreReceiveHook set a PreReceiveHook to the backend
// https://git-scm.com/docs/git-receive-pack#_pre_receive_hook
func WithPreReceiveHook(hook func(cmd *packp.Command, options []string) error) Option {
	return func(b *Backend) {
		b.PreReceiveHook = hook
	}
}

// WithPostReceiveHook set a PostReceiveHook to the backend
// https://git-scm.com/docs/git-receive-pack#_post_receive_hook
func WithPostReceiveHook(hook func(cmd *packp.Command, options []string) error) Option {
	return func(b *Backend) {
		b.PostReceiveHook = hook
	}
}

// WithPostUpdateHook set a PostUpdateHook to the backend
// https://git-scm.com/docs/git-receive-pack#_post_update_hook
func WithPostUpdateHook(hook func(refs []plumbing.ReferenceName, options []string)) Option {
	return func(b *Backend) {
		b.PostUpdateHook = hook
	}
}
