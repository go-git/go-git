package zlib

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		provider     Provider
		wantErr      error
		wantContains []string
	}{
		{
			name:     "accepts conforming provider",
			provider: validProvider{},
		},
		{
			name:     "rejects nil provider",
			provider: nil,
			wantErr:  ErrNilProvider,
		},
		{
			name:         "rejects reader initialization errors",
			provider:     readerErrorProvider{},
			wantContains: []string{"provider.NewReader", "boom"},
		},
		{
			name:     "rejects nil reader",
			provider: nilReaderProvider{},
			wantErr:  ErrNilReader,
		},
		{
			name:     "rejects nil writer",
			provider: nilWriterProvider{},
			wantErr:  ErrNilWriter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateProvider(tt.provider)
			if tt.wantErr == nil && len(tt.wantContains) == 0 {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			for _, want := range tt.wantContains {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

type validProvider struct{}

func (validProvider) NewReader(r io.Reader) (Reader, error) {
	return NewStdlib().NewReader(r)
}

func (validProvider) NewWriter(w io.Writer) Writer {
	return NewStdlib().NewWriter(w)
}

type readerErrorProvider struct{}

func (readerErrorProvider) NewReader(io.Reader) (Reader, error) {
	return nil, errors.New("boom")
}

func (readerErrorProvider) NewWriter(io.Writer) Writer {
	return NewStdlib().NewWriter(io.Discard)
}

type nilReaderProvider struct{}

func (nilReaderProvider) NewReader(io.Reader) (Reader, error) {
	return nil, nil
}

func (nilReaderProvider) NewWriter(io.Writer) Writer {
	return NewStdlib().NewWriter(io.Discard)
}

type nilWriterProvider struct{}

func (nilWriterProvider) NewReader(r io.Reader) (Reader, error) {
	return NewStdlib().NewReader(r)
}

func (nilWriterProvider) NewWriter(io.Writer) Writer {
	return nil
}
