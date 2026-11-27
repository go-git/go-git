package memory

import formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"

type options struct {
	objectFormat formatcfg.ObjectFormat
}

func newOptions() options {
	return options{}
}

// StorageOption is a function that configures storage options.
type StorageOption func(*options)

// WithObjectFormat sets the storage's object format. Unset means SHA-1.
//
// [NewStorage] cannot return an error, so it does not reject a format
// that is neither SHA-1 nor SHA-256. It records that format in the
// storage's config but hashes objects with SHA-1, and initialising or
// opening a repository on the storage does not reject it either. Use
// [Storage.SetObjectFormat] to get an error for an unsupported format.
func WithObjectFormat(of formatcfg.ObjectFormat) StorageOption {
	return func(o *options) {
		o.objectFormat = of
	}
}
