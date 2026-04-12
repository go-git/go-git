package memory

import formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"

type options struct {
	objectFormat       formatcfg.ObjectFormat
	compatObjectFormat formatcfg.ObjectFormat
}

func newOptions() options {
	return options{}
}

// StorageOption is a function that configures storage options.
type StorageOption func(*options)

// WithObjectFormat sets the storage's object format.
func WithObjectFormat(of formatcfg.ObjectFormat) StorageOption {
	return func(o *options) {
		o.objectFormat = of
	}
}

// WithCompatObjectFormat sets the storage's compat object format,
// enabling bidirectional hash mapping between the native and compat formats.
func WithCompatObjectFormat(of formatcfg.ObjectFormat) StorageOption {
	return func(o *options) {
		o.compatObjectFormat = of
	}
}
