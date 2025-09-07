package convert

import (
	"bytes"
	"io"
)

type crlfToLFConverter struct {
	w io.Writer
}

// NewCRLFToLFConverter wraps a writer to convert CRLF line endings into LF line endings.
// It assumes that data is text; not binary. See [Stat.IsBinary].
func NewCRLFToLFConverter(w io.Writer) *crlfToLFConverter {
	return &crlfToLFConverter{w: w}
}

func (conv *crlfToLFConverter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	var n int
	for {
		window := data[n:]

		idx := bytes.Index(window, []byte{'\r', '\n'})
		if idx == -1 {
			break
		}

		w, err := conv.w.Write(window[:idx])
		if err != nil {
			return n + w, err
		}
		_, err = conv.w.Write([]byte{'\n'})
		if err != nil {
			return n + w, err
		}

		n += idx + 2
	}

	if data[len(data)-1] == '\r' {
		// Lone CR woudn't exist. So it's CRLF.
		rest, err := conv.w.Write(data[n : len(data)-1])
		if err != nil {
			return n + rest, err
		}
		return n + rest + 1, nil
	}

	rest, err := conv.w.Write(data[n:])
	return n + rest, err
}

type lfToCRLFConverter struct {
	w     io.Writer
	hadCR bool
}

// NewLFToCRLFConverter wraps a writer to convert LF line endings into CRLF line endings.
// It assumes that data is text; not binary. See [Stat.IsBinary].
func NewLFToCRLFConverter(w io.Writer) *lfToCRLFConverter {
	return &lfToCRLFConverter{w: w}
}

func (conv *lfToCRLFConverter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	var n int
	for {
		window := data[n:]

		idx := bytes.IndexByte(window, '\n')
		if idx == -1 {
			break
		}

		switch {
		case idx == 0 && conv.hadCR:
			fallthrough
		case idx > 0 && window[idx-1] == '\r':
			w, err := conv.w.Write(window[:idx+1])
			if err != nil {
				return n + w, err
			}
		default:
			w, err := conv.w.Write(window[:idx])
			if err != nil {
				return n + w, err
			}
			_, err = conv.w.Write([]byte{'\r', '\n'})
			if err != nil {
				return n + w, err
			}
		}

		n += idx + 1
	}

	conv.hadCR = data[len(data)-1] == '\r'

	rest, err := conv.w.Write(data[n:])
	return n + rest, err
}
