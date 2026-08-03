package config

import (
	"fmt"
	"slices"
	"strings"
)

// Option defines a key/value entity in a config file.
type Option struct {
	// Key preserving original caseness.
	// Use IsKey instead to compare key regardless of caseness.
	Key string
	// Original value as string, could be not normalized.
	Value string
}

// Options is a collection of Option.
type Options []*Option

// IsKey returns true if the given key matches
// this option's key in a case-insensitive comparison.
func (o *Option) IsKey(key string) bool {
	return strings.EqualFold(o.Key, key)
}

// GoString returns a Go-syntax representation of Options.
func (opts Options) GoString() string {
	strs := make([]string, 0, len(opts))
	for _, opt := range opts {
		strs = append(strs, fmt.Sprintf("%#v", opt))
	}

	return strings.Join(strs, ", ")
}

// Get gets the value for the given key if set,
// otherwise it returns the empty string.
//
// # Note that there is no difference
//
// This matches git behaviour since git v1.8.1-rc1,
// if there are multiple definitions of a key, the
// last one wins.
//
// See: http://article.gmane.org/gmane.linux.kernel/1407184
//
// In order to get all possible values for the same key,
// use GetAll.
func (opts Options) Get(key string) string {
	for i := len(opts) - 1; i >= 0; i-- {
		o := opts[i]
		if o.IsKey(key) {
			return o.Value
		}
	}
	return ""
}

// Has checks if an Option exist with the given key.
func (opts Options) Has(key string) bool {
	for _, o := range opts {
		if o.IsKey(key) {
			return true
		}
	}
	return false
}

// GetAll returns all possible values for the same key.
func (opts Options) GetAll(key string) []string {
	result := []string{}
	for _, o := range opts {
		if o.IsKey(key) {
			result = append(result, o.Value)
		}
	}
	return result
}

// Lookup returns the last value for key and whether the key was present at all.
// Unlike Get it lets callers distinguish an absent key from a key with an empty
// value.
func (opts Options) Lookup(key string) (string, bool) {
	for i := len(opts) - 1; i >= 0; i-- {
		if opts[i].IsKey(key) {
			return opts[i].Value, true
		}
	}
	return "", false
}

// String returns the value for key, or def when key is absent.
func (opts Options) String(key, def string) string {
	if v, ok := opts.Lookup(key); ok {
		return v
	}
	return def
}

// Bool returns the boolean value of key, or def when key is absent. A present
// but unparseable value returns def together with ErrInvalidBool.
func (opts Options) Bool(key string, def bool) (bool, error) {
	v, ok := opts.Lookup(key)
	if !ok {
		return def, nil
	}
	b, err := ParseBool(v)
	if err != nil {
		return def, err
	}
	return b, nil
}

// Int returns the integer value of key, or def when key is absent. A present
// but unparseable value returns def together with an error.
func (opts Options) Int(key string, def int) (int, error) {
	v, ok := opts.Lookup(key)
	if !ok {
		return def, nil
	}
	i, err := ParseInt(v)
	if err != nil {
		return def, err
	}
	return i, nil
}

// Int64 returns the int64 value of key, or def when key is absent. A present
// but unparseable value returns def together with an error.
func (opts Options) Int64(key string, def int64) (int64, error) {
	v, ok := opts.Lookup(key)
	if !ok {
		return def, nil
	}
	i, err := ParseInt64(v)
	if err != nil {
		return def, err
	}
	return i, nil
}

// Uint returns the unsigned value of key, or def when key is absent. A present
// but unparseable value returns def together with an error.
func (opts Options) Uint(key string, def uint) (uint, error) {
	v, ok := opts.Lookup(key)
	if !ok {
		return def, nil
	}
	u, err := ParseUint(v)
	if err != nil {
		return def, err
	}
	return u, nil
}

// Uint64 returns the unsigned 64-bit value of key, or def when key is absent. A
// present but unparseable value returns def together with an error.
func (opts Options) Uint64(key string, def uint64) (uint64, error) {
	v, ok := opts.Lookup(key)
	if !ok {
		return def, nil
	}
	u, err := ParseUint64(v)
	if err != nil {
		return def, err
	}
	return u, nil
}

func (opts Options) withoutOption(key string) Options {
	result := Options{}
	for _, o := range opts {
		if !o.IsKey(key) {
			result = append(result, o)
		}
	}
	return result
}

func (opts Options) withAddedOption(key, value string) Options {
	return append(opts, &Option{key, value})
}

func (opts Options) withSettedOption(key string, values ...string) Options {
	var result Options
	var added []string
	for _, o := range opts {
		if !o.IsKey(key) {
			result = append(result, o)
			continue
		}

		if slices.Contains(values, o.Value) {
			added = append(added, o.Value)
			result = append(result, o)
			continue
		}
	}

	for _, value := range values {
		if slices.Contains(added, value) {
			continue
		}

		result = result.withAddedOption(key, value)
	}

	return result
}
