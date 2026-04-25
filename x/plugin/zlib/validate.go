package zlib

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
)

var (
	// ErrNilProvider is returned when a nil Provider is validated.
	ErrNilProvider = errors.New("zlib provider must not be nil")
	// ErrNilReader is returned when Provider.NewReader reports success but
	// returns a nil Reader.
	ErrNilReader = errors.New("zlib provider returned nil reader")
	// ErrNilWriter is returned when Provider.NewWriter returns a nil Writer.
	ErrNilWriter = errors.New("zlib provider returned nil writer")
)

var validateInitBytes = []byte{0x78, 0x9c, 0x01, 0x00, 0x00, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01}

// ValidateProvider checks the minimal behavioral contract go-git needs
// from a zlib Provider during initialization.
func ValidateProvider(provider Provider) error {
	if isNil(provider) {
		return ErrNilProvider
	}

	reader, err := provider.NewReader(bytes.NewReader(validateInitBytes))
	if err != nil {
		return fmt.Errorf("provider.NewReader: %w", err)
	}
	if isNil(reader) {
		return ErrNilReader
	}
	if err := reader.Close(); err != nil {
		return fmt.Errorf("provider.NewReader.Close: %w", err)
	}

	writer := provider.NewWriter(io.Discard)
	if isNil(writer) {
		return ErrNilWriter
	}

	return nil
}

func isNil(v any) bool {
	if v == nil {
		return true
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
