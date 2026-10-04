package transport

import (
	"reflect"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage"
)

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
