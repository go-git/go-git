package transport

import (
	"errors"
	"reflect"
	"slices"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/storage"
)

// ErrUnsupportedObjectFormat is returned by a push when the server does not
// support the local repository's object format. transport.
// ErrUnsupportedObjectFormat aliases this value.
var ErrUnsupportedObjectFormat = errors.New("the receiving end does not support this repository's hash algorithm")

// ObjectFormat returns the repository's configured object format, defaulting to
// the package default when the storer is nil or its config is missing or unset.
// A typed nil storer, such as a nil *memory.Storage, counts as nil: SendPack
// ignored its storer before it checked the object format, and callers may
// still pass one.
func ObjectFormat(st storage.Storer) config.ObjectFormat {
	if st == nil {
		return config.DefaultObjectFormat
	}
	if v := reflect.ValueOf(st); v.Kind() == reflect.Pointer && v.IsNil() {
		return config.DefaultObjectFormat
	}
	cfg, err := st.Config()
	if err != nil || cfg == nil {
		return config.DefaultObjectFormat
	}
	if cfg.Extensions.ObjectFormat == config.UnsetObjectFormat {
		return config.DefaultObjectFormat
	}
	return cfg.Extensions.ObjectFormat
}

// CheckPushObjectFormat returns ErrUnsupportedObjectFormat when a
// receive-pack server that advertised caps cannot store objects in st's
// format, as git's send-pack refuses it: a server that does not advertise
// object-format only speaks sha1, and an advertising server must list the
// format. git checks before it looks at the refs to push, so a push is
// refused whether or not it has anything to send.
func CheckPushObjectFormat(st storage.Storer, caps capability.List) error {
	if !serverSupportsObjectFormat(caps, ObjectFormat(st)) {
		return ErrUnsupportedObjectFormat
	}
	return nil
}

// serverSupportsObjectFormat mirrors git's server_supports_hash.
func serverSupportsObjectFormat(caps capability.List, f config.ObjectFormat) bool {
	if !caps.Supports(capability.ObjectFormat) {
		return f == config.SHA1
	}
	return slices.Contains(caps.Get(capability.ObjectFormat), f.String())
}
